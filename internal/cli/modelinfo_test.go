/**
 * @spec-handoff
 * @interface litegauge models -o json|table and litegauge probe MODEL -o json, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - Characterization of how /v1/model/info entries become a price class and a mode; these tests pin today's behaviour
 *   - Entries with the same model_name keep a field (mode, input price, output price) only when all of them state the same value
 *   - A field every entry states alike is used; a field the entries disagree on is treated as not stated
 *   - Price: both prices 0 is free, both known with one above 0 is paid, anything else (a missing, negative or non-numeric price) is unknown
 *   - Probe on a 2xx: ok only for paid; free or unknown price is key_valid (ok*); both exit 0
 *   - Mode: a model is listed unless its agreed mode is not chat, completion or responses; an unstated or disputed mode is listed
 * @edge-cases
 *   - One field of the wrong JSON type is read as not stated; the other fields of that entry and the other entries still count
 *   - Disagreeing modes (image first, chat second) leave the model listed
 * @see ./classify_test.go
 */

package cli

import (
	"slices"
	"strings"
	"testing"
)

type listedModel struct{ Model, Price string }

// infoGateway serves the given ids on /v1/models and the given entries on /v1/model/info, and answers every probe with 200.
func infoGateway(t *testing.T, ids []string, entries ...infoEntry) *fakeGateway {
	t.Helper()
	return newFakeGateway(t, []fakeRule{
		{Path: chatPath, Status: 200, Body: okBody()},
		{Path: "/v1/models", Status: 200, Body: modelsBody("openai", ids...)},
		{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(entries...)},
		plain404("/model/info"),
	})
}

func listedByModels(t *testing.T, g *fakeGateway) []listedModel {
	t.Helper()
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models", "-o", "json")
	if res.Code != 0 {
		t.Fatalf("models exit %d, stderr=%q", res.Code, res.Stderr)
	}
	var out []listedModel
	for _, m := range decodeDoc(t, res.Stdout).Models {
		out = append(out, listedModel{m.Model, m.Price})
	}
	return out
}

func probeOf(t *testing.T, g *fakeGateway, model string) outResult {
	t.Helper()
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", model, "-o", "json")
	d := decodeDoc(t, res.Stdout)
	if len(d.Results) != 1 || res.Code != 0 {
		t.Fatalf("probe %s: exit %d, stdout=%q", model, res.Code, res.Stdout)
	}
	return d.Results[0]
}

func withDecoys(e infoEntry, k string, v any) infoEntry {
	e.Decoys = map[string]any{k: v}
	return e
}

func named(e infoEntry, name string) infoEntry {
	e.Name = name
	return e
}

func TestModelInfo_EntriesWithTheSameNameKeepOnlyWhatTheyAgreeOn(t *testing.T) {
	paid2 := infoEntry{In: pf(4e-06), Out: pf(1.5e-05), Mode: "chat"}
	noPrice := infoEntry{Mode: "chat"}
	cases := []struct {
		name    string
		entries []infoEntry
		price   string
		verdict string
	}{
		{"agree_paid", []infoEntry{paidInfo("m"), paidInfo("m")}, "paid", "ok"},
		{"agree_free", []infoEntry{freeInfo("m"), freeInfo("m")}, "free", "key_valid"},
		{"paid_and_free_is_unknown", []infoEntry{paidInfo("m"), freeInfo("m")}, "unknown", "key_valid"},
		{"free_and_paid_is_unknown", []infoEntry{freeInfo("m"), paidInfo("m")}, "unknown", "key_valid"},
		{"paid_and_missing_price_is_unknown", []infoEntry{paidInfo("m"), named(noPrice, "m")}, "unknown", "key_valid"},
		{"two_different_paid_prices_is_unknown", []infoEntry{paidInfo("m"), named(paid2, "m")}, "unknown", "key_valid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := infoGateway(t, []string{"m"}, c.entries...)
			if got := listedByModels(t, g); !slices.Equal(got, []listedModel{{"m", c.price}}) {
				t.Errorf("models = %v, want m %s", got, c.price)
			}
			if r := probeOf(t, g, "m"); r.Price != c.price || r.Verdict != c.verdict {
				t.Errorf("probe price=%q verdict=%q, want %q %q", r.Price, r.Verdict, c.price, c.verdict)
			}
		})
	}
}

