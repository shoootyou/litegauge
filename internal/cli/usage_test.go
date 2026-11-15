/**
 * @spec-handoff
 * @interface litegauge [CMD] [-h] and every usage or configuration error, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - litegauge, litegauge -h and CMD -h print the full help on stdout, exit 0, nothing on stderr
 *   - Help sections in order: header, Long, Usage:, Commands: (root only), Flags:, Examples:, then the root footer; the root lists -h and --version
 *   - Every usage error prints its sentence, a blank line, then that command's full help, all on stderr, exit 2, nothing on stdout
 *   - Sentences: missing model; unknown format; missing LITEGAUGE_URL; missing key; http only on loopback hosts; pick one with --name; no key named "X"
 *   - No request leaves the process before the arguments and configuration are valid
 * @edge-cases
 *   - The name X is printed only when it passes the key-name rule
 *   - Exit 0 and 1 are covered in classify_test.go, 130 in security_test.go
 * @see ./keys_test.go
 */

package cli

import (
	"strings"
	"testing"
)

const wantRootHelp = `Litegauge dev

Checks whether the keys of a LiteLLM gateway have budget for a model.
Each command sends minimal requests and gives a verdict per key and model.
The URL goes in LITEGAUGE_URL; the key goes in LITEGAUGE_KEY or in --keys-file.

Usage:
  litegauge COMMAND [flags]

Commands:
  models   Lists the text models the gateway serves to the key.
  probe    Tests one key against one model with a single request.
  matrix   Tests every model with every key.

Flags:
  -h, --help      show this help
      --version   show the version

Examples:
  litegauge models
  litegauge probe claude-sonnet-5-5 -o json
  litegauge matrix --keys-file keys.txt

Use "litegauge COMMAND -h" for help on a command.
`

const wantProbeHelp = `Litegauge dev

Sends a minimal request to /v1/chat/completions with one key and gives its verdict.
Exits 0 if the result is ok or ok*, and 1 if it is not.
With several keys in --keys-file, pick one with --name.

Usage:
  litegauge probe MODEL [flags]

Flags:
  -o, --output string      format: table, json or yaml (default "table")
      --keys-file string   keys file (one name=key per line), or - for stdin
      --name string        name of the key to use from the file
  -h, --help               show this help

Examples:
  litegauge probe claude-sonnet-5-5
  litegauge probe claude-sonnet-5-5 -o json
  litegauge probe claude-opus-5-5 --keys-file keys.txt --name Alpha
`

func TestUsage_HelpFlagPrintsTheContractTextOnStdout(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-h"}, wantRootHelp},
		{[]string{"probe", "-h"}, wantProbeHelp},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			res := runCLI(t, cliOpts{}, c.args...)
			if res.Code != 0 || res.Stdout != c.want || res.Stderr != "" {
				t.Errorf("exit=%d stderr=%q stdout=\n%s\nwant\n%s", res.Code, res.Stderr, res.Stdout, c.want)
			}
		})
	}
}

func TestUsage_BareCommandsAndHelpFlagsPrintTheFullHelpWithExit0(t *testing.T) {
	cases := []struct {
		args []string
		use  string
	}{
		{nil, "Usage:\n  litegauge COMMAND"},
		{[]string{"-h"}, "Usage:\n  litegauge COMMAND"},
		{[]string{"models"}, "Usage:\n  litegauge models"},
		{[]string{"models", "-h"}, "Usage:\n  litegauge models"},
		{[]string{"matrix", "-h"}, "Usage:\n  litegauge matrix"},
		{[]string{"probe", "-h"}, "Usage:\n  litegauge probe MODEL"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			res := runCLI(t, cliOpts{}, c.args...)
			root := strings.HasSuffix(c.use, "COMMAND")
			// Every screen holds a usage line, the flags and the examples; only the root lists the commands and takes just -h.
			ok := res.Code == 0 && res.Stderr == "" && strings.Contains(res.Stdout, c.use) &&
				strings.Contains(res.Stdout, "-h, --help") && strings.Contains(res.Stdout, "Examples:") &&
				strings.Contains(res.Stdout, "Commands:") == root && strings.Contains(res.Stdout, "-o, --output") == !root
			if !ok {
				t.Errorf("exit=%d stderr=%q stdout=%q", res.Code, res.Stderr, res.Stdout)
			}
		})
	}
}

// wantUsage checks the shape of a usage error: sentence, blank line, the command's help (what CMD -h prints), exit 2.
func wantUsage(t *testing.T, res cliResult, sentence string, helpArgs ...string) {
	t.Helper()
	help := runCLI(t, cliOpts{}, helpArgs...).Stdout
	if help == "" {
		t.Fatalf("%v -h printed no help", helpArgs)
	}
	if res.Code != 2 || res.Stdout != "" || res.Stderr != sentence+"\n\n"+help {
		t.Errorf("exit=%d stdout=%q\nstderr=%q\nwant stderr=%q", res.Code, res.Stdout, res.Stderr, sentence+"\n\n"+help)
	}
}

