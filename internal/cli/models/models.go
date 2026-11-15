// Package models is the models command: it lists the text models the gateway serves to a key.
package models

import (
	"github.com/spf13/cobra"

	"github.com/shoootyou/litegauge/internal/gateway"
	"github.com/shoootyou/litegauge/internal/keys"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/session"
)

type options struct {
	session.Common
	name string
}

func NewCmd(s *session.Session) *cobra.Command {
	var o options
	t := messages.Models
	c := &cobra.Command{Use: t.Use, Short: t.Short, Long: t.Long, Example: t.Example, Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return run(s, &o) }}
	session.AddCommon(c, &o.Common)
	c.Flags().StringVar(&o.name, "name", "", messages.Flags.Name)
	return c
}

func run(s *session.Session, o *options) error {
	gw, ks, key, err := s.Pick(o.Common, o.name)
	if err != nil {
		return err
	}
	ids, err := gw.ListModels(s.Ctx, key.Value)
	if err != nil {
		return err
	}
	metas := gw.ModelInfo(s.Ctx, key.Value)
	if ids = gateway.TextModels(keys.SafeModels(ids, ks), metas); len(ids) == 0 {
		return gateway.ListError(messages.Err.NoModels)
	}
	if err := s.Interrupted(); err != nil {
		return err
	}
	s.Printer(o.Output).Models(ids, gateway.Prices(ids, metas))
	return nil
}
