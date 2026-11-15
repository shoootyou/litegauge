/**
 * @spec-handoff
 * @interface litegauge probe|matrix|models under attack inputs, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - S1 a key flag or an argument shaped like a key exits 2 before any request and is never echoed
 *   - S2 stdout+stderr never hold a canary form of the key, nor any gateway body, header or error-message text, in every verdict scenario and every -o mode
 *   - S3 the key travels only in the Authorization header, to the base host only; S4 a 3xx is returned, not followed
 *   - S5 (HTTP/1.1 only) and S10 (proxy use) are white-box tests of the transport and live in internal/gateway/gateway_test.go
 *   - S6 error bodies are read up to 64 KiB, listings up to 1 MiB
 *   - S7 LITEGAUGE_KEY is gone from the process environment after the run reads it
 *   - S8 a panic in the main path, a worker or the TUI ends as "Error: internal error." with exit 1 and no trace
 *   - S9 model ids carrying a key window or hash are never printed or probed
 *   - S11 SIGINT exits 130 within 2 s, also on an idle FIFO keys file
 * @edge-cases
 *   - The idle FIFO keys file has no writer; the run must still return on SIGINT
 * @see ./classify_test.go
 */

package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/shoootyou/litegauge/internal/session"
)

func TestS1_KeyFlagsAreRejectedBeforeAnyRequestWithoutEcho(t *testing.T) {
	for _, flag := range []string{"--key", "--token", "--api-key", "--password", "--secret"} {
		for _, form := range []string{"spaced", "equals"} {
			t.Run(strings.TrimPrefix(flag, "--")+"_"+form, func(t *testing.T) {
				g := keysGateway(t)
				args := []string{"probe", probeModel, flag, otherKey}
				if form == "equals" {
					args = []string{"probe", probeModel, flag + "=" + otherKey}
				}
				res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, args...)
				if res.Code != 2 || res.Stdout != "" {
					t.Errorf("exit=%d stdout=%q, want exit 2 and no stdout", res.Code, res.Stdout)
				}
				assertNoLeak(t, "stderr", res.Stderr, otherKey)
				assertNoText(t, "stderr", res.Stderr, otherKey)
				// the flag as a whole token: "--keys-file" in the usage text is legitimate
				if flagRe := regexp.MustCompile(regexp.QuoteMeta(flag) + `(?:[^\w-]|$)`); flagRe.MatchString(res.Stderr) {
					t.Errorf("stderr echoes the forbidden flag %q: %q", flag, res.Stderr)
				}
				if n := len(g.Calls()); n != 0 {
					t.Errorf("%d requests reached the gateway", n)
				}
			})
		}
	}
}

func TestS1_ArgumentsShapedLikeKeysAreRejectedBeforeAnyRequestWithoutEcho(t *testing.T) {
	hex64 := sha256Hex("any")
	loaded := "FAKEtoken0123456789"
	cases := map[string][]string{
		"sk_shape":        {"probe", "sk-FAKE-Argument-0000001"},
		"sk_shape_in_o":   {"probe", probeModel, "-o", "sk-FAKE-Argument-0000001"},
		"hex64_shape":     {"probe", hex64},
		"loaded_key":      {"probe", loaded},
		"loaded_key_name": {"probe", probeModel, "--name", loaded},
		// a key-shaped value that passes the name rule: without the argument check it would be echoed as an unknown name
		"sk_shape_name": {"probe", probeModel, "--name", "sk-FAKE-Argument-0000001"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			g := keysGateway(t)
			res := runCLI(t, cliOpts{Env: gwEnv(g, loaded)}, args...)
			if res.Code != 2 || res.Stdout != "" {
				t.Errorf("exit=%d stdout=%q, want exit 2 and no stdout", res.Code, res.Stdout)
			}
			assertNoText(t, "stderr", res.Stderr, "Argument-0000001", hex64, loaded)
			if n := len(g.Calls()); n != 0 {
				t.Errorf("%d requests reached the gateway", n)
			}
		})
	}
}

// leakScenarios hold the canary forms in every place the gateway controls.
type leakScenario struct {
	name   string
	price  string
	status int
	body   string
	hdr    http.Header
}

