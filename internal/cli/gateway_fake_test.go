/**
 * @spec-handoff
 * @interface run(args []string, e session.Env, t gateway.Timing) int
 * @behavior
 *   - Test harness only: httptest fake LiteLLM gateway with rules, call recording and in-flight tracking
 *   - runCLI drives run(args, session.Env, gateway.Timing) with an env map as Getenv and captured stdout/stderr
 *   - Pacing, retry and timeouts are gateway.ProdTiming in production; tests pass a gateway.Timing through cliOpts.Timing
 *   - canaryForms builds the canary forms for a fake key at test time
 * @edge-cases
 *   - cliOpts.Timing zero value means testTiming (no pacing, 3 s attempt, 5 ms retry, 2 in flight)
 * @see ./security_test.go
 */

package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shoootyou/litegauge/internal/gateway"
	"github.com/shoootyou/litegauge/internal/session"
)

type fakeRule struct {
	Path   string
	Key    string
	Model  string
	Status int
	Header http.Header
	Body   string
	Delay  time.Duration
	Times  int // harness addition: 0 means unlimited, n > 0 means the rule stops matching after n uses
}

type recordedCall struct {
	Method, Path string
	Query        string
	Header       http.Header
	Body         []byte
	At           time.Time
}

type fakeGateway struct {
	*httptest.Server
	mu          sync.Mutex
	rules       []fakeRule
	used        []int
	calls       []recordedCall
	inFlight    int
	maxInFlight int
}

func newFakeGateway(t *testing.T, rules []fakeRule) *fakeGateway {
	t.Helper()
	g := &fakeGateway{rules: rules, used: make([]int, len(rules))}
	g.Server = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.Server.Close)
	return g
}

func (g *fakeGateway) Calls() []recordedCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]recordedCall(nil), g.calls...)
}

func (g *fakeGateway) CallsTo(method, path string) []recordedCall {
	var out []recordedCall
	for _, c := range g.Calls() {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (g *fakeGateway) MaxInFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.maxInFlight
}

func requestKey(h http.Header) string {
	if v := strings.TrimPrefix(h.Get("Authorization"), "Bearer "); v != "" && h.Get("Authorization") != "" {
		return v
	}
	if v := h.Get("x-api-key"); v != "" {
		return v
	}
	return h.Get("api-key")
}

func requestModel(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &m)
	return m.Model
}

func (g *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	g.mu.Lock()
	g.calls = append(g.calls, recordedCall{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body, At: time.Now()})
	g.inFlight++
	if g.inFlight > g.maxInFlight {
		g.maxInFlight = g.inFlight
	}
	key, model := requestKey(r.Header), requestModel(body)
	var rule *fakeRule
	for i := range g.rules {
		ru := &g.rules[i]
		if ru.Path != r.URL.Path || (ru.Key != "" && ru.Key != key) || (ru.Model != "" && ru.Model != model) {
			continue
		}
		if ru.Times > 0 && g.used[i] >= ru.Times {
			continue
		}
		g.used[i]++
		rule = ru
		break
	}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.inFlight--
		g.mu.Unlock()
	}()
	if rule == nil {
		http.Error(w, "fake: no rule for "+r.URL.Path, http.StatusNotFound)
		return
	}
	if rule.Delay > 0 {
		select {
		case <-time.After(rule.Delay):
		case <-r.Context().Done():
			return
		}
	}
	for k, vs := range rule.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		if strings.HasPrefix(rule.Body, "{") || strings.HasPrefix(rule.Body, "[") {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "text/plain")
		}
	}
	w.WriteHeader(rule.Status)
	_, _ = io.WriteString(w, rule.Body)
}

// testTiming keeps every run fast: no pacing and a 5 ms retry delay.
var testTiming = gateway.Timing{Attempt: 3 * time.Second, Pace: 0, Retry: 5 * time.Millisecond, InFlight: 2}

type cliOpts struct {
	Timing    gateway.Timing // zero means testTiming
	Env       map[string]string
	Stdin     string
	StdinR    io.Reader
	Transport http.RoundTripper
	StdinTTY  bool
	StdoutTTY bool
}

