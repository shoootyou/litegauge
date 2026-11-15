/**
 * @spec-handoff
 * @interface litegauge models|probe|matrix with a rejected value that can carry key material, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - An invalid LITEGAUGE_KEY (too short, too long, outside the allowed set, whitespace or control inside) exits 2 with stdout empty and stderr equal to the fixed sentence plus the command help, in every -o mode
 *   - A rejected output format, URL, keys-file path, flag, argument, model id or --name exits 2 with the fixed sentence of its rule and the command help; the rejected value is never printed
 *   - No substring of 6 or more characters of the rejected value appears in stdout or stderr, and no request reaches the gateway
 * @edge-cases
 *   - A value that reaches the sentence only through string concatenation (an error built from the raw value) changes stderr, so the exact stderr equality catches it
 * @see ./security_test.go
 */

package cli

import (
	"strings"
	"testing"
)

const (
	sentBadEnvKey = "Error: the key in LITEGAUGE_KEY is not valid."
	sentBadFormat = "Error: unknown format; use table, json or yaml."
	sentBadURL    = "Error: LITEGAUGE_URL must be https (or http on localhost)."
	sentKeysFile  = "Error: could not read the keys file."
	sentBadArg    = "Error: invalid argument."
	sentNoKey     = "Error: missing key: use LITEGAUGE_KEY or --keys-file."
	sentKeyArg    = "Error: the key does not go on the command line; use LITEGAUGE_KEY or --keys-file."
	// keyEchoLoaded is a loaded key that is not sk- shaped, so only the later checks can catch it.
	keyEchoLoaded = "FAKEtoken0123456789"
)

// assertNoWindow fails when out holds any 6-character window of secret.
func assertNoWindow(t *testing.T, where, out, secret string) {
	t.Helper()
	for i := 0; i+6 <= len(secret); i++ {
		if w := secret[i : i+6]; strings.Contains(out, w) {
			t.Errorf("%s: window %q of the rejected value appears in output", where, w)
		}
	}
}

// usageOnly runs args and checks exit 2, empty stdout, stderr = sentence + blank line + help of helpOf, and no request.
func usageOnly(t *testing.T, o cliOpts, g *fakeGateway, sentence, helpOf string, secrets []string, args ...string) {
	t.Helper()
	res := runCLI(t, o, args...)
	helpArgs := []string{"-h"}
	if helpOf != "" {
		helpArgs = []string{helpOf, "-h"}
	}
	want := sentence + "\n\n" + runCLI(t, cliOpts{}, helpArgs...).Stdout
	if res.Code != 2 || res.Stdout != "" || res.Stderr != want {
		t.Errorf("exit=%d stdout=%q stderr=%q, want exit 2, no stdout, stderr %q", res.Code, res.Stdout, res.Stderr, want)
	}
	// Redundant with the exact stderr equality above; kept so a failure names the leaked window.
	for _, s := range secrets {
		assertNoWindow(t, "stdout+stderr", res.Stdout+res.Stderr, s)
	}
	if n := len(g.Calls()); n != 0 {
		t.Errorf("%d requests reached the gateway", n)
	}
	// With a counting transport the URL does not point at the fake, so only this counter sees a request.
	if ct, ok := o.Transport.(*countingTransport); ok && ct.Count() != 0 {
		t.Errorf("%d requests left the process", ct.Count())
	}
}

func TestKeyEcho_InvalidEnvKeyIsNeverEchoedByAnyCommandOrFormat(t *testing.T) {
	forms := map[string]string{
		"too_short":      "sk-FAKE",
		"too_long":       "sk-FAKE-CANARY-LONG-" + strings.Repeat("Zq7Xk9Rt2L", 52),
		"bad_char":       "sk-FAKE-CANARY-BAD!char-0001",
		"non_ascii":      "sk-FAKE-CANARY-λ-0002",
		"inner_space":    "sk-FAKE-CANARY BAD-0003",
		"inner_tab":      "sk-FAKE-CANARY\tBAD-0004",
		"inner_newline":  "sk-FAKE-CANARY\nSECOND-LINE-0005",
		"control_char":   "sk-FAKE-CANARY-\x01CTRL-0006",
		"trailing_ctrl":  "sk-FAKE-CANARY-TAIL-0007\x7f",
		"padded_invalid": "  sk-FAKE-CANARY-PAD!-0008  ",
	}
	cmds := map[string][]string{
		"models": {"models"},
		"probe":  {"probe", probeModel},
		"matrix": {"matrix"},
	}
	for fname, key := range forms {
		for cname, base := range cmds {
			for _, mode := range []string{"table", "json", "yaml"} {
				t.Run(fname+"_"+cname+"_"+mode, func(t *testing.T) {
					g := keysGateway(t)
					secret := strings.TrimSpace(key)
					usageOnly(t, cliOpts{Env: gwEnv(g, key)}, g, sentBadEnvKey, cname, []string{secret}, append(append([]string{}, base...), "-o", mode)...)
				})
			}
		}
	}
}

