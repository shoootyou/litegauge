/**
 * @spec-handoff
 * @interface newRoot(s *session.Session) *cobra.Command
 * @behavior
 *   - Every command (root, models, probe, matrix) has a one-line Short, a Long of 2 to 4 lines and an Example of 2 to 3 lines
 *   - Every flag of every command has a non-empty usage text
 *   - Every Example line starts with "litegauge ", resolves through root.Find to the command that owns it
 *     (the root's lines to the command they name), and its flags and arguments are accepted by ParseFlags and the command's Args
 * @edge-cases
 *   - The tree is walked, not listed: a command added to newRoot is checked with no edit here
 *   - Nothing runs: no handler, no environment read, no request
 * @see ./usage_test.go
 */

package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/shoootyou/litegauge/internal/session"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	return newRoot(&session.Session{
		Env:    session.Env{Getenv: func(string) string { return "" }, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard},
		Ctx:    t.Context(),
		Timing: testTiming,
	})
}

func walkCmds(c *cobra.Command, f func(*cobra.Command)) {
	f(c)
	for _, k := range c.Commands() {
		walkCmds(k, f)
	}
}

func splitLines(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }

func TestHelp_EveryCommandAndFlagCarriesItsDescriptions(t *testing.T) {
	var names []string
	walkCmds(testRoot(t), func(c *cobra.Command) {
		names = append(names, c.Name())
		if c.Short == "" || strings.Contains(c.Short, "\n") {
			t.Errorf("%s: Short = %q, want one non-empty line", c.Name(), c.Short)
		}
		if n := len(splitLines(c.Long)); strings.TrimSpace(c.Long) == "" || n < 2 || n > 4 {
			t.Errorf("%s: Long has %d lines, want 2 to 4", c.Name(), n)
		}
		if n := len(splitLines(c.Example)); strings.TrimSpace(c.Example) == "" || n < 2 || n > 3 {
			t.Errorf("%s: Example has %d lines, want 2 to 3", c.Name(), n)
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if strings.TrimSpace(f.Usage) == "" {
				t.Errorf("%s: flag --%s has no usage text", c.Name(), f.Name)
			}
		})
	})
	if got := strings.Join(names, " "); !strings.HasPrefix(got, "litegauge ") || len(names) != 4 {
		t.Errorf("walked %q, want the root and its three commands", got)
	}
}

func TestHelp_EveryExampleLineResolvesToItsCommandAndParses(t *testing.T) {
	var owners []string
	walkCmds(testRoot(t), func(c *cobra.Command) { owners = append(owners, c.Name()) })
	checked := 0
	for _, owner := range owners {
		for _, line := range splitLines(testExample(t, owner)) {
			root := testRoot(t) // a fresh tree per line: parsed flag values never carry over
			rest, ok := strings.CutPrefix(line, "litegauge ")
			if !ok {
				t.Errorf("%s: example %q does not start with \"litegauge \"", owner, line)
				continue
			}
			args := strings.Fields(rest)
			c, flagsAndArgs, err := root.Find(args)
			want := owner
			if owner == root.Name() {
				want = args[0] // the root's examples name the command they show
			}
			if err != nil || c == root || c.Name() != want {
				t.Errorf("%s: example %q resolves to %v (err %v), want command %q", owner, line, c, err, want)
				continue
			}
			if err := c.ParseFlags(flagsAndArgs); err != nil {
				t.Errorf("%s: example %q: flags rejected: %v", owner, line, err)
				continue
			}
			if c.Args != nil {
				if err := c.Args(c, c.Flags().Args()); err != nil {
					t.Errorf("%s: example %q: arguments rejected: %v", owner, line, err)
				}
			}
			checked++
		}
	}
	if checked < 6 {
		t.Errorf("only %d example lines were checked, want at least 6 (2 per command)", checked)
	}
}

// testExample returns the Example text of the command called name in a fresh tree.
func testExample(t *testing.T, name string) string {
	t.Helper()
	var ex string
	walkCmds(testRoot(t), func(c *cobra.Command) {
		if c.Name() == name {
			ex = c.Example
		}
	})
	return ex
}