func TestModelInfo_DisagreeingPricePrintsUnknownInTheModelsTable(t *testing.T) {
	g := infoGateway(t, []string{"agreed", "disputed"},
		paidInfo("agreed"), paidInfo("agreed"), paidInfo("disputed"), freeInfo("disputed"))
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "models", "-o", "table")
	rows := map[string]string{}
	for _, line := range strings.Split(stripANSI(res.Stdout), "\n") {
		if f := strings.SplitN(strings.TrimSpace(line), " ", 2); len(f) == 2 {
			rows[f[0]] = strings.TrimSpace(f[1])
		}
	}
	if rows["agreed"] != "paid" || rows["disputed"] != "unknown" {
		t.Errorf("price column = %v, want agreed paid and disputed unknown\n%s", rows, res.Stdout)
	}
}

func TestModelInfo_EntriesWithTheSameNameAndDifferentModesLeaveTheModelListed(t *testing.T) {
	img := infoEntry{Name: "split", In: pf(3e-06), Out: pf(1.5e-05), Mode: "image_generation"}
	chat := paidInfo("split")
	bothImg := infoEntry{Name: "image-only", In: pf(3e-06), Out: pf(1.5e-05), Mode: "image_generation"}
	g := infoGateway(t, []string{"split", "image-only", "chat-only"}, img, chat, bothImg, bothImg, paidInfo("chat-only"))
	want := []listedModel{{"split", "paid"}, {"chat-only", "paid"}}
	if got := listedByModels(t, g); !slices.Equal(got, want) {
		t.Errorf("models = %v, want %v (disputed mode stays listed, agreed image mode is dropped)", got, want)
	}
}

func TestModelInfo_OneFieldOfTheWrongTypeIsDroppedAndTheRestOfTheEntryStays(t *testing.T) {
	img := func(e infoEntry) infoEntry { e.Mode = "image_generation"; return e }
	ids := []string{"in-is-string", "mode-is-number", "max-is-object", "out-is-string-image", "control"}
	g := infoGateway(t, ids,
		withDecoys(paidInfo("in-is-string"), "input_cost_per_token", "3e-06"),
		withDecoys(paidInfo("mode-is-number"), "mode", 7),
		withDecoys(img(paidInfo("max-is-object")), "max_tokens", map[string]any{"n": 1}),
		withDecoys(img(paidInfo("out-is-string-image")), "output_cost_per_token", "1.5e-05"),
		paidInfo("control"),
	)
	want := []listedModel{
		{"in-is-string", "unknown"}, // bad price dropped, mode chat kept, entry still listed
		{"mode-is-number", "paid"},  // bad mode dropped (listed as unstated), price kept
		{"control", "paid"},         // the other entries are still used
	}
	if got := listedByModels(t, g); !slices.Equal(got, want) {
		t.Errorf("models = %v, want %v (image mode must survive a bad max_tokens or a bad price)", got, want)
	}
	if r := probeOf(t, g, "mode-is-number"); r.Price != "paid" || r.Verdict != "ok" {
		t.Errorf("probe mode-is-number price=%q verdict=%q, want paid ok", r.Price, r.Verdict)
	}
	if r := probeOf(t, g, "in-is-string"); r.Price != "unknown" || r.Verdict != "key_valid" {
		t.Errorf("probe in-is-string price=%q verdict=%q, want unknown key_valid", r.Price, r.Verdict)
	}
}

func TestModelInfo_NegativeOrNonNumericPriceIsUnknown(t *testing.T) {
	cases := []struct {
		name    string
		in, out any
	}{
		{"negative_input", -3e-06, 1.5e-05},
		{"negative_output_with_free_input", 0.0, -1.0},
		{"negative_both", -1.0, -1.0},
		{"string_input", "3e-06", 1.5e-05},
		{"null_output", 3e-06, nil},
		{"bool_input", true, 1.5e-05},
		{"object_output", 3e-06, map[string]any{"v": 1}},
	}
	var ids []string
	var entries []infoEntry
	for _, c := range cases {
		ids = append(ids, c.name)
		entries = append(entries, infoEntry{Name: c.name, Mode: "chat", Decoys: map[string]any{"input_cost_per_token": c.in, "output_cost_per_token": c.out}})
	}
	g := infoGateway(t, ids, entries...)
	got := listedByModels(t, g)
	if len(got) != len(cases) {
		t.Fatalf("models = %v, want all %d listed", got, len(cases))
	}
	for _, m := range got {
		if m.Price != "unknown" {
			t.Errorf("%s: price = %q, want unknown", m.Model, m.Price)
		}
	}
	for _, name := range []string{"negative_input", "negative_output_with_free_input"} {
		if r := probeOf(t, g, name); r.Price != "unknown" || r.Verdict != "key_valid" {
			t.Errorf("probe %s price=%q verdict=%q, want unknown key_valid", name, r.Price, r.Verdict)
		}
	}
}