func TestUsage_MissingModelPrintsTheSentenceThenProbeHelp(t *testing.T) {
	g := keysGateway(t)
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe")
	wantUsage(t, res, "Error: missing model.", "probe", "-h")
	if !strings.HasSuffix(res.Stderr, "\n\n"+wantProbeHelp) {
		t.Errorf("stderr does not end with the contract help block: %q", res.Stderr)
	}
	if n := len(g.Calls()); n != 0 {
		t.Errorf("%d requests before the usage error", n)
	}
}

func TestUsage_UnknownOutputFormatPrintsTheSentenceThenHelp(t *testing.T) {
	g := keysGateway(t)
	res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel, "-o", "xml")
	wantUsage(t, res, "Error: unknown format; use table, json or yaml.", "probe", "-h")
	if n := len(g.Calls()); n != 0 {
		t.Errorf("%d requests before the usage error", n)
	}
}

func TestUsage_MissingURLAndMissingKeyNameWhatIsMissing(t *testing.T) {
	t.Run("url", func(t *testing.T) {
		res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_KEY": canaryKey}}, "matrix")
		wantUsage(t, res, "Error: missing LITEGAUGE_URL.", "matrix", "-h")
	})
	t.Run("key", func(t *testing.T) {
		g := keysGateway(t)
		res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "models")
		wantUsage(t, res, "Error: missing key: use LITEGAUGE_KEY or --keys-file.", "models", "-h")
		if n := len(g.Calls()); n != 0 {
			t.Errorf("%d requests before the usage error", n)
		}
	})
}

func TestUsage_HTTPOnANonLoopbackHostIsRefusedBeforeAnyRequest(t *testing.T) {
	ct := &countingTransport{}
	env := map[string]string{"LITEGAUGE_URL": "http://gateway.example.com", "LITEGAUGE_KEY": canaryKey}
	res := runCLI(t, cliOpts{Env: env, Transport: ct}, "models")
	wantUsage(t, res, "Error: LITEGAUGE_URL must be https (or http on localhost).", "models", "-h")
	if ct.Count() != 0 {
		t.Errorf("%d requests left the process", ct.Count())
	}
}

func TestUsage_HostsThatOnlyContainLocalhostAreNotLoopbackAndAreRefusedBeforeAnyRequest(t *testing.T) {
	// A suffix or prefix match on "localhost" would accept every one of these.
	for _, base := range []string{"http://evillocalhost", "http://notlocalhost:8080", "http://localhost.evil.com"} {
		t.Run(base, func(t *testing.T) {
			ct := &countingTransport{}
			res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": base, "LITEGAUGE_KEY": canaryKey}, Transport: ct}, "models")
			wantUsage(t, res, "Error: LITEGAUGE_URL must be https (or http on localhost).", "models", "-h")
			if ct.Count() != 0 {
				t.Errorf("%d requests left the process", ct.Count())
			}
		})
	}
}

func TestUsage_NameIsRequiredForSeveralKeysAndMustExist(t *testing.T) {
	keys := td("keys", "simple.txt")
	t.Run("missing", func(t *testing.T) {
		g := keysGateway(t)
		three := writeTemp(t, t.TempDir(), "three.txt", "Alpha=sk-FAKE-THREE-0001\nBravo=sk-FAKE-THREE-0002\nCharlie=sk-FAKE-THREE-0003\n")
		res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "probe", probeModel, "--keys-file", three)
		wantUsage(t, res, "Error: the file has 3 keys; pick one with --name.", "probe", "-h")
		if len(g.Calls()) != 0 {
			t.Error("requests were sent without a chosen key")
		}
	})
	t.Run("unknown", func(t *testing.T) {
		g := keysGateway(t)
		res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "probe", probeModel, "--keys-file", keys, "--name", "Zulu")
		wantUsage(t, res, `Error: no key named "Zulu".`, "probe", "-h")
	})
	t.Run("unknown_name_that_breaks_the_name_rule_is_not_echoed", func(t *testing.T) {
		g := keysGateway(t)
		res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "probe", probeModel, "--keys-file", keys, "--name", "bad name<>")
		if res.Code != 2 || res.Stdout != "" || strings.Contains(res.Stderr, "bad name") {
			t.Errorf("exit=%d stdout=%q stderr=%q, want exit 2 and no echo of the name", res.Code, res.Stdout, res.Stderr)
		}
	})
}
