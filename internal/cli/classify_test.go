/**
 * @spec-handoff
 * @interface litegauge probe MODEL -o json|table, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - Each response row of contract section 4 maps to one verdict (json English enum), one http_status and one exit code
 *   - 2xx on a paid model without X-Litellm-Cache-Key is ok; free or unknown price, or a cache hit, is key_valid; both exit 0
 *   - The first matching row wins; among the error rows error.type and the message beat the HTTP status; the E003 message decides blocked and is never printed
 *   - 3xx is returned as error and never followed; 429, 5xx and a timeout are error; http_status is 0 when no response arrived
 *   - A failed models listing prints "Error: could not list the models (HTTP 401)." on stderr and exits 1
 * @edge-cases
 *   - A 5xx or transport error is retried once, so the gateway sees two POSTs
 *   - matrix exits 0 when every cell is ok or ok*, 1 when any cell is not
 *   - matrix over 3 models x 2 keys with Pace 40 ms and InFlight 2: at most 2 requests in flight, consecutive request starts at least 35 ms apart
 * @see ./security_test.go
 */

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shoootyou/litegauge/internal/gateway"
)

const (
	probeModel = "claude-sonnet-5-5"
	chatPath   = "/v1/chat/completions"
)

type outResult struct {
	Key        string `json:"key"`
	Model      string `json:"model"`
	Group      string `json:"group"`
	Price      string `json:"price"`
	Verdict    string `json:"verdict"`
	HTTPStatus int    `json:"http_status"`
	LatencyMS  int    `json:"latency_ms"`
}

type outDoc struct {
	Results []outResult `json:"results"`
	Keys    []string    `json:"keys"`
	Models  []struct {
		Model string `json:"model"`
		Group string `json:"group"`
		Price string `json:"price"`
	} `json:"models"`
}

func decodeDoc(t *testing.T, out string) outDoc {
	t.Helper()
	var d outDoc
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%q", err, out)
	}
	return d
}

func okBody() string {
	return `{"id":"chatcmpl-RAWBODY1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"x"}}]}`
}

// infoRules answers model/info for probeModel with a paid, free or (anything else) unknown price.
func infoRules(kind string) []fakeRule {
	switch kind {
	case "paid":
		return []fakeRule{{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(paidInfo(probeModel))}, plain404("/model/info")}
	case "free":
		return []fakeRule{{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(freeInfo(probeModel))}, plain404("/model/info")}
	}
	return []fakeRule{plain404("/v1/model/info"), plain404("/model/info")}
}

// chatGateway answers every chat request with one status, body and header set.
func chatGateway(t *testing.T, price string, status int, body string, h http.Header) *fakeGateway {
	t.Helper()
	return newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: status, Body: body, Header: h}}, infoRules(price)...))
}

func probeJSON(t *testing.T, g *fakeGateway, o cliOpts) (cliResult, outResult) {
	t.Helper()
	if o.Env == nil {
		o.Env = gwEnv(g, canaryKey)
	}
	res := runCLI(t, o, "probe", probeModel, "-o", "json")
	d := decodeDoc(t, res.Stdout)
	if len(d.Results) != 1 {
		t.Fatalf("probe -o json must hold one result, got %d: %q", len(d.Results), res.Stdout)
	}
	return res, d.Results[0]
}

