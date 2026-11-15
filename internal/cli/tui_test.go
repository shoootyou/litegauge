/**
 * @spec-handoff
 * @interface litegauge probe MODEL on a terminal (stdin and stdout are terminals)
 * @behavior
 *   - The TUI shows a spinner and ends by printing the same table the non-TUI table mode prints on a terminal
 *   - After removing ANSI escape sequences, the run's output ends with that table (latency normalised)
 *   - The exit code is the verdict's: 1 for a failed key
 * @edge-cases
 *   - The reference table comes from a run whose stdout is a terminal and stdin is not, so no TUI starts
 * @see ./golden_test.go
 */

package cli

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestTUI_FinalFrameEqualsTheTableOutput(t *testing.T) {
	budget := func(delay time.Duration) *fakeGateway {
		return newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: 422, Body: errJSON("budget_exceeded", "422", "m"), Delay: delay}}, infoRules("paid")...))
	}
	g := budget(0)
	want := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey), StdoutTTY: true}, "probe", probeModel)
	if want.Code != 1 || want.Stdout == "" {
		t.Fatalf("reference run: exit=%d stdout=%q", want.Code, want.Stdout)
	}
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	g2 := budget(15 * time.Millisecond) // the delay makes the TUI run's latency at least 15 ms, so its digit count can exceed the reference run's (10 ms or more against under 10 ms)
	res, ok := within(10*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: gwEnv(g2, canaryKey), StdinR: pr, StdinTTY: true, StdoutTTY: true}, "probe", probeModel)
	})
	if !ok {
		t.Fatal("the TUI run did not finish by itself")
	}
	if res.Code != 1 {
		t.Errorf("exit = %d stderr=%q, want 1", res.Code, res.Stderr)
	}
	// The cell padding follows the latency digit count, which differs between two runs: collapse the runs of
	// spaces inside each line, but keep the line breaks, so a frame printed without them still fails.
	got, table := collapseSpaces(stripANSI(res.Stdout)), collapseSpaces(want.Stdout)
	if !strings.Contains(got, "╭") {
		t.Errorf("the final frame holds no border glyph\n%s", got)
	}
	if !strings.HasSuffix(got, table) {
		t.Errorf("the final frame is not the table output\n--- final output\n%s\n--- table\n%s", got, table)
	}
}

// collapseSpaces normalises the latency, then turns each line's runs of spaces into one space and drops its edge spaces.
func collapseSpaces(s string) string {
	lines := strings.Split(normalize(s), "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.Join(lines, "\n")
}