func leakScenarios() []leakScenario {
	blob := canaryBlob(canaryKey)
	mk := func(name string, status int, typ, code string) leakScenario {
		h := leakHeaders(canaryKey)
		h.Set("X-Leak-Sentinel", "HDRTEXT-"+name)
		return leakScenario{name, "paid", status, errJSON(typ, code, "GWMSG-"+name+" E003 "+blob), h}
	}
	okLeak := `{"id":"chatcmpl-RAWBODY1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"GWMSG-ok ` + blob + `"}}]}`
	h := leakHeaders(canaryKey)
	h.Set("X-Leak-Sentinel", "HDRTEXT-ok")
	cache := h.Clone()
	cache.Set("X-Litellm-Cache-Key", "HDRTEXT-cache")
	redirect := h.Clone()
	redirect.Set("Location", "http://127.0.0.1:1/HDRTEXT-redirect")
	return []leakScenario{
		{"ok", "paid", 200, okLeak, h},
		{"key_valid_free", "free", 200, okLeak, h},
		{"key_valid_cache", "paid", 200, okLeak, cache},
		mk("no_budget", 422, "budget_exceeded", "422"),
		mk("blocked", 400, "invalid_request_error", ""),
		mk("invalid_key", 401, "auth_error", "401"),
		mk("model_not_found", 404, "not_found_error", "model_not_found"),
		mk("rate_limit", 429, "rate_limit_error", ""),
		mk("server_error", 500, "", ""),
		{"redirect", "paid", 302, "GWMSG-redirect " + blob, redirect},
		{"unparsed", "paid", 418, "GWMSG-unparsed " + blob, h},
	}
}

func TestS2_ProbeNeverPrintsCanariesOrGatewayTextInAnyScenarioAndMode(t *testing.T) {
	for _, sc := range leakScenarios() {
		for _, mode := range []string{"table", "json", "yaml"} {
			t.Run(sc.name+"_"+mode, func(t *testing.T) {
				g := chatGateway(t, sc.price, sc.status, sc.body, sc.hdr)
				res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel, "-o", mode)
				out := res.Stdout + res.Stderr
				if out == "" || len(g.CallsTo("POST", chatPath)) == 0 {
					t.Fatalf("the probe never ran (calls=%d, output=%q): the scenario proves nothing", len(g.CallsTo("POST", chatPath)), out)
				}
				assertNoLeak(t, sc.name+" "+mode, out, canaryKey)
				assertNoText(t, sc.name+" "+mode, out, "GWMSG-", "HDRTEXT-", "RAWBODY1", "E003", "12.5", "sid=", "realm")
			})
		}
	}
}

func TestS2_MatrixAndModelsNeverPrintCanariesOrGatewayTextInAnyMode(t *testing.T) {
	blob := canaryBlob(canaryKey)
	h := leakHeaders(canaryKey)
	for _, mode := range []string{"table", "json", "yaml"} {
		t.Run("matrix_"+mode, func(t *testing.T) {
			g := newFakeGateway(t, append([]fakeRule{
				{Path: chatPath, Status: 400, Body: errJSON("invalid_request_error", "", "GWMSG-matrix E003 "+blob), Header: h},
				{Path: "/v1/models", Status: 200, Body: modelsBody("GWMSG-owner "+blob, probeModel)},
			}, infoRules("paid")...))
			res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "matrix", "-o", mode)
			if len(g.CallsTo("POST", chatPath)) == 0 || res.Stdout == "" {
				t.Fatalf("the matrix never ran (calls=%s, stdout=%q): the scenario proves nothing", sprintCalls(g.Calls()), res.Stdout)
			}
			assertNoLeak(t, "matrix "+mode, res.Stdout+res.Stderr, canaryKey)
			assertNoText(t, "matrix "+mode, res.Stdout+res.Stderr, "GWMSG-", "E003", "sid=")
		})
		t.Run("models_ok_"+mode, func(t *testing.T) {
			g := newFakeGateway(t, []fakeRule{
				{Path: "/v1/models", Status: 200, Body: modelsBody("GWMSG-owner "+blob, probeModel), Header: h},
				{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(infoEntry{Name: probeModel, In: pf(1e-6), Out: pf(1e-6), Mode: "chat", Decoys: map[string]any{"note": "GWMSG-info " + blob}})},
			})
			res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models", "-o", mode)
			if res.Code != 0 || res.Stdout == "" {
				t.Fatalf("models never ran: exit=%d stdout=%q: the scenario proves nothing", res.Code, res.Stdout)
			}
			assertNoLeak(t, "models "+mode, res.Stdout+res.Stderr, canaryKey)
			assertNoText(t, "models "+mode, res.Stdout+res.Stderr, "GWMSG-", "sid=")
		})
		t.Run("models_error_"+mode, func(t *testing.T) {
			g := newFakeGateway(t, []fakeRule{{Path: "/v1/models", Status: 401, Body: errJSON("auth_error", "401", "GWMSG-list "+blob), Header: h}})
			res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models", "-o", mode)
			if res.Code != 1 || len(g.CallsTo("GET", "/v1/models")) == 0 {
				t.Fatalf("the listing never failed as set up: exit=%d stderr=%q", res.Code, res.Stderr)
			}
			assertNoLeak(t, "models error "+mode, res.Stdout+res.Stderr, canaryKey)
			assertNoText(t, "models error "+mode, res.Stdout+res.Stderr, "GWMSG-", "sid=")
		})
	}
}

