/**
 * @spec-handoff
 * @interface litegauge models -o table|json|yaml, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - A /v1/models answer with no text models makes models exit 1, print noModelsSentence on stderr and nothing on stdout
 *   - matrix gives the identical stderr and exit code for the same gateway answer
 *   - Holds for table, json and yaml output
 * @edge-cases
 *   - {"data":[]} and a listing whose every entry has a non-text mode (image_generation)
 *   - A listing with at least one text model keeps printing the models and exits 0
 * @see ./classify_test.go
 */

package cli

import (
	"strings"
	"testing"
)

// noModelsSentence is the exact line matrix prints when the listing holds no text model.
const noModelsSentence = "Error: could not list the models (no models)."

func TestModels_EmptyListingExits1WithTheMatrixSentenceInEveryFormat(t *testing.T) {
	img := func(name string) infoEntry {
		e := paidInfo(name)
		e.Mode = "image_generation"
		return e
	}
	listings := []struct {
		name  string
		rules []fakeRule
	}{
		{"empty data array", []fakeRule{
			{Path: "/v1/models", Status: 200, Body: `{"data":[]}`},
			plain404("/v1/model/info"), plain404("/model/info")}},
		{"only non-text models", []fakeRule{
			{Path: "/v1/models", Status: 200, Body: modelsBody("openai", "pic-a", "pic-b")},
			{Path: "/v1/model/info", Status: 200, Body: modelInfoBody(img("pic-a"), img("pic-b"))},
			plain404("/model/info")}},
	}
	for _, l := range listings {
		for _, format := range []string{"table", "json", "yaml"} {
			t.Run(l.name+"/"+format, func(t *testing.T) {
				g := newFakeGateway(t, l.rules)
				o := cliOpts{Env: gwEnv(g, canaryKey)}
				models := runCLI(t, o, "models", "-o", format)
				matrix := runCLI(t, o, "matrix", "-o", format)

				if matrix.Code != 1 || strings.TrimSpace(matrix.Stderr) != noModelsSentence {
					t.Fatalf("matrix baseline drifted: exit=%d stderr=%q", matrix.Code, matrix.Stderr)
				}
				if models.Code != 1 || models.Stdout != "" || strings.TrimSpace(models.Stderr) != noModelsSentence {
					t.Errorf("models: exit=%d stdout=%q stderr=%q, want exit 1, empty stdout, %q", models.Code, models.Stdout, models.Stderr, noModelsSentence)
				}
				if models.Code != matrix.Code || models.Stderr != matrix.Stderr {
					t.Errorf("parity: models (exit=%d stderr=%q) differs from matrix (exit=%d stderr=%q)", models.Code, models.Stderr, matrix.Code, matrix.Stderr)
				}
			})
		}
	}
}