func TestClassify_EveryResponseRowMapsToItsVerdictStatusAndExit(t *testing.T) {
	cache := http.Header{"X-Litellm-Cache-Key": {"abc"}}
	cases := []struct {
		name    string
		price   string
		status  int
		body    string
		hdr     http.Header
		verdict string
		exit    int
	}{
		{"2xx_paid_is_ok", "paid", 200, okBody(), nil, "ok", 0},
		{"2xx_free_is_key_valid", "free", 200, okBody(), nil, "key_valid", 0},
		{"2xx_unknown_price_is_key_valid", "unknown", 200, okBody(), nil, "key_valid", 0},
		{"2xx_paid_cache_hit_is_key_valid", "paid", 200, okBody(), cache, "key_valid", 0},
		{"budget_exceeded_is_no_budget", "paid", 422, errJSON("budget_exceeded", "422", "m"), nil, "no_budget", 1},
		{"budget_exceeded_wins_over_401", "paid", 401, errJSON("budget_exceeded", "", "m"), nil, "no_budget", 1},
		{"model_access_denied_type_is_blocked", "paid", 403, errJSON("team_model_access_denied", "", "m"), nil, "blocked", 1},
		{"E003_400_invalid_request_is_blocked", "paid", 400, errJSON("invalid_request_error", "", "E003: policy"), nil, "blocked", 1},
		{"E003_403_empty_type_is_blocked", "paid", 403, errJSON("", "", "E003 blocked"), nil, "blocked", 1},
		{"400_invalid_request_without_E003_is_error", "paid", 400, errJSON("invalid_request_error", "", "bad param"), nil, "error", 1},
		{"400_empty_type_without_E003_is_error", "paid", 400, errJSON("", "", "bad param"), nil, "error", 1},
		{"403_empty_type_without_E003_is_invalid_key", "paid", 403, errJSON("", "", "bad param"), nil, "invalid_key", 1},
		{"403_invalid_request_without_E003_is_invalid_key", "paid", 403, errJSON("invalid_request_error", "", "bad param"), nil, "invalid_key", 1},
		{"E003_with_a_numeric_type_does_not_block", "paid", 400, `{"error":{"type":400,"message":"E003 policy"}}`, nil, "error", 1},
		{"E003_with_status_401_is_invalid_key", "paid", 401, errJSON("", "", "E003 policy"), nil, "invalid_key", 1},
		{"E003_with_status_404_is_model_not_found", "paid", 404, errJSON("", "", "E003 policy"), nil, "model_not_found", 1},
		{"model_access_denied_wins_over_401", "paid", 401, errJSON("team_model_access_denied", "", "m"), nil, "blocked", 1},
		{"type_without_the_underscore_prefix_is_not_blocked", "paid", 403, errJSON("model_access_denied", "", "m"), nil, "invalid_key", 1},
		{"type_with_a_suffix_after_access_denied_is_not_blocked", "paid", 403, errJSON("team_model_access_denied_x", "", "m"), nil, "invalid_key", 1},
		{"budget_exceeded_wins_over_400", "paid", 400, errJSON("budget_exceeded", "", "m"), nil, "no_budget", 1},
		{"budget_exceeded_on_399_is_error", "paid", 399, errJSON("budget_exceeded", "", "m"), nil, "error", 1},
		{"budget_exceeded_on_300_is_error", "paid", 300, errJSON("budget_exceeded", "", "m"), nil, "error", 1},
		{"299_paid_is_ok", "paid", 299, okBody(), nil, "ok", 0},
		{"auth_error_type_is_invalid_key", "paid", 400, errJSON("authentication_error", "", "m"), nil, "invalid_key", 1},
		{"auth_error_short_type_is_invalid_key", "paid", 400, errJSON("auth_error", "", "m"), nil, "invalid_key", 1},
		{"expired_key_type_is_invalid_key", "paid", 400, errJSON("expired_key", "", "m"), nil, "invalid_key", 1},
		{"token_not_found_in_db_type_is_invalid_key", "paid", 400, errJSON("token_not_found_in_db", "", "m"), nil, "invalid_key", 1},
		{"401_wins_over_not_found_type", "paid", 401, errJSON("not_found_error", "", "m"), nil, "invalid_key", 1},
		{"401_wins_over_model_not_found_code", "paid", 401, errJSON("", "model_not_found", "m"), nil, "invalid_key", 1},
		{"403_wins_over_not_found_type", "paid", 403, errJSON("not_found_error", "", "m"), nil, "invalid_key", 1},
		{"auth_type_wins_over_404", "paid", 404, errJSON("auth_error", "", "m"), nil, "invalid_key", 1},
		{"model_not_found_code_is_model_not_found", "paid", 400, errJSON("", "model_not_found", "m"), nil, "model_not_found", 1},
		{"404_with_an_object_without_error_is_error", "paid", 404, `{"detail":"x"}`, nil, "error", 1},
		{"404_with_a_string_error_is_error", "paid", 404, `{"error":"x"}`, nil, "error", 1},
		{"401_without_json_is_invalid_key", "paid", 401, "Unauthorized", nil, "invalid_key", 1},
		{"not_found_error_type_is_model_not_found", "paid", 400, errJSON("not_found_error", "", "m"), nil, "model_not_found", 1},
		{"404_with_json_error_is_model_not_found", "paid", 404, errJSON("", "", "m"), nil, "model_not_found", 1},
		{"404_plain_text_is_error", "paid", 404, "404 page not found", nil, "error", 1},
		{"429_is_error", "paid", 429, errJSON("rate_limit_error", "", "m"), nil, "error", 1},
		{"5xx_is_error", "paid", 503, "upstream down", nil, "error", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := chatGateway(t, c.price, c.status, c.body, c.hdr)
			res, r := probeJSON(t, g, cliOpts{})
			if r.Verdict != c.verdict || r.HTTPStatus != c.status || res.Code != c.exit {
				t.Errorf("verdict=%q http_status=%d exit=%d, want %q %d %d (stderr=%q)", r.Verdict, r.HTTPStatus, res.Code, c.verdict, c.status, c.exit, res.Stderr)
			}
		})
	}
}

