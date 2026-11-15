/**
 * @spec-handoff
 * @interface litegauge --version (root only), and the version line that opens every help screen
 * @behavior
 *   - --version prints "Litegauge <versions.String()>\n" on stdout, nothing on stderr, exit 0, before any URL or key is read and with no request
 *   - --version with an extra argument prints the same line, exit 0; with -h (either order) the root help wins, exit 0
 *   - probe, models and matrix have no --version: exit 2, "Error: invalid argument.", blank line, that command's help
 *   - Every help screen (-h, a bare command, the help after a usage error) starts with "Litegauge dev", then an empty line
 * @edge-cases
 *   - LITEGAUGE_URL and LITEGAUGE_KEY set or not: the same line, and Getenv sees no LITEGAUGE_* name
 *   - These tests assume a build without ldflags, so the version shows as dev
 * @see ./usage_test.go
 * @see ../versions/versions_test.go
 */

package cli

import (
	"strings"
	"testing"
)

func TestVersion_FlagPrintsTheHeaderWithoutURLOrKey(t *testing.T) {
	const line = "Litegauge dev\n"
	set := map[string]string{"LITEGAUGE_URL": "https://gateway.example.com", "LITEGAUGE_KEY": canaryKey}
	for _, c := range []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{"alone", nil, []string{"--version"}, line},
		{"with_url_and_key_set", set, []string{"--version"}, line},
		{"extra_argument", nil, []string{"--version", "extra"}, line},
		{"then_help", nil, []string{"--version", "-h"}, wantRootHelp},
		{"help_then", nil, []string{"-h", "--version"}, wantRootHelp},
	} {
		t.Run(c.name, func(t *testing.T) {
			ct := &countingTransport{}
			res := runCLI(t, cliOpts{Env: c.env, Transport: ct}, c.args...)
			if res.Code != 0 || res.Stdout != c.want || res.Stderr != "" {
				t.Errorf("exit=%d stderr=%q stdout=%q, want exit 0 and stdout %q", res.Code, res.Stderr, res.Stdout, c.want)
			}
			for _, name := range res.EnvQueried {
				if strings.HasPrefix(name, "LITEGAUGE_") {
					t.Errorf("Getenv(%q) was called", name)
				}
			}
			if ct.Count() != 0 {
				t.Errorf("%d requests left the process", ct.Count())
			}
		})
	}
	for _, cmd := range []string{"probe", "models", "matrix"} {
		t.Run(cmd+"_has_no_version_flag", func(t *testing.T) {
			res := runCLI(t, cliOpts{}, cmd, "--version")
			wantUsage(t, res, "Error: invalid argument.", cmd, "-h")
			if !strings.HasPrefix(res.Stderr, "Error: invalid argument.\n\n"+line) {
				t.Errorf("stderr = %q, want it to start with the sentence, a blank line and %q", res.Stderr, line)
			}
		})
	}
}

func TestHelp_EveryScreenStartsWithTheVersionLine(t *testing.T) {
	type screen struct {
		name string
		args []string
	}
	screens := []screen{
		{"root_bare", nil},
		{"root_help", []string{"-h"}},
		{"root_usage_error", []string{"--bogus"}},
	}
	for _, cmd := range []string{"models", "probe", "matrix"} {
		screens = append(screens,
			screen{cmd + "_bare", []string{cmd}},
			screen{cmd + "_help", []string{cmd, "-h"}},
			screen{cmd + "_usage_error", []string{cmd, "--bogus"}},
		)
	}
	for _, s := range screens {
		t.Run(s.name, func(t *testing.T) {
			res := runCLI(t, cliOpts{}, s.args...)
			screen := res.Stdout
			if strings.HasSuffix(s.name, "usage_error") {
				var ok bool
				if _, screen, ok = strings.Cut(res.Stderr, "\n\n"); !ok {
					t.Fatalf("stderr = %q, want a sentence, a blank line and the help", res.Stderr)
				}
			}
			first := strings.SplitN(screen, "\n", 3)
			if len(first) < 3 || first[0] != "Litegauge dev" || first[1] != "" {
				t.Errorf("help starts with %q, want the line %q then an empty line", screen[:min(len(screen), 60)], "Litegauge dev")
			}
		})
	}
}
