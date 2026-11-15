/**
 * @spec-handoff
 * @interface litegauge models, matrix and probe, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - The model listing and the model info answer are read up to exactly 1 MiB; the probe error body up to exactly 64 KiB
 *   - A model info answer cut at the cap is unparsable: the next path is tried, and with none usable every price is unknown
 *   - The listing takes only a 2xx status; a 3xx or 4xx with a valid-looking list is the listing-failed sentence, exit 1, nothing printed
 *   - Model info takes only a 200; any other status (2xx included) is skipped
 *   - A probe answers ok/key_valid on 200-299, error on 300-399 whatever the body says, and retries once only from status 500 up
 * @edge-cases
 *   - A body of exactly the cap is read whole; with one byte more the limit reader drops the final closing brace, so the JSON stays broken
 *   - Status 199 cannot be tested: the Go server sends 1xx as an interim response and the client skips it
 * @see ./security_test.go
 */

package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shoootyou/litegauge/internal/gateway"
)

// sized is prefix + 'a' padding + suffix, n bytes long in all.
func sized(prefix, suffix string, n int) string {
	return prefix + strings.Repeat("a", n-len(prefix)-len(suffix)) + suffix
}

const (
	listingHead = `{"data":[{"id":"x"}],"pad":"`
	infoHead    = `{"data":[{"model_name":"x","model_info":{"mode":"chat","input_cost_per_token":3e-06,"output_cost_per_token":1.5e-05,"pad":"`
	infoTail    = `"}}]}`
	errHead     = `{"error":{"type":"budget_exceeded","message":"`
	errTail     = `"}}`
	listSentFmt = "Error: could not list the models (HTTP %d)."
)

func TestLimits_ModelsListingCapIsExactly1MiB(t *testing.T) {
	run := func(n int) cliResult {
		g := newFakeGateway(t, []fakeRule{{Path: "/v1/models", Status: 200, Body: sized(listingHead, `"}`, n)}, plain404("/v1/model/info"), plain404("/model/info")})
		return runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models", "-o", "json")
	}
	if res := run(1 << 20); res.Code != 0 {
		t.Errorf("a listing of exactly 1 MiB exit = %d stderr=%q, want 0", res.Code, res.Stderr)
	}
	res := run(1<<20 + 1)
	if want := fmt.Sprintf(listSentFmt, 200); res.Code != 1 || res.Stdout != "" || strings.TrimSpace(res.Stderr) != want {
		t.Errorf("a listing of 1 MiB + 1 exit=%d stdout=%q stderr=%q, want 1, nothing, %q", res.Code, res.Stdout, res.Stderr, want)
	}
}

func TestLimits_ProbeErrorBodyCapIsExactly64KiB(t *testing.T) {
	for _, c := range []struct {
		name string
		n    int
		want string
	}{{"exactly_64KiB", 64 << 10, "no_budget"}, {"64KiB_plus_1", 64<<10 + 1, "error"}} {
		t.Run(c.name, func(t *testing.T) {
			_, r := probeJSON(t, chatGateway(t, "paid", 422, sized(errHead, errTail, c.n), nil), cliOpts{})
			if r.Verdict != c.want {
				t.Errorf("verdict = %q, want %q", r.Verdict, c.want)
			}
		})
	}
}

// The model info answer is cut at 1 MiB: what is left cannot be parsed, so the fallback path is tried.
func TestLimits_ModelInfoCapIsExactly1MiBAndACutAnswerFallsThroughToTheNextPath(t *testing.T) {
	price := func(t *testing.T, n int, fallback fakeRule) string {
		t.Helper()
		g := newFakeGateway(t, []fakeRule{
			{Path: "/v1/models", Status: 200, Body: modelsBody("openai", "x")},
			{Path: "/v1/model/info", Status: 200, Body: sized(infoHead, infoTail, n)},
			fallback,
		})
		got := listedByModels(t, g)
		if len(got) != 1 || got[0].Model != "x" {
			t.Fatalf("models = %v, want x only", got)
		}
		return got[0].Price
	}
	free := fakeRule{Path: "/model/info", Status: 200, Body: modelInfoBody(freeInfo("x"))}
	if p := price(t, 1<<20, free); p != "paid" {
		t.Errorf("model info of exactly 1 MiB: price = %q, want paid (read whole)", p)
	}
	if p := price(t, 1<<20+1, free); p != "free" {
		t.Errorf("model info of 1 MiB + 1: price = %q, want free (cut answer skipped, /model/info used)", p)
	}
	if p := price(t, 1<<20+1, plain404("/model/info")); p != "unknown" {
		t.Errorf("model info of 1 MiB + 1 with no fallback: price = %q, want unknown", p)
	}
}

