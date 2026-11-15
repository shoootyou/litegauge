/**
 * @spec-handoff
 * @interface litegauge probe MODEL | matrix | models, each with -o table|json|yaml, off a terminal
 * @behavior
 *   - Output equals the hand-written files in testdata/golden/ (latency is normalised to N ms, 0 in json and yaml)
 *   - Off a terminal, or with NO_COLOR, the table is plain text: no ANSI, no borders, columns padded to the widest cell and joined by two spaces, no trailing spaces
 *   - On a terminal without NO_COLOR the table is bordered (rounded corners, box lines); the run forces the colour the harness writer would drop
 *   - matrix prints one table per group (group name as a title line), then the "ok* = ..." legend; models prints one table per group with Model and Price
 *   - json is compact on one line, keys in the contract order; yaml is block style with double-quoted strings and the same data
 *   - matrix json holds "results" (row-major, one object per cell) then "keys"; models json holds "models"
 * @edge-cases
 *   - Embedding models are not listed (text models only); a model whose model_info states no mode is listed too
 *   - Default matrix models are the same set as models (text mode plus no stated mode), in listing order, all probed
 *   - A model without model_info has price "unknown"
 * @see ./testdata/golden
 */

package cli

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

var (
	latencyTextRe = regexp.MustCompile(`\d+ ms`)
	latencyDataRe = regexp.MustCompile(`("?latency_ms"?: ?)\d+`) // quoted in json, bare in yaml
)

func normalize(s string) string {
	return latencyDataRe.ReplaceAllString(latencyTextRe.ReplaceAllString(s, "N ms"), "${1}0")
}

const goldenKeys = "Charlie=sk-FAKE-GOLD-0003\nAlpha=sk-FAKE-GOLD-0001\nBravo=sk-FAKE-GOLD-0002\n" // not alphabetical: the matrix columns follow the file order

func goldenGateway(t *testing.T) *fakeGateway {
	t.Helper()
	ids := []string{"claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5", "gpt-example", "embed-4"}
	embed := infoEntry{Name: "embed-4", In: pf(1e-07), Out: pf(0), Mode: "embedding"}
	return newFakeGateway(t, []fakeRule{
		{Path: chatPath, Key: "sk-FAKE-GOLD-0003", Status: 401, Body: errJSON("auth_error", "401", "bad key")},
		{Path: chatPath, Model: "claude-opus-5-5", Status: 400, Body: errJSON("invalid_request_error", "", "E003 blocked by policy")},
		{Path: chatPath, Key: "sk-FAKE-GOLD-0002", Model: "claude-sonnet-5-5", Status: 422, Body: errJSON("budget_exceeded", "422", "m")},
		{Path: chatPath, Status: 200, Body: okBody()},
		{Path: "/v1/models", Status: 200, Body: modelsBody("openai", ids...)},
		{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(freeInfo("claude-haiku-5-5"), paidInfo("claude-sonnet-5-5"), paidInfo("claude-opus-5-5"), infoEntry{Name: "gpt-example"}, embed)}, // gpt-example states no mode and no price
		plain404("/model/info"),
	})
}

func goldenRun(t *testing.T, cmd, mode string, extra cliOpts) cliResult {
	t.Helper()
	g := goldenGateway(t)
	o := extra
	o.Env = map[string]string{"LITEGAUGE_URL": g.URL}
	for k, v := range extra.Env {
		o.Env[k] = v
	}
	args := []string{cmd, "-o", mode}
	switch cmd {
	case "probe":
		o.Env["LITEGAUGE_KEY"] = "sk-FAKE-GOLD-0002"
		args = []string{cmd, probeModel, "-o", mode}
	case "models":
		o.Env["LITEGAUGE_KEY"] = "sk-FAKE-GOLD-0001"
	default:
		o.Stdin = goldenKeys
		args = append(args, "--keys-file", "-")
	}
	return runCLI(t, o, args...)
}

// the probe gateway answers 422 budget_exceeded for the Bravo key on sonnet, so the probe golden is that cell.
func checkGolden(t *testing.T, cmd, mode string, wantExit int) {
	t.Helper()
	res := goldenRun(t, cmd, mode, cliOpts{})
	want := readFileT(t, td("golden", cmd+"_"+mode+".golden"))
	if got := normalize(res.Stdout); got != want {
		t.Errorf("%s -o %s differs from testdata/golden/%s_%s.golden\n--- got\n%s--- want\n%s", cmd, mode, cmd, mode, got, want)
	}
	if res.Code != wantExit || res.Stderr != "" {
		t.Errorf("exit=%d stderr=%q, want exit %d and no stderr", res.Code, res.Stderr, wantExit)
	}
}

func TestGolden_Probe(t *testing.T) {
	for _, mode := range []string{"table", "json", "yaml"} {
		t.Run(mode, func(t *testing.T) { checkGolden(t, "probe", mode, 1) })
	}
}

func TestGolden_Matrix(t *testing.T) {
	for _, mode := range []string{"table", "json", "yaml"} {
		t.Run(mode, func(t *testing.T) { checkGolden(t, "matrix", mode, 1) })
	}
}

