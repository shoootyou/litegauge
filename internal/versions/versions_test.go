/**
 * @spec-handoff
 * @interface var Version string (default "dev", set at build time with -X); func String() string
 * @behavior
 *   - String() shows the version to people: "" or "dev" give "dev"; a Version that starts with a digit gets one leading "v"; anything else is kept as is
 *   - A build with -ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=<v>" prints "Litegauge v<v>" on --version, "Litegauge vX" when <v> already starts with "v", and "Litegauge dev" with no flag
 * @edge-cases
 *   - "v1.2.3" stays "v1.2.3", never "vv1.2.3"; "1.2.3-rc.1" gives "v1.2.3-rc.1"; "abc" stays "abc"
 *   - The ldflags test builds ./cmd/litegauge from the module root and runs the binary with no LITEGAUGE_* variables
 * @see ../cli/version_test.go
 */

package versions

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestString_ShowsALeadingVOnce(t *testing.T) {
	if Version != "dev" {
		t.Errorf("default Version = %q, want %q", Version, "dev")
	}
	orig := Version
	t.Cleanup(func() { Version = orig })
	for _, c := range []struct{ version, want string }{
		{"dev", "dev"},
		{"", "dev"},
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
		{"1.2.3-rc.1", "v1.2.3-rc.1"},
		{"abc", "abc"},
	} {
		Version = c.version
		if got := String(); got != c.want {
			t.Errorf("Version=%q: String() = %q, want %q", c.version, got, c.want)
		}
	}
}

// goTool finds the Go tool: GOROOT/bin/go, or go on PATH when GOROOT is empty (as under -trimpath).
func goTool(t *testing.T) string {
	t.Helper()
	var tried []string
	if root := runtime.GOROOT(); root != "" {
		p := filepath.Join(root, "bin", "go")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		tried = append(tried, p)
	}
	p, err := exec.LookPath("go")
	if err == nil {
		return p
	}
	t.Fatalf("no Go tool found: looked for GOROOT/bin/go (%q) and go on PATH (%v)", tried, err)
	return ""
}

func TestLdflagsInjectionReachesTheBinary(t *testing.T) {
	wd, err := os.Getwd() // a test runs in its package directory, so the module root is two levels up
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..")
	goBin := goTool(t)
	const target = "github.com/shoootyou/litegauge/internal/versions.Version"
	for _, c := range []struct{ name, version, want string }{
		{"digit_gets_one_v", "1.2.3", "Litegauge v1.2.3\n"},
		{"leading_v_is_kept", "v1.2.3", "Litegauge v1.2.3\n"},
		{"no_ldflags_is_dev", "", "Litegauge dev\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "litegauge")
			args := []string{"build"}
			if c.version != "" {
				args = append(args, "-ldflags", "-X "+target+"="+c.version)
			}
			args = append(args, "-o", bin, "./cmd/litegauge")
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			build := exec.CommandContext(ctx, goBin, args...)
			build.Dir = root
			build.Env = append(os.Environ(), "GOTOOLCHAIN=local")
			quoted := []string{"GOTOOLCHAIN=local", strconv.Quote(goBin)}
			for _, a := range args {
				quoted = append(quoted, strconv.Quote(a))
			}
			cmdLine := strings.Join(quoted, " ")
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("%s (in %s): %v\n%s", cmdLine, root, err, out)
			}
			var stdout, stderr bytes.Buffer
			run := exec.CommandContext(ctx, bin, "--version")
			run.Env = []string{"PATH=" + os.Getenv("PATH")}
			run.Stdout, run.Stderr = &stdout, &stderr
			if err := run.Run(); err != nil || stdout.String() != c.want {
				t.Errorf("built with %s, then --version: err=%v stdout=%q stderr=%q, want stdout %q", cmdLine, err, stdout.String(), stderr.String(), c.want)
			}
		})
	}
}
