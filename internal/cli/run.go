// Package cli wires the commands to the process: arguments, signals, panics and the exit code.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/gateway"
	"github.com/shoootyou/litegauge/internal/keys"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/session"
)

// Main is the process entry point called by cmd/litegauge.
func Main() int {
	return run(os.Args[1:], session.RealEnv(), gateway.ProdTiming)
}

func run(args []string, e session.Env, t gateway.Timing) (code int) {
	defer func() {
		if recover() != nil {
			fmt.Fprintln(e.Stderr, messages.Err.Internal)
			code = 1
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRoot(&session.Session{Env: e, Ctx: ctx, Args: args, Timing: t})
	root.SetArgs(args)
	root.SetOut(e.Stdout)
	root.SetErr(io.Discard)
	err := guardArgs(args)
	if err == nil {
		// A command alone, with nothing configured, shows its help.
		if c, _, ferr := root.Find(args); ferr == nil && c != root && len(args) == 1 &&
			e.Getenv("LITEGAUGE_URL") == "" && e.Getenv("LITEGAUGE_KEY") == "" {
			_, _ = io.WriteString(e.Stdout, help(c))
			return 0
		}
		err = root.Execute()
	}
	code, msg := apperr.Code(err)
	switch {
	case code == apperr.ExitUsage:
		if msg == "" {
			msg = messages.Err.BadArg
		}
		c, _, ferr := root.Find(args)
		if ferr != nil {
			c = root
		}
		fmt.Fprintf(e.Stderr, "%s\n\n%s", msg, help(c))
	case msg != "":
		fmt.Fprintln(e.Stderr, msg)
	}
	return code
}

// guardArgs runs the key-argument checks before cobra sees the arguments. Cobra's hidden
// completion commands can write a debug file and echo arguments, so they are refused wherever they appear
// (no model id can equal them: ids start with a letter or digit).
func guardArgs(args []string) error {
	if slices.ContainsFunc(args, func(a string) bool { return a == "__complete" || a == "__completeNoDesc" }) ||
		(len(args) > 0 && strings.HasPrefix(args[0], "__")) {
		return apperr.Usage(messages.Err.BadArg)
	}
	if err := keys.RejectFlags(args); err != nil {
		return err
	}
	return keys.RejectLikeArgs(args, nil)
}
