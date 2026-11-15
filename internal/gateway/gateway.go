// Package gateway talks to a LiteLLM gateway: one probe request per cell, the model listing and the model prices.
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/keys"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/verdict"
	"github.com/shoootyou/litegauge/internal/versions"
)

const listBodyLimit = 1 << 20

// Timing is a value so tests can run without pacing; production uses ProdTiming.
type Timing struct {
	Attempt, Pace, Retry time.Duration
	InFlight             int
}

var ProdTiming = Timing{Attempt: 30 * time.Second, Pace: 500 * time.Millisecond, Retry: time.Second, InFlight: 2}

type Client struct {
	base   *url.URL
	client *http.Client
	t      Timing
	pace   *pacer
}

func New(base *url.URL, rt http.RoundTripper, t Timing) *Client {
	if rt == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		d := min(10*time.Second, t.Attempt)
		tr.DialContext = (&net.Dialer{Timeout: d}).DialContext
		tr.TLSHandshakeTimeout = d
		// HTTP/1.1 only: an h2 debug log (GODEBUG=http2debug) could print the Authorization header.
		tr.Protocols = new(http.Protocols)
		tr.Protocols.SetHTTP1(true)
		// Protocols alone leaves h2 in the TLS ALPN offer: a server that picked it would get HTTP/1.1 on an h2 connection.
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
		if base.Scheme == "http" { // a proxy would receive the key in clear
			tr.Proxy = nil
		}
		rt = tr
	}
	client := &http.Client{
		Transport: rt,
		Timeout:   t.Attempt,
		// A redirect would carry the key to another host: the 3xx response is returned as is.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Client{base: base, client: client, t: t, pace: &pacer{every: t.Pace}}
}

type rawResp struct {
	status int
	hdr    http.Header
	body   []byte
	err    error
	ms     int
}

func (c *Client) do(ctx context.Context, method, path string, h http.Header, body []byte, limit int64) rawResp {
	u := *c.base
	u.Path = c.base.Path + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return rawResp{err: err}
	}
	req.Header = h
	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return rawResp{err: err, ms: sinceMS(start)}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return rawResp{status: resp.StatusCode, hdr: resp.Header, body: b, err: err, ms: sinceMS(start)}
}

func sinceMS(t time.Time) int { return int(time.Since(t) / time.Millisecond) }

func baseHeaders(key string) http.Header {
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set("User-Agent", messages.Name+"/"+versions.Version)
	h.Set("Authorization", "Bearer "+key)
	return h
}

// probeRequest builds the chat request with a fresh nonce, so no cache can answer for another key.
func probeRequest(model, key string) (h http.Header, body []byte) {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	h = baseHeaders(key)
	h.Set("Content-Type", "application/json")
	body, _ = json.Marshal(struct {
		Model     string              `json:"model"`
		MaxTokens int                 `json:"max_tokens"`
		Messages  []map[string]string `json:"messages"`
	}{model, 16, []map[string]string{{"role": "user", "content": "ping lg-" + hex.EncodeToString(raw[:])}}})
	return h, body
}

// pacer spaces request starts at least `every` apart across all workers.
type pacer struct {
	mu    sync.Mutex
	next  time.Time
	every time.Duration
}

func (p *pacer) wait(ctx context.Context) error {
	if p.every <= 0 {
		return ctx.Err()
	}
	p.mu.Lock()
	slot := p.next
	if now := time.Now(); slot.Before(now) {
		slot = now
	}
	p.next = slot.Add(p.every)
	p.mu.Unlock()
	return sleepUntil(ctx, slot)
}

func sleepUntil(ctx context.Context, at time.Time) error {
	d := time.Until(at)
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunGrid probes every key x model cell with at most Timing.InFlight requests running and starts paced apart.
// A worker panic cancels the run and is raised again here, once every worker has stopped.
func (c *Client) RunGrid(ctx context.Context, ks []keys.Entry, models []string, prices []verdict.Price, onDone func(key, model int, r verdict.Result)) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var crash atomic.Pointer[any]
	var next atomic.Int64
	total := len(ks) * len(models)
	var wg sync.WaitGroup
	for range max(1, min(c.t.InFlight, total)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					crash.CompareAndSwap(nil, &r)
					cancel()
				}
			}()
			for {
				i := int(next.Add(1)) - 1
				if i >= total || c.pace.wait(ctx) != nil {
					return
				}
				key, model := i/len(models), i%len(models)
				r := c.Probe(ctx, ks[key].Name, ks[key].Value, models[model], prices[model])
				if ctx.Err() != nil {
					return
				}
				onDone(key, model, r)
			}
		}()
	}
	wg.Wait()
	if p := crash.Load(); p != nil {
		panic(*p)
	}
}