func TestGolden_Models(t *testing.T) {
	for _, mode := range []string{"table", "json", "yaml"} {
		t.Run(mode, func(t *testing.T) { checkGolden(t, "models", mode, 0) })
	}
}

func TestOutput_NonTerminalTableHasNoANSIAndNoBorders(t *testing.T) {
	for _, cmd := range []string{"probe", "matrix", "models"} {
		res := goldenRun(t, cmd, "table", cliOpts{})
		if res.Stdout == "" {
			t.Fatalf("%s printed nothing (exit=%d stderr=%q)", cmd, res.Code, res.Stderr)
		}
		if res.Stdout != stripANSI(res.Stdout) || strings.ContainsAny(res.Stdout, "╭╮╰╯│─┬┴┼├┤") {
			t.Errorf("%s: output off a terminal holds ANSI or borders: %q", cmd, res.Stdout)
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Errorf("%s: trailing spaces in %q", cmd, line)
			}
		}
	}
}

func TestOutput_NoColorOnATerminalPrintsThePlainTable(t *testing.T) {
	// Stdout is a terminal, stdin is not: no TUI, and NO_COLOR removes colour and borders.
	res := goldenRun(t, "matrix", "table", cliOpts{StdoutTTY: true, Env: map[string]string{"NO_COLOR": "1"}})
	if res.Stdout == "" {
		t.Fatalf("matrix printed nothing (exit=%d stderr=%q)", res.Code, res.Stderr)
	}
	if res.Stdout != stripANSI(res.Stdout) || strings.ContainsAny(res.Stdout, "╭╮╰╯│─┬┴┼├┤") {
		t.Errorf("NO_COLOR output holds ANSI or borders: %q", res.Stdout)
	}
	if got, want := normalize(res.Stdout), readFileT(t, td("golden", "matrix_table.golden")); got != want {
		t.Errorf("NO_COLOR table differs from the plain golden\n--- got\n%s--- want\n%s", got, want)
	}
}

func TestOutput_TerminalTableIsBorderedAndColouredAndDiffersFromThePlainTable(t *testing.T) {
	// Stdout is a terminal, stdin is not (no TUI), NO_COLOR is unset: the bordered, coloured table of docs/output.md.
	// The harness writer is not a terminal, so CLICOLOR_FORCE makes lipgloss keep the colour it would drop there.
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	res := goldenRun(t, "probe", "table", cliOpts{StdoutTTY: true})
	if !verdictColourRe.MatchString(res.Stdout) {
		t.Errorf("the verdict of the terminal table has no colour: %q", res.Stdout)
	}
	lines := strings.Split(strings.TrimSuffix(stripANSI(res.Stdout), "\n"), "\n")
	want := []string{"│ Key │ env │", "│ Model │ claude-sonnet-5-5 (paid) │", "│ Result │ ✗ No budget HTTP 422 · N ms │", "│ Detail │ The budget of this key for this model is used up. │"}
	if len(lines) != len(want)+2 {
		t.Fatalf("want %d lines, got %d (exit=%d stderr=%q)\n%s", len(want)+2, len(lines), res.Code, res.Stderr, res.Stdout)
	}
	if !topBorderRe.MatchString(lines[0]) || !bottomBorderRe.MatchString(lines[len(lines)-1]) {
		t.Errorf("the table has no rounded top and bottom border\n%s", res.Stdout)
	}
	for i, w := range want {
		if got := collapseSpaces(lines[i+1]); got != w {
			t.Errorf("row %d = %q, want %q", i, got, w)
		}
		if utf8.RuneCountInString(lines[i+1]) != utf8.RuneCountInString(lines[0]) {
			t.Errorf("row %d is not as wide as the top border\n%s", i, res.Stdout)
		}
	}
	if res.Stdout == goldenRun(t, "probe", "table", cliOpts{}).Stdout {
		t.Error("the terminal table equals the plain table")
	}
	// The group title is the line above each table's top border: bold on a terminal, the plain name between the bold sequences.
	for _, cmd := range []string{"matrix", "models"} {
		t.Run(cmd+"_group_titles_are_bold", func(t *testing.T) {
			lines := strings.Split(goldenRun(t, cmd, "table", cliOpts{StdoutTTY: true}).Stdout, "\n")
			var titles []string
			for i, l := range lines {
				if i > 0 && strings.HasPrefix(l, "╭") {
					titles = append(titles, lines[i-1])
				}
			}
			if want := []string{"\x1b[1mclaude\x1b[m", "\x1b[1mgpt\x1b[m"}; !slices.Equal(titles, want) {
				t.Errorf("group title lines = %q, want %q", titles, want)
			}
		})
	}
}

var (
	topBorderRe     = regexp.MustCompile(`^╭─+┬─+╮$`)
	bottomBorderRe  = regexp.MustCompile(`^╰─+┴─+╯$`)
	verdictColourRe = regexp.MustCompile(`\x1b\[31m✗ No budget\x1b\[m`) // lipgloss.Red under xterm-256color
)