func TestS3_KeyTravelsOnlyInTheAuthorizationHeaderToTheBaseHost(t *testing.T) {
	g := keysGateway(t)
	if res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel); res.Code != 0 {
		t.Fatalf("exit = %d stderr=%q", res.Code, res.Stderr)
	}
	calls := g.Calls()
	if len(calls) == 0 {
		t.Fatal("no request reached the gateway")
	}
	for _, c := range calls {
		if got := c.Header.Get("Authorization"); got != "Bearer "+canaryKey {
			t.Errorf("%s %s: Authorization = %q, want the bearer key", c.Method, c.Path, got)
		}
		for name, vs := range c.Header {
			if name != "Authorization" && strings.Contains(strings.Join(vs, " "), canaryKey[len("sk-FAKE-"):]) {
				t.Errorf("%s %s: key in header %s", c.Method, c.Path, name)
			}
		}
		if strings.Contains(c.Path+c.Query+string(c.Body), canaryKey[len("sk-FAKE-"):]) {
			t.Errorf("%s %s: key in the path, query or body", c.Method, c.Path)
		}
	}
}

func TestS4_RedirectsAreReturnedNotFollowedForEveryMethod(t *testing.T) {
	other := newFakeGateway(t, []fakeRule{{Path: "/landing", Status: 200, Body: "{}"}})
	loc := http.Header{"Location": {other.URL + "/landing"}}
	g := newFakeGateway(t, []fakeRule{
		{Path: chatPath, Status: 307, Header: loc},
		{Path: "/v1/models", Status: 301, Header: loc},
		{Path: "/v1/model/info", Status: 302, Header: loc},
	})
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models")
	if res.Code != 1 {
		t.Errorf("models over a redirect exit = %d, want 1", res.Code)
	}
	runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel)
	if n := len(other.Calls()); n != 0 {
		t.Errorf("the redirect target received %d requests (the key would travel with them), want 0", n)
	}
}

func padded(typ string, n int) string {
	return `{"error":{"type":"` + typ + `","message":"` + strings.Repeat("a", n) + `"}}`
}

func TestS6_ErrorBodyIsReadUpTo64KiBOnly(t *testing.T) {
	inside := padded("budget_exceeded", 60<<10)
	beyond := padded("budget_exceeded", 70<<10)
	_, r := probeJSON(t, chatGateway(t, "paid", 422, inside, nil), cliOpts{})
	if r.Verdict != "no_budget" {
		t.Errorf("an error body inside 64 KiB gave %q, want no_budget", r.Verdict)
	}
	_, r = probeJSON(t, chatGateway(t, "paid", 422, beyond, nil), cliOpts{})
	if r.Verdict != "error" {
		t.Errorf("an error body beyond 64 KiB gave %q, want error (the tail must not be parsed)", r.Verdict)
	}
}

