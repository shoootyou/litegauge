// Package matrix is the matrix command: every model against every key.
package matrix

import (
	"context"
	"slices"
	"strings"

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
	models string
}

func NewCmd(s *session.Session) *cobra.Command {
	var o options
	t := messages.Matrix
	c := &cobra.Command{Use: t.Use, Short: t.Short, Long: t.Long, Example: t.Example, Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return run(s, &o) }}
	session.AddCommon(c, &o.Common)
	c.Flags().StringVar(&o.models, "models", "", messages.Flags.Models)
	return c
}

// parseModelList validates a --models value; every id passes the model id rule or the run stops.
func parseModelList(csv string, ks []keys.Entry) ([]string, error) {
	var ids []string
	for _, id := range strings.Split(csv, ",") {
		if !keys.SafeModelID(id, ks) {
			return nil, apperr.Usage(messages.Err.BadArg)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// discoverModels lists with the first key whose listing succeeds and returns that key.
func discoverModels(s *session.Session, gw *gateway.Client, ks []keys.Entry) ([]string, keys.Entry, error) {
	var last error
	for _, k := range ks {
		ids, err := gw.ListModels(s.Ctx, k.Value)
		if err == nil {
			return keys.SafeModels(ids, ks), k, nil
		}
		last = err
	}
	return nil, keys.Entry{}, last
}

func run(s *session.Session, o *options) error {
	gw, ks, err := s.Connect(o.Common)
	if err != nil {
		return err
	}
	var models []string
	infoKey := ks[0]
	if o.models != "" {
		if models, err = parseModelList(o.models, ks); err != nil {
			return err
		}
	} else if models, infoKey, err = discoverModels(s, gw, ks); err != nil {
		return err
	}
	metas := gw.ModelInfo(s.Ctx, infoKey.Value)
	if o.models == "" {
		if models = gateway.TextModels(models, metas); len(models) == 0 {
			return gateway.ListError(messages.Err.NoModels)
		}
	}
	prices := gateway.Prices(models, metas)
	cells := make([][]verdict.Result, len(models)) // [model][key], the order the output reads them
	for i := range cells {
		cells[i] = make([]verdict.Result, len(ks))
	}
	err = s.Work(o.Common, tui.Progress(len(ks)*len(models), s.NoColor()), func(ctx context.Context, step func()) {
		gw.RunGrid(ctx, ks, models, prices, func(key, model int, r verdict.Result) {
			cells[model][key] = r
			step()
		})
	})
	if err != nil {
		return err
	}
	s.Printer(o.Output).Matrix(ks, models, cells)
	if slices.ContainsFunc(cells, func(row []verdict.Result) bool {
		return slices.ContainsFunc(row, func(r verdict.Result) bool { return !r.Verdict.Passes() })
	}) {
		return apperr.ErrNotOK
	}
	return nil
}
