/**
 * @spec-handoff
 * @interface litegauge models|probe|matrix: the User-Agent request header
 * @behavior
 *   - Every request the CLI sends carries User-Agent "litegauge/" + the raw versions.Version: "litegauge/dev" by default
 *   - The raw value goes on the wire unchanged: Version "1.2.3" gives "litegauge/1.2.3", Version "v1.2.3" gives "litegauge/v1.2.3"
 * @see ../versions/versions_test.go
 */

package cli

import (
	"testing"

	"github.com/shoootyou/litegauge/internal/versions"
)

func TestUserAgent_IsTheProductAndTheRawVersion(t *testing.T) {
	orig := versions.Version
	t.Cleanup(func() { versions.Version = orig })
	for _, c := range []struct{ name, version, want string }{
		{"default_build", orig, "litegauge/dev"},
		{"digit_version", "1.2.3", "litegauge/1.2.3"},
		{"leading_v_version", "v1.2.3", "litegauge/v1.2.3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			versions.Version = c.version
			g := newFakeGateway(t, append([]fakeRule{
				{Path: chatPath, Status: 200, Body: okBody()},
				{Path: "/v1/models", Status: 200, Body: modelsBody("openai", probeModel)},
			}, infoRules("paid")...))
			env := gwEnv(g, canaryKey)
			for _, args := range [][]string{{"models"}, {"probe", probeModel}, {"matrix", "--models", probeModel}} {
				runCLI(t, cliOpts{Env: env}, args...)
			}
			calls := g.Calls()
			if len(calls) < 3 {
				t.Fatalf("only %d requests reached the gateway", len(calls))
			}
			for _, call := range calls {
				if got := call.Header.Get("User-Agent"); got != c.want {
					t.Errorf("%s %s: User-Agent = %q, want %q", call.Method, call.Path, got, c.want)
				}
			}
		})
	}
}
