// Package session carries one run's inputs. It is the only reader of the environment and of the terminal state.
package session

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/gateway"
	"github.com/shoootyou/litegauge/internal/keys"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/output"
	"github.com/shoootyou/litegauge/internal/tui"
)

type Env struct {
	Getenv    func(string) string
	Unsetenv  func(string) error // nil in tests; RealEnv wires os.Unsetenv so a key leaves the process environment once read
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	StdinTTY  bool
	StdoutTTY bool
	Transport http.RoundTripper
}

func RealEnv() Env {
	return Env{
		Getenv: os.Getenv, Unsetenv: os.Unsetenv, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdinTTY: isTTY(os.Stdin), StdoutTTY: isTTY(os.Stdout),
	}
}

// isTTY is a Unix-only heuristic: a character device other than /dev/null.
// shortcut: no Windows support, replace with a terminal library if that is ever needed.
func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(st, null)
}

// Session holds one run's inputs.
type Session struct {
	Env    Env
	Ctx    context.Context
	Args   []string
	Timing gateway.Timing
}

// Common is what every command that talks to a gateway takes.
type Common struct{ Output, KeysFile string }

// AddCommon adds the shared flags first; the root adds -h last, the order the help shows.
func AddCommon(c *cobra.Command, o *Common) {
	f := c.Flags()
	f.SortFlags = false
	f.StringVarP(&o.Output, "output", "o", "table", messages.Flags.Output)
	f.StringVar(&o.KeysFile, "keys-file", "", messages.Flags.KeysFile)
}

// Connect validates the output format and the URL and loads the keys; nothing is sent before all succeed.
func (s *Session) Connect(c Common) (*gateway.Client, []keys.Entry, error) {
	if !slices.Contains([]string{"table", "json", "yaml"}, c.Output) {
		return nil, nil, apperr.Usage(messages.Err.BadFormat)
	}
	raw := strings.TrimSpace(s.Env.Getenv("LITEGAUGE_URL"))
	if raw == "" {
		return nil, nil, apperr.Usage(messages.Err.NoURL)
	}
	base, err := validateBaseURL(raw)
	if err != nil {
		return nil, nil, err
	}
	ks, err := keys.Load(s.Ctx, keys.Source{Getenv: s.Env.Getenv, Unsetenv: s.Env.Unsetenv, Stdin: s.Env.Stdin}, c.KeysFile)
	if err != nil {
		return nil, nil, err
	}
	if err := keys.RejectLikeArgs(s.Args, ks); err != nil {
		return nil, nil, err
	}
	return gateway.New(base, s.Env.Transport, s.Timing), ks, nil
}

// Pick is Connect plus the one key a command works with: the named one, or the only one.
func (s *Session) Pick(c Common, name string) (*gateway.Client, []keys.Entry, keys.Entry, error) {
	gw, ks, err := s.Connect(c)
	if err != nil {
		return nil, nil, keys.Entry{}, err
	}
	key, err := keys.Pick(ks, name)
	return gw, ks, key, err
}

// validateBaseURL accepts https, or http on loopback only; a path prefix is kept,
// trailing slashes and one trailing /v1 segment are trimmed.
func validateBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil, u.Hostname() == "", u.User != nil, u.Fragment != "", u.RawQuery != "", u.ForceQuery, strings.ContainsAny(raw, "?#"):
		return nil, apperr.Usage(messages.Err.BadURL)
	case u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())):
		return nil, apperr.Usage(messages.Err.BadURL)
	}
	u.Host = strings.ToLower(u.Host)
	u.Path, u.RawPath = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1"), "/"), ""
	return u, nil
}

// isLoopbackHost is a literal check (no DNS): the name localhost exactly, plus 127.0.0.0/8 and ::1.
func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && ((a.Is4() && a.IsLoopback()) || a == netip.IPv6Loopback())
}

// Interrupted is ErrInterrupted once the run's context is done.
func (s *Session) Interrupted() error {
	if s.Ctx.Err() != nil {
		return apperr.ErrInterrupted
	}
	return nil
}

func (s *Session) NoColor() bool { return s.Env.Getenv("NO_COLOR") != "" }

// Printer prints in the given format: bordered and coloured on a terminal, and never with NO_COLOR.
func (s *Session) Printer(format string) output.Printer {
	return output.Printer{W: s.Env.Stdout, Format: format, Fancy: s.Env.StdoutTTY && !s.NoColor()}
}

// Work runs work behind the model m on a terminal, or plain otherwise. work calls step once per finished cell.
func (s *Session) Work(c Common, m tea.Model, work func(ctx context.Context, step func())) error {
	// The TUI needs both ends on a terminal, TERM not dumb, the table output, and keys not from stdin (the TUI owns stdin).
	if c.Output == "table" && s.Env.StdinTTY && s.Env.StdoutTTY && s.Env.Getenv("TERM") != "dumb" && c.KeysFile != "-" {
		return tui.Run(s.Ctx, s.Env.Stdin, s.Env.Stdout, m, work)
	}
	work(s.Ctx, func() {})
	return s.Interrupted()
}
