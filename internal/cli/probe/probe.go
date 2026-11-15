// Package probe is the probe command: one request with one key against one model.
package probe

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/gateway"
	"github.com/shoootyou/litegauge/internal/keys"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/session"
	"github.com/shoootyou/litegauge/internal/tui"
	"github.com/shoootyou/litegauge/internal/verdict"
)

type options struct {
	session.Common
	name string
}

func NewCmd(s *session.Session) *cobra.Command {
	var o options
	t := messages.Probe
	c := &cobra.Command{Use: t.Use, Short: t.Short, Long: t.Long, Example: t.Example, Args: args,
		RunE: func(_ *cobra.Command, a []string) error { return run(s, &o, a[0]) }}
	session.AddCommon(c, &o.Common)
	c.Flags().StringVar(&o.name, "name", "", messages.Flags.Name)
	return c
}

func args(_ *cobra.Command, a []string) error {
	switch len(a) {
	case 0:
		return apperr.Usage(messages.Err.NoModel)
	case 1:
		return nil
	}
	return apperr.Usage(messages.Err.BadArg)
}

func run(s *session.Session, o *options, model string) error {
	gw, ks, key, err := s.Pick(o.Common, o.name)
	if err != nil {
		return err
	}
	if !keys.SafeModelID(model, ks) {
		return apperr.Usage(messages.Err.BadArg)
	}
	price := gateway.PriceOf(gw.ModelInfo(s.Ctx, key.Value)[model])
	var r verdict.Result
	err = s.Work(o.Common, tui.Spinner(model, key.Name), func(ctx context.Context, _ func()) {
		r = gw.Probe(ctx, key.Name, key.Value, model, price)
	})
	if err != nil {
		return err
	}
	s.Printer(o.Output).Probe(r)
	if !r.Verdict.Passes() {
		return apperr.ErrNotOK
	}
	return nil
}