func TestClassify_E003MustBeASeparateTokenToBlock(t *testing.T) {
	cases := []struct {
		msg     string
		verdict string
	}{
		{"E003", "blocked"},
		{"code E003 here", "blocked"},
		{"(E003)", "blocked"},
		{"AE003", "error"},
		{"aE003", "error"},
		{"1E003", "error"},
		{"E003A", "error"},
		{"E003a", "error"},
		{"E0031", "error"},
		{"e003", "error"},
	}
	for _, c := range cases {
		t.Run(c.msg, func(t *testing.T) {
			g := chatGateway(t, "paid", 400, errJSON("invalid_request_error", "", c.msg), nil)
			_, r := probeJSON(t, g, cliOpts{})
			if r.Verdict != c.verdict {
				t.Errorf("message %q: verdict=%q, want %q", c.msg, r.Verdict, c.verdict)
			}
		})
	}
}

func TestClassify_ServerErrorIsRetriedOnce(t *testing.T) {
	g := chatGateway(t, "paid", 503, "upstream down", nil)
	probeJSON(t, g, cliOpts{})
	if n := len(g.CallsTo("POST", chatPath)); n != 2 {
		t.Errorf("POSTs = %d, want 2 (one attempt and one retry)", n)
	}
}

func TestClassify_TimeoutIsErrorWithStatusZero(t *testing.T) {
	g := newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: 200, Body: okBody(), Delay: 5 * time.Second}}, infoRules("paid")...))
	o := cliOpts{Timing: gateway.Timing{Attempt: 150 * time.Millisecond, Retry: 5 * time.Millisecond, InFlight: 2}}
	res, r := probeJSON(t, g, o)
	if r.Verdict != "error" || r.HTTPStatus != 0 || res.Code != 1 {
		t.Errorf("verdict=%q http_status=%d exit=%d, want error 0 1", r.Verdict, r.HTTPStatus, res.Code)
	}
}

