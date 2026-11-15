package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/cli/matrix"
	"github.com/shoootyou/litegauge/internal/cli/models"
	"github.com/shoootyou/litegauge/internal/cli/probe"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/session"
	"github.com/shoootyou/litegauge/internal/versions"
)

// orderKey marks a command's place in the root help; cobra lists commands by name, the help by registration.
const orderKey = "order"

func newRoot(s *session.Session) *cobra.Command {
	t := messages.Root
	root := &cobra.Command{
		Use: t.Use, Short: t.Short, Long: t.Long, Example: t.Example,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.SetHelpFunc(func(c *cobra.Command, _ []string) { _, _ = io.WriteString(c.OutOrStdout(), help(c)) })
	// pflag echoes the offending flag text, which can carry a key: every parse error becomes one fixed sentence.
	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return apperr.Usage(messages.Err.BadArg) })
	for i, c := range []*cobra.Command{models.NewCmd(s), probe.NewCmd(s), matrix.NewCmd(s)} {
		c.Annotations = map[string]string{orderKey: strconv.Itoa(i)}
		c.Flags().BoolP("help", "h", false, messages.Flags.Help)
		root.AddCommand(c)
	}
	root.Flags().BoolP("help", "h", false, messages.Flags.Help)
	// A flag named version stops cobra from adding its own; the template reads the field, so "{{" in a version cannot run.
	root.Flags().Bool("version", false, messages.Flags.Version)
	root.Version = header()
	root.SetVersionTemplate("{{.Version}}\n")
	return root
}

func header() string { return fmt.Sprintf(messages.Help.Header, versions.String()) }

// help is the one template of the root and of every command: the version line, Long, usage, commands (root only),
// flags, examples, and the root footer.
func help(c *cobra.Command) string {
	h := messages.Help
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", header())
	fmt.Fprintf(&b, "%s\n\n%s\n  %s%s [flags]\n\n", c.Long, h.Usage, c.CommandPath(), strings.TrimPrefix(c.Use, c.Name()))
	if !c.HasParent() {
		cmds := slices.DeleteFunc(slices.Clone(c.Commands()), func(k *cobra.Command) bool { return k.Hidden })
		slices.SortFunc(cmds, func(x, y *cobra.Command) int {
			return strings.Compare(x.Annotations[orderKey], y.Annotations[orderKey])
		})
		width := 0
		for _, k := range cmds {
			width = max(width, len(k.Name()))
		}
		fmt.Fprintf(&b, "%s\n", h.Commands)
		for _, k := range cmds {
			fmt.Fprintf(&b, "  %-*s   %s\n", width, k.Name(), k.Short)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%s\n%s\n%s\n  %s\n", h.Flags, c.Flags().FlagUsages(), h.Examples, strings.ReplaceAll(c.Example, "\n", "\n  "))
	if !c.HasParent() {
		fmt.Fprintf(&b, "\n%s\n", h.Footer)
	}
	return b.String()
}