type cliResult struct {
	Code           int
	Stdout, Stderr string
	EnvQueried     []string
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func runCLI(t *testing.T, o cliOpts, args ...string) cliResult {
	t.Helper()
	var out, errb syncBuf
	var queried []string
	var qmu sync.Mutex
	stdin := o.StdinR
	if stdin == nil {
		stdin = strings.NewReader(o.Stdin)
	}
	e := session.Env{
		Getenv: func(name string) string {
			qmu.Lock()
			queried = append(queried, name)
			qmu.Unlock()
			return o.Env[name]
		},
		Stdin:     stdin,
		Stdout:    &out,
		Stderr:    &errb,
		StdinTTY:  o.StdinTTY,
		StdoutTTY: o.StdoutTTY,
		Transport: o.Transport,
	}
	t0 := o.Timing
	if t0 == (gateway.Timing{}) {
		t0 = testTiming
	}
	code := run(args, e, t0)
	return cliResult{Code: code, Stdout: out.String(), Stderr: errb.String(), EnvQueried: queried}
}

func gwEnv(g *fakeGateway, key string) map[string]string {
	return map[string]string{"LITEGAUGE_URL": g.URL, "LITEGAUGE_KEY": key}
}

// countingTransport proves that no request left the process.
type countingTransport struct {
	mu sync.Mutex
	n  int
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return nil, errors.New("test transport: no network")
}

func (c *countingTransport) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

var testdataAbs = func() string {
	p, err := filepath.Abs("testdata")
	if err != nil {
		panic(err)
	}
	return p
}()

func td(parts ...string) string { return filepath.Join(append([]string{testdataAbs}, parts...)...) }

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	canaryKey = "sk-FAKE-Zq7Xk9Rt2LmVwf4k3"
	otherKey  = "sk-FAKE-Hn4Jm6Kb8Vc1Xz2Q9"
)

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// canaryForms returns C1 to C6 for key.
func canaryForms(key string) []string {
	tail := key[len(key)-4:]
	mid := key[len(key)/2-4 : len(key)/2+4]
	b := []byte(key)
	all := []string{
		key,
		"sk-..." + tail, "sk-****" + tail, "****" + tail, "..." + tail,
		tail + "..." + tail, tail[:1] + "***" + tail, "XXXXXXXX" + tail,
		sha256Hex(key),
		"Bearer " + key, "Authorization: Bearer " + key,
		base64.StdEncoding.EncodeToString(b), base64.URLEncoding.EncodeToString(b),
		base64.RawStdEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b),
		mid,
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range all {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// canaryBlob is every canary form on one line, safe inside a header value.
func canaryBlob(key string) string { return strings.Join(canaryForms(key), " ") }

// assertNoLeak covers P1 (canary forms) and P2 (6-char key windows, 8-char hash windows).
func assertNoLeak(t *testing.T, where, out string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		for _, f := range canaryForms(k) {
			if strings.Contains(out, f) {
				t.Errorf("%s: canary form %q appears in output", where, f)
			}
		}
		body := k[len("sk-FAKE-"):]
		for i := 0; i+6 <= len(body); i++ {
			if w := body[i : i+6]; strings.Contains(out, w) {
				t.Errorf("%s: 6-char key window %q appears in output", where, w)
			}
		}
		h := sha256Hex(k)
		for i := 0; i+8 <= len(h); i++ {
			if w := h[i : i+8]; strings.Contains(out, w) {
				t.Errorf("%s: 8-char hash window %q appears in output", where, w)
			}
		}
	}
}

func assertNoText(t *testing.T, where, out string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if s != "" && strings.Contains(out, s) {
			t.Errorf("%s: forbidden text %q appears in output", where, s)
		}
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func nilIf(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func errJSON(typ, code, msg string) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": nilIf(typ), "param": nil, "code": nilIf(code)}})
	return string(b)
}

func pf(x float64) *float64 { return &x }

type infoEntry struct {
	Name    string
	In, Out *float64
	Mode    string
	Decoys  map[string]any // extra model_info fields carrying canaries
}

func modelInfoBody(entries ...infoEntry) string {
	var data []any
	for _, e := range entries {
		mi := map[string]any{"id": "id-" + e.Name, "db_model": false}
		params := map[string]any{"model": "provider/" + e.Name, "api_key": "sk-...f4k3"}
		if e.In != nil {
			mi["input_cost_per_token"] = *e.In
		}
		if e.Out != nil {
			mi["output_cost_per_token"] = *e.Out
		}
		if e.Mode != "" {
			mi["mode"] = e.Mode
		}
		for k, v := range e.Decoys {
			mi[k] = v
		}
		data = append(data, map[string]any{"model_name": e.Name, "litellm_params": params, "model_info": mi})
	}
	b, _ := json.Marshal(map[string]any{"data": data})
	return string(b)
}

func modelsBody(ownedBy string, ids ...string) string {
	var data []any
	for _, id := range ids {
		data = append(data, map[string]any{"id": id, "object": "model", "created": 1, "owned_by": ownedBy})
	}
	b, _ := json.Marshal(map[string]any{"object": "list", "data": data})
	return string(b)
}

func paidInfo(model string) infoEntry {
	return infoEntry{Name: model, In: pf(3e-06), Out: pf(1.5e-05), Mode: "chat"}
}

func freeInfo(model string) infoEntry {
	return infoEntry{Name: model, In: pf(0), Out: pf(0), Mode: "chat"}
}

func plain404(path string) fakeRule {
	return fakeRule{Path: path, Status: 404, Body: "404 page not found"}
}

// leakHeaders places canaries in response headers (C8).
func leakHeaders(key string) http.Header {
	h := http.Header{}
	h.Set("X-Litellm-Key-Spend", "12.5")
	h.Set("X-Litellm-Debug", canaryBlob(key))
	h.Set("Set-Cookie", "sid="+key)
	h.Set("Www-Authenticate", `Bearer realm="`+key+`"`)
	return h
}

func sprintCalls(cs []recordedCall) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.Method+" "+c.Path)
	}
	return fmt.Sprint(parts)
}

func TestFakeGateway_MatchesRulesInOrderAndRecordsCalls(t *testing.T) {
	g := newFakeGateway(t, []fakeRule{
		{Path: "/p", Key: "k1", Status: 201, Body: "one"},
		{Path: "/p", Status: 202, Body: "two", Times: 1},
		{Path: "/p", Status: 203, Body: "three"},
	})
	get := func(key string) int {
		req, _ := http.NewRequest("POST", g.URL+"/p", strings.NewReader(`{"model":"m"}`))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := []int{get("k1"), get("k2"), get("k2"), get("k2")}; fmt.Sprint(got) != "[201 202 203 203]" {
		t.Fatalf("rule order or Times wrong: %v", got)
	}
	if n := len(g.CallsTo("POST", "/p")); n != 4 {
		t.Fatalf("calls recorded = %d, want 4", n)
	}
}