func TestClassify_RedirectIsErrorAndNotFollowed(t *testing.T) {
	other := newFakeGateway(t, []fakeRule{{Path: "/landing", Status: 200, Body: "{}"}})
	g := chatGateway(t, "paid", 302, "", http.Header{"Location": {other.URL + "/landing"}})
	res, r := probeJSON(t, g, cliOpts{})
	if r.Verdict != "error" || r.HTTPStatus != 302 || res.Code != 1 {
		t.Errorf("verdict=%q http_status=%d exit=%d, want error 302 1", r.Verdict, r.HTTPStatus, res.Code)
	}
	if n := len(other.Calls()); n != 0 {
		t.Errorf("the redirect target received %d requests, want 0", n)
	}
}

func TestClassify_E003MessageDecidesButIsNeverPrinted(t *testing.T) {
	msg := "E003 POLICY-SENTENCE-CANARY"
	g := chatGateway(t, "paid", 400, errJSON("invalid_request_error", "", msg), nil)
	for _, mode := range []string{"table", "json", "yaml"} {
		res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel, "-o", mode)
		if res.Code != 1 || res.Stdout == "" || len(g.CallsTo("POST", chatPath)) == 0 {
			t.Fatalf("-o %s: the probe never ran as set up: exit=%d stdout=%q", mode, res.Code, res.Stdout)
		}
		assertNoText(t, "-o "+mode, res.Stdout+res.Stderr, "E003", "POLICY-SENTENCE-CANARY")
	}
}

func TestClassify_ErrorDetailNamesTheCauseInTheTable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		hdr    http.Header
		delay  time.Duration
		want   string
	}{
		{"rate_limit", 429, nil, 0, "rate limit"},
		{"server_error", 500, nil, 0, "server error"},
		{"no_response", 200, nil, 5 * time.Second, "no response"},
		{"redirect", 302, http.Header{"Location": {"http://127.0.0.1:1/x"}}, 0, "redirect not followed"},
		{"redirect_lowest_3xx", 300, http.Header{"Location": {"http://127.0.0.1:1/x"}}, 0, "redirect not followed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: c.status, Body: "x", Header: c.hdr, Delay: c.delay}}, infoRules("paid")...))
			o := cliOpts{Env: gwEnv(g, canaryKey), Timing: gateway.Timing{Attempt: 150 * time.Millisecond, Retry: 5 * time.Millisecond, InFlight: 2}}
			res := runCLI(t, o, "probe", probeModel)
			if !strings.Contains(strings.ToLower(res.Stdout), c.want) {
				t.Errorf("Detail lacks %q:\n%s", c.want, res.Stdout)
			}
		})
	}
}

func TestExit_FailedModelsListingPrintsTheStatusAndExits1(t *testing.T) {
	g := newFakeGateway(t, []fakeRule{{Path: "/v1/models", Status: 401, Body: errJSON("auth_error", "401", "bad key")}})
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models")
	if res.Code != 1 || res.Stdout != "" || strings.TrimSpace(res.Stderr) != "Error: could not list the models (HTTP 401)." {
		t.Errorf("exit=%d stdout=%q stderr=%q", res.Code, res.Stdout, res.Stderr)
	}
}

func matrixGateway(t *testing.T, chat []fakeRule) *fakeGateway {
	t.Helper()
	rules := append(chat,
		fakeRule{Path: "/v1/models", Status: 200, Body: modelsBody("openai", "claude-haiku-5-5", probeModel)},
		fakeRule{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(freeInfo("claude-haiku-5-5"), paidInfo(probeModel))},
		plain404("/model/info"))
	return newFakeGateway(t, rules)
}

func TestExit_MatrixWithOnlyOkAndOkStarCellsExits0(t *testing.T) {
	g := matrixGateway(t, []fakeRule{{Path: chatPath, Status: 200, Body: okBody()}})
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "matrix", "-o", "json")
	if res.Code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", res.Code, res.Stderr)
	}
	got := map[string]string{}
	for _, r := range decodeDoc(t, res.Stdout).Results {
		got[r.Model] = r.Verdict
	}
	if got["claude-haiku-5-5"] != "key_valid" || got[probeModel] != "ok" {
		t.Errorf("verdicts = %v, want haiku key_valid and sonnet ok", got)
	}
}