func TestLimits_ModelInfoTakesOnlyA200(t *testing.T) {
	// No 204: the Go server drops a 204 body, so the price is unknown under any rule.
	for _, st := range []int{200, 201, 299, 300, 301, 302, 399, 400} {
		t.Run(fmt.Sprint(st), func(t *testing.T) {
			g := newFakeGateway(t, []fakeRule{
				{Path: "/v1/models", Status: 200, Body: modelsBody("openai", "x")},
				{Path: "/v1/model/info", Status: st, Body: modelInfoBody(paidInfo("x"))},
				plain404("/model/info"),
			})
			want := "unknown"
			if st == 200 {
				want = "paid"
			}
			if got := listedByModels(t, g); len(got) != 1 || got[0].Price != want {
				t.Errorf("status %d: models = %v, want x %s", st, got, want)
			}
		})
	}
}

func TestLimits_ModelsListingTakesOnly2xx(t *testing.T) {
	list := modelsBody("openai", "x")
	for _, st := range []int{200, 299, 300, 301, 302, 399, 400} {
		ok := st < 300
		t.Run(fmt.Sprint(st), func(t *testing.T) {
			rules := []fakeRule{
				{Path: "/v1/models", Status: st, Body: list},
				{Path: chatPath, Status: 200, Body: okBody()},
				plain404("/v1/model/info"), plain404("/model/info"),
			}
			want := fmt.Sprintf(listSentFmt, st)
			for _, cmd := range []string{"models", "matrix"} {
				res := runCLI(t, cliOpts{Env: gwEnv(newFakeGateway(t, rules), canaryKey)}, cmd, "-o", "json")
				if ok {
					if res.Code != 0 || !strings.Contains(res.Stdout, `"x"`) {
						t.Errorf("%s with status %d: exit=%d stdout=%q stderr=%q, want 0 and x listed", cmd, st, res.Code, res.Stdout, res.Stderr)
					}
					continue
				}
				if res.Code != 1 || res.Stdout != "" || strings.TrimSpace(res.Stderr) != want {
					t.Errorf("%s with status %d: exit=%d stdout=%q stderr=%q, want 1, nothing, %q", cmd, st, res.Code, res.Stdout, res.Stderr, want)
				}
			}
		})
	}
}

func TestLimits_ProbeVerdictFollowsTheStatusClass(t *testing.T) {
	budget := errJSON("budget_exceeded", "", "m")
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{200, okBody(), "ok"},
		{299, okBody(), "ok"},
		{300, budget, "error"},
		{399, budget, "error"},
		{400, budget, "no_budget"},
		{400, "", "error"},
		{401, "", "invalid_key"},
		{403, "", "invalid_key"},
		{404, errJSON("", "", "gone"), "model_not_found"},
		{404, "gone", "error"},
		{402, "", "error"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status, "_", c.want, "_", c.body == ""), func(t *testing.T) {
			if _, r := probeJSON(t, chatGateway(t, "paid", c.status, c.body, nil), cliOpts{}); r.Verdict != c.want || r.HTTPStatus != c.status {
				t.Errorf("status %d: verdict=%q http_status=%d, want %q", c.status, r.Verdict, r.HTTPStatus, c.want)
			}
		})
	}
}

func TestLimits_ProbeRetriesOnlyFromStatus500Up(t *testing.T) {
	for _, c := range []struct{ status, posts int }{{499, 1}, {500, 2}, {501, 2}, {599, 2}} {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			g := chatGateway(t, "paid", c.status, "x", nil)
			probeJSON(t, g, cliOpts{})
			if n := len(g.CallsTo("POST", chatPath)); n != c.posts {
				t.Errorf("status %d: %d chat requests, want %d", c.status, n, c.posts)
			}
		})
	}
}

func TestLimits_ModelsListingWithNoReplyIsTheNoReplySentence(t *testing.T) {
	g := newFakeGateway(t, []fakeRule{{Path: "/v1/models", Status: 200, Body: modelsBody("openai", "x"), Delay: 5 * time.Second}})
	o := cliOpts{Env: gwEnv(g, canaryKey), Timing: gateway.Timing{Attempt: 150 * time.Millisecond, Retry: 5 * time.Millisecond, InFlight: 2}}
	res := runCLI(t, o, "models", "-o", "json")
	if want := "Error: could not list the models (no response)."; res.Code != 1 || res.Stdout != "" || strings.TrimSpace(res.Stderr) != want {
		t.Errorf("exit=%d stdout=%q stderr=%q, want 1, nothing, %q", res.Code, res.Stdout, res.Stderr, want)
	}
}