func listing(n int) string {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"m-%07d"}`, i)
	}
	b.WriteString(`]}`)
	return b.String()
}

func TestS6_ModelsListingIsReadUpTo1MiBOnly(t *testing.T) {
	// listing(n) is 19n+10 bytes: 47000 entries are about 893,000 bytes, 58000 about 1,102,000, 1 MiB is 1,048,576 in between.
	run := func(n int) cliResult {
		body := listing(n)
		if n == 47000 && len(body) > 1<<20-100_000 || n == 58000 && len(body) < 1<<20+40_000 {
			t.Fatalf("listing(%d) is %d bytes, too close to 1 MiB", n, len(body))
		}
		g := newFakeGateway(t, []fakeRule{{Path: "/v1/models", Status: 200, Body: body}, plain404("/v1/model/info"), plain404("/model/info")})
		return runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models", "-o", "json")
	}
	if res := run(47000); res.Code != 0 {
		t.Errorf("a listing under 1 MiB exit = %d stderr=%q, want 0", res.Code, res.Stderr)
	}
	if res := run(58000); res.Code != 1 || !strings.Contains(res.Stderr, "could not list the models") {
		t.Errorf("a listing over 1 MiB exit = %d stderr=%q, want 1 and the listing sentence", res.Code, res.Stderr)
	}
}

func TestS7_KeyLeavesTheProcessEnvironmentOnceRead(t *testing.T) {
	g := keysGateway(t)
	t.Setenv("LITEGAUGE_KEY", canaryKey)
	t.Setenv("LITEGAUGE_URL", g.URL)
	var out, errb syncBuf
	e := session.Env{Getenv: os.Getenv, Unsetenv: os.Unsetenv, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb}
	if code := run([]string{"probe", probeModel}, e, testTiming); code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, errb.String())
	}
	if len(g.CallsTo("POST", chatPath)) != 1 {
		t.Fatalf("the probe did not reach the gateway: %s", sprintCalls(g.Calls()))
	}
	if _, ok := os.LookupEnv("LITEGAUGE_KEY"); ok {
		t.Error("LITEGAUGE_KEY is still set in the process environment after it was read")
	}
}

// panicRT panics on POST (the probe requests) and answers GET with 404.
type panicRT struct{ posts atomic.Int32 }

func (p *panicRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost {
		p.posts.Add(1)
		panic("PANIC-CANARY " + canaryKey)
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("not found")), Request: r}, nil
}

func dummyEnv() map[string]string {
	return map[string]string{"LITEGAUGE_URL": "https://gw.example.com", "LITEGAUGE_KEY": canaryKey}
}

func assertInternalError(t *testing.T, res cliResult, rt *panicRT) {
	t.Helper()
	if res.Code != 1 || !strings.Contains(res.Stderr, "Error: internal error.") {
		t.Errorf("exit=%d stdout=%q stderr=%q, want exit 1 and \"Error: internal error.\"", res.Code, res.Stdout, res.Stderr)
	}
	assertNoText(t, "output", res.Stdout+res.Stderr, "goroutine ", "panic:", "PANIC-CANARY", ".go:", canaryKey)
	if rt.posts.Load() == 0 {
		t.Error("the panicking transport was never reached: the test proves nothing")
	}
}

func TestS8_PanicOnTheProbePathEndsAsInternalError(t *testing.T) {
	rt := &panicRT{}
	res, ok := within(10*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: dummyEnv(), Transport: rt}, "probe", probeModel)
	})
	if !ok {
		t.Fatal("run did not return")
	}
	assertInternalError(t, res, rt)
}

func TestS8_PanicInAMatrixWorkerEndsAsInternalError(t *testing.T) {
	rt := &panicRT{}
	res, ok := within(10*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: dummyEnv(), Transport: rt}, "matrix", "--models", probeModel+",m2,m3")
	})
	if !ok {
		t.Fatal("run did not return")
	}
	assertInternalError(t, res, rt)
}

func TestS8_PanicInTheTUIWorkEndsAsInternalError(t *testing.T) {
	rt := &panicRT{}
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	go func() {
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, err := pw.Write([]byte("q")); err != nil {
				return
			}
		}
	}()
	res, ok := within(10*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: dummyEnv(), Transport: rt, StdinR: pr, StdinTTY: true, StdoutTTY: true}, "probe", probeModel)
	})
	if !ok {
		t.Fatal("run did not return")
	}
	if res.Code != 1 || !strings.Contains(stripANSI(res.Stdout)+res.Stderr, "Error: internal error.") {
		t.Errorf("exit=%d stdout=%q stderr=%q, want exit 1 and \"Error: internal error.\"", res.Code, res.Stdout, res.Stderr)
	}
	assertNoText(t, "output", res.Stdout+res.Stderr, "goroutine ", "PANIC-CANARY", canaryKey)
}

func TestS9_ModelIDsCarryingAKeyWindowOrHashAreNeverPrintedOrProbed(t *testing.T) {
	window := canaryKey[len("sk-FAKE-") : len("sk-FAKE-")+8]
	hash := sha256Hex(canaryKey)[:8]
	bad := []string{"m-" + window + "-x", "m-" + hash + "-x"}
	g := newFakeGateway(t, []fakeRule{
		{Path: "/v1/models", Status: 200, Body: modelsBody("openai", append([]string{probeModel}, bad...)...)},
		{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(paidInfo(probeModel))},
		plain404("/model/info"),
		{Path: chatPath, Status: 200, Body: okBody()},
	})
	for _, cmd := range []string{"models", "matrix"} {
		res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, cmd, "-o", "json")
		assertNoText(t, cmd, res.Stdout+res.Stderr, window, hash)
		if !strings.Contains(res.Stdout, probeModel) {
			t.Errorf("%s dropped the safe model too: %q", cmd, res.Stdout)
		}
	}
	for _, c := range g.CallsTo("POST", chatPath) {
		if m := requestModel(c.Body); m != probeModel {
			t.Errorf("a request probed model %q", m)
		}
	}
}

// signalWhen sends SIGINT to this process every 50 ms once cond holds. A test-owned sink keeps the
// default action (process exit) from applying, whatever the code under test does.
func signalWhen(t *testing.T, cond func() bool) {
	t.Helper()
	sink := make(chan os.Signal, 16)
	signal.Notify(sink, os.Interrupt)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop); signal.Stop(sink) })
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for !cond() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
			}
		}
	}()
}

func slowGateway(t *testing.T) *fakeGateway {
	t.Helper()
	return newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: 200, Body: okBody(), Delay: 10 * time.Second}}, infoRules("paid")...))
}

func TestS11_SIGINTDuringAProbeExits130WithNoOutputWithin2Seconds(t *testing.T) {
	g := slowGateway(t)
	signalWhen(t, func() bool { return len(g.CallsTo("POST", chatPath)) > 0 })
	res, ok := within(2*time.Second+time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel)
	})
	if !ok {
		t.Fatal("run did not return within 2 s of the signal")
	}
	if res.Code != 130 || res.Stdout != "" || res.Stderr != "" {
		t.Errorf("exit=%d stdout=%q stderr=%q, want 130 and no output", res.Code, res.Stdout, res.Stderr)
	}
}

func TestS11_SIGINTDuringATUIProbeExits130(t *testing.T) {
	g := slowGateway(t)
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	signalWhen(t, func() bool { return len(g.CallsTo("POST", chatPath)) > 0 })
	res, ok := within(3*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: gwEnv(g, canaryKey), StdinR: pr, StdinTTY: true, StdoutTTY: true}, "probe", probeModel)
	})
	if !ok {
		t.Fatal("run did not return within 2 s of the signal")
	}
	if res.Code != 130 {
		t.Errorf("exit = %d stderr=%q, want 130", res.Code, res.Stderr)
	}
}

func TestS11_SIGINTExits130WithinTwoSecondsOnAnIdleFIFOKeysFile(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "keys.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { // releases a reader that a regression left blocked in open
		if f, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	})
	g := newFakeGateway(t, nil)
	signalWhen(t, func() bool { return true })
	res, ok := within(2*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "models", "--keys-file", fifo)
	})
	if !ok {
		t.Fatal("run did not return within 2 s while the keys file blocked")
	}
	if res.Code != 130 || res.Stdout != "" || res.Stderr != "" {
		t.Errorf("exit=%d stdout=%q stderr=%q, want 130 and no output", res.Code, res.Stdout, res.Stderr)
	}
}

// within runs f and reports whether it returned before d; a late result is drained in the background.
func within(d time.Duration, f func() cliResult) (cliResult, bool) {
	done := make(chan cliResult, 1)
	go func() { done <- f() }()
	select {
	case r := <-done:
		return r, true
	case <-time.After(d):
		return cliResult{}, false
	}
}