func TestExit_MatrixWithOneFailedCellExits1(t *testing.T) {
	g := matrixGateway(t, []fakeRule{
		{Path: chatPath, Model: probeModel, Status: 422, Body: errJSON("budget_exceeded", "422", "m")},
		{Path: chatPath, Status: 200, Body: okBody()},
	})
	if res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "matrix", "-o", "json"); res.Code != 1 {
		t.Errorf("exit = %d, want 1 (stderr=%q)", res.Code, res.Stderr)
	}
}

func TestM3_MatrixPacesRequestStartsAndKeepsAtMostTwoInFlight(t *testing.T) {
	// 100 ms of server time per request makes the two-slot limit bite: starts at 0, 40 and 80 ms would overlap three deep without it.
	g := newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: 200, Body: okBody(), Delay: 100 * time.Millisecond}}, infoRules("paid")...))
	o := cliOpts{
		Env:    map[string]string{"LITEGAUGE_URL": g.URL},
		Stdin:  "Alpha=sk-FAKE-PACE-0001\nBravo=sk-FAKE-PACE-0002\n",
		Timing: gateway.Timing{Attempt: 3 * time.Second, Pace: 40 * time.Millisecond, Retry: 5 * time.Millisecond, InFlight: 2},
	}
	res := runCLI(t, o, "matrix", "--models", probeModel+",claude-haiku-5-5,claude-opus-5-5", "--keys-file", "-", "-o", "json")
	if res.Code != 0 {
		t.Fatalf("exit = %d stderr=%q", res.Code, res.Stderr)
	}
	posts := g.CallsTo("POST", chatPath)
	if len(posts) != 6 {
		t.Fatalf("POSTs = %d, want 3 models x 2 keys = 6", len(posts))
	}
	if m := g.MaxInFlight(); m > 2 || m < 2 {
		t.Errorf("max in flight = %d, want 2 (never above the limit, and the limit reached)", m)
	}
	starts := make([]time.Time, len(posts))
	for i, c := range posts {
		starts[i] = c.At
	}
	if !slices.IsSortedFunc(starts, func(a, b time.Time) int { return a.Compare(b) }) {
		t.Fatal("the fake did not record the starts in order")
	}
	for _, msg := range pacingGapErrors(starts, o.Timing.Pace) {
		t.Error(msg)
	}
}

// pacingGapErrors checks consecutive request arrivals against the client's start spacing. The fake stamps
// arrival, not the client's start, so the slack allows scheduler and loopback jitter; without pacing the gap is about 0.
func pacingGapErrors(starts []time.Time, pace time.Duration) []string {
	const slack = 15 * time.Millisecond // deliberate jitter tolerance: a regression that spaces starts between pace-slack and pace is not caught, the absence of pacing is
	var errs []string
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < pace-slack {
			errs = append(errs, fmt.Sprintf("starts %d and %d are %v apart, want at least %v", i-1, i, gap, pace-slack))
		}
	}
	return errs
}

func TestM3_PacingCheckToleratesArrivalJitterButNotMissingPacing(t *testing.T) {
	at := func(ms ...int) []time.Time {
		base := time.Now()
		out := make([]time.Time, len(ms))
		for i, m := range ms {
			out[i] = base.Add(time.Duration(m) * time.Millisecond)
		}
		return out
	}
	const pace = 40 * time.Millisecond
	if errs := pacingGapErrors(at(8, 40, 80), pace); len(errs) != 0 { // the first arrival 8 ms late leaves a 32 ms gap
		t.Errorf("an 8 ms late arrival was rejected: %v", errs)
	}
	if errs := pacingGapErrors(at(0, 1, 2), pace); len(errs) != 2 {
		t.Errorf("unpaced starts: %d errors, want 2", len(errs))
	}
}