func TestKeyEcho_RejectedValuesNeverReachTheirErrorSentence(t *testing.T) {
	win := keyEchoLoaded[4:12]
	cases := []struct {
		name     string
		env      map[string]string // overrides on top of gwEnv(g, keyEchoLoaded)
		sentence string
		helpOf   string
		secrets  []string
		args     []string
		stdin    string
	}{
		{"format_equal_to_loaded_key", nil, sentBadFormat, "models", []string{keyEchoLoaded}, []string{"models", "-o", keyEchoLoaded}, ""},
		{"unknown_long_flag", nil, sentBadArg, "models", []string{keyEchoLoaded}, []string{"models", "--" + keyEchoLoaded}, ""},
		{"unknown_short_flag_cluster", nil, sentBadArg, "probe", []string{keyEchoLoaded}, []string{"probe", probeModel, "-Q" + keyEchoLoaded}, ""},
		{"extra_positional_on_models", nil, sentBadArg, "models", []string{keyEchoLoaded}, []string{"models", keyEchoLoaded}, ""},
		{"extra_positional_on_probe", nil, sentBadArg, "probe", []string{keyEchoLoaded}, []string{"probe", probeModel, keyEchoLoaded}, ""},
		{"extra_positional_on_matrix", nil, sentBadArg, "matrix", []string{keyEchoLoaded}, []string{"matrix", keyEchoLoaded}, ""},
		{"unknown_command", nil, sentBadArg, "", []string{keyEchoLoaded}, []string{keyEchoLoaded}, ""},
		{"url_with_userinfo", map[string]string{"LITEGAUGE_URL": "https://user:" + keyEchoLoaded + "@gw.example.com"}, sentBadURL, "models", []string{keyEchoLoaded}, []string{"models"}, ""},
		{"url_with_query", map[string]string{"LITEGAUGE_URL": "https://gw.example.com/?k=" + keyEchoLoaded}, sentBadURL, "probe", []string{keyEchoLoaded}, []string{"probe", probeModel}, ""},
		{"url_with_fragment", map[string]string{"LITEGAUGE_URL": "https://gw.example.com/#" + keyEchoLoaded}, sentBadURL, "matrix", []string{keyEchoLoaded}, []string{"matrix"}, ""},
		{"url_with_bad_scheme", map[string]string{"LITEGAUGE_URL": "ftp://" + keyEchoLoaded + ".example.com"}, sentBadURL, "models", []string{keyEchoLoaded}, []string{"models"}, ""},
		{"url_without_host", map[string]string{"LITEGAUGE_URL": keyEchoLoaded}, sentBadURL, "models", []string{keyEchoLoaded}, []string{"models"}, ""},
		{"missing_keys_file_path", nil, sentKeysFile, "models", []string{keyEchoLoaded}, []string{"models", "--keys-file", "/nonexistent-dir/" + keyEchoLoaded + ".txt"}, ""},
		{"model_id_with_key_window_on_probe", nil, sentBadArg, "probe", []string{"m-" + win}, []string{"probe", "m-" + win}, ""},
		{"model_id_with_key_window_on_matrix", nil, sentBadArg, "matrix", []string{"m-" + win}, []string{"matrix", "--models", "m-" + win}, ""},
		{"name_with_key_window_on_probe", nil, sentBadArg, "probe", []string{"n-" + win}, []string{"probe", probeModel, "--name", "n-" + win}, ""},
		{"name_with_key_window_on_models", nil, sentBadArg, "models", []string{"n-" + win}, []string{"models", "--name", "n-" + win}, ""},
		{"short_key_flag_with_value", nil, sentKeyArg, "probe", []string{keyEchoLoaded}, []string{"probe", probeModel, "-k" + keyEchoLoaded}, ""},
		{"blank_env_key", map[string]string{"LITEGAUGE_KEY": " \t "}, sentNoKey, "models", []string{" \t "}, []string{"models"}, ""},
		{"keys_file_with_no_key_named_like_one", nil, sentNoKey, "models", []string{keyEchoLoaded}, []string{"models", "--keys-file", "-"}, "# only a comment " + keyEchoLoaded + "\n"},
		{"keys_file_over_1MiB_holding_a_key", nil, sentKeysFile, "models", []string{keyEchoLoaded}, []string{"models", "--keys-file", "-"}, "Alpha=" + keyEchoLoaded + "\n# " + strings.Repeat("x", 1<<20) + "\n"},
		{"keys_file_over_500_lines_holding_a_key", nil, sentKeysFile, "models", []string{keyEchoLoaded}, []string{"models", "--keys-file", "-"}, "Alpha=" + keyEchoLoaded + "\n" + strings.Repeat("# note\n", 500)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := keysGateway(t)
			env := gwEnv(g, keyEchoLoaded)
			for k, v := range c.env {
				env[k] = v
			}
			o := cliOpts{Env: env, Stdin: c.stdin}
			if _, ok := c.env["LITEGAUGE_URL"]; ok {
				o.Transport = &countingTransport{} // the URL is not the fake gateway: a request must fail, never reach the network
			}
			usageOnly(t, o, g, c.sentence, c.helpOf, c.secrets, c.args...)
		})
	}
}

// A bare "?" is rejected through url.URL.ForceQuery; a bare "#" parses to an empty fragment, so only the raw-string check for "?#" rejects it.
func TestKeyEcho_BareQueryOrFragmentMarkerInTheURLIsRejected(t *testing.T) {
	cmds := map[string][]string{
		"models": {"models"},
		"probe":  {"probe", probeModel},
		"matrix": {"matrix"},
	}
	for name, url := range map[string]string{
		"bare_fragment":            "https://gw.example.com/#",
		"bare_fragment_after_path": "https://gw.example.com/path#",
		"bare_query":               "https://gw.example.com/?",
		"bare_query_after_path":    "https://gw.example.com/path?",
		"short_fragment":           "https://gw.example.com/#x",
	} {
		for cname, args := range cmds {
			t.Run(name+"_"+cname, func(t *testing.T) {
				g := keysGateway(t)
				env := gwEnv(g, keyEchoLoaded)
				env["LITEGAUGE_URL"] = url
				o := cliOpts{Env: env, Transport: &countingTransport{}}
				usageOnly(t, o, g, sentBadURL, cname, nil, args...) // the host also appears in the help examples, so the exact stderr equality is the echo check
			})
		}
	}
}