// Probe sends one cell: one request, plus one after the retry delay on a transport error, timeout or 5xx.
func (c *Client) Probe(ctx context.Context, keyName, key, model string, price verdict.Price) verdict.Result {
	var raw rawResp
	for attempt := range 2 {
		if attempt == 1 && sleepUntil(ctx, time.Now().Add(c.t.Retry)) != nil {
			break
		}
		h, body := probeRequest(model, key)
		raw = c.do(ctx, http.MethodPost, "/v1/chat/completions", h, body, 64<<10)
		if ctx.Err() != nil || (raw.err == nil && raw.status < 500) {
			break
		}
	}
	r := verdict.Result{Key: keyName, Model: model, Price: price, Verdict: verdict.Error, Status: raw.status, LatencyMS: raw.ms}
	if raw.err == nil && raw.status != 0 {
		r.Verdict = verdict.Classify(raw.status, raw.hdr, raw.body, price)
	}
	return r
}

// ListModels returns the served ids; a failed call is the sentence of the listing error.
func (c *Client) ListModels(ctx context.Context, key string) ([]string, error) {
	raw := c.do(ctx, http.MethodGet, "/v1/models", baseHeaders(key), nil, listBodyLimit)
	if ctx.Err() != nil {
		return nil, apperr.ErrInterrupted
	}
	var doc struct {
		Data *[]struct {
			ID *string `json:"id"`
		} `json:"data"`
	}
	if raw.err != nil || raw.status < 200 || raw.status >= 300 || json.Unmarshal(raw.body, &doc) != nil || doc.Data == nil {
		if raw.status == 0 {
			return nil, ListError(messages.Err.NoReply)
		}
		return nil, ListError(fmt.Sprintf(messages.Err.HTTPStatus, raw.status))
	}
	var ids []string
	seen := map[string]bool{}
	for _, m := range *doc.Data {
		if m.ID != nil && !seen[*m.ID] {
			seen[*m.ID] = true
			ids = append(ids, *m.ID)
		}
	}
	return ids, nil
}

// ListError is the fatal error of a listing that failed or came back empty; reason is one of the messages.Err reasons.
func ListError(reason string) error {
	return apperr.Fatal(fmt.Sprintf(messages.Err.ListFailed, reason))
}

// Meta holds the only two things the tool reads from model_info: the mode and both prices.
type Meta struct {
	mode    *string
	in, out *float64
}

// ModelInfo tries /v1/model/info, then /model/info; the first 200 with a parsable data list wins.
func (c *Client) ModelInfo(ctx context.Context, key string) map[string]Meta {
	for _, path := range []string{"/v1/model/info", "/model/info"} {
		raw := c.do(ctx, http.MethodGet, path, baseHeaders(key), nil, listBodyLimit)
		if raw.err != nil || raw.status != http.StatusOK {
			continue
		}
		if metas, ok := parseModelInfo(raw.body); ok {
			return metas
		}
	}
	return nil
}

func parseModelInfo(body []byte) (map[string]Meta, bool) {
	var doc struct {
		Data *[]struct {
			ModelName string         `json:"model_name"`
			ModelInfo map[string]any `json:"model_info"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &doc) != nil || doc.Data == nil {
		return nil, false
	}
	entries := map[string][]Meta{}
	for _, d := range *doc.Data {
		entries[d.ModelName] = append(entries[d.ModelName], metaFrom(d.ModelInfo))
	}
	out := make(map[string]Meta, len(entries))
	for name, es := range entries {
		out[name] = Meta{
			mode: agreed(es, func(m Meta) *string { return m.mode }),
			in:   agreed(es, func(m Meta) *float64 { return m.in }),
			out:  agreed(es, func(m Meta) *float64 { return m.out }),
		}
	}
	return out, true
}

// as is v as a T, or nil when v is of another type.
func as[T any](v any) *T {
	if t, ok := v.(T); ok {
		return &t
	}
	return nil
}

// metaFrom reads only model_info.* fields; litellm_params and top-level fields are never looked at.
func metaFrom(mi map[string]any) Meta {
	price := func(v any) *float64 {
		if p := as[float64](v); p != nil && *p >= 0 {
			return p
		}
		return nil
	}
	return Meta{mode: as[string](mi["mode"]), in: price(mi["input_cost_per_token"]), out: price(mi["output_cost_per_token"])}
}

// agreed keeps a field only when every entry for the model says the same thing.
func agreed[T comparable](es []Meta, get func(Meta) *T) *T {
	first := get(es[0])
	for _, e := range es[1:] {
		if v := get(e); (first == nil) != (v == nil) || (v != nil && *first != *v) {
			return nil
		}
	}
	return first
}

// TextModels lists the served models of text mode, plus those whose mode the gateway does not state.
func TextModels(ids []string, metas map[string]Meta) []string {
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
		m := metas[id].mode
		return m != nil && !slices.Contains([]string{"chat", "completion", "responses"}, *m)
	})
}

// PriceOf: free needs both prices at 0, paid needs both known and one above 0.
func PriceOf(m Meta) verdict.Price {
	switch {
	case m.in == nil || m.out == nil:
		return verdict.Unknown
	case *m.in == 0 && *m.out == 0:
		return verdict.Free
	}
	return verdict.Paid
}

// Prices is the price of each id, by position.
func Prices(ids []string, metas map[string]Meta) []verdict.Price {
	out := make([]verdict.Price, len(ids))
	for i, id := range ids {
		out[i] = PriceOf(metas[id])
	}
	return out
}
