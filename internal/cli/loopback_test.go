/**
 * @spec-handoff
 * @interface LITEGAUGE_URL validation through run(args, env, gateway.Timing) for models and probe
 * @behavior
 *   - http is accepted only for the exact name localhost (any case), 127.0.0.0/8 and [::1], with or without a port
 *   - An accepted base URL goes on to make a request; a refused one prints the bad-URL sentence, exits 2 and makes none
 *   - The check is literal: no DNS lookup is made, so a name that resolves to loopback only through DNS is refused
 * @edge-cases
 *   - Neighbours of the loopback set are refused: 128.0.0.1, 126.255.255.255, [::2], 0.0.0.0, a trailing-dot localhost
 *   - IPv4-mapped IPv6 (::ffff:127.0.0.1) is refused today; the rule lists ::1 only
 * @see ./usage_test.go
 */

package cli

import (
	"strings"
	"testing"
)

const badURLSentence = "Error: LITEGAUGE_URL must be https (or http on localhost)."

func loopbackCommands() map[string][]string {
	return map[string][]string{"models": {"models"}, "probe": {"probe", probeModel}}
}

func TestLoopback_HTTPOnALoopbackHostIsAcceptedAndReachesTheTransport(t *testing.T) {
	bases := []string{
		"http://localhost", "http://localhost:8080", "http://LocalHost:8080",
		"http://127.0.0.1", "http://127.0.0.2:9", "http://127.255.255.254",
		"http://[::1]", "http://[::1]:8080",
	}
	for name, args := range loopbackCommands() {
		for _, base := range bases {
			t.Run(name+"/"+base, func(t *testing.T) {
				ct := &countingTransport{}
				res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": base, "LITEGAUGE_KEY": canaryKey}, Transport: ct}, args...)
				if strings.Contains(res.Stderr, badURLSentence) || res.Code == 2 {
					t.Fatalf("base URL refused: code %d, stderr %q", res.Code, res.Stderr)
				}
				if ct.Count() == 0 {
					t.Errorf("no request reached the transport (code %d, stderr %q)", res.Code, res.Stderr)
				}
			})
		}
	}
}

func TestLoopback_HostsNextToTheLoopbackSetAreRefusedBeforeAnyRequest(t *testing.T) {
	bases := []string{
		"http://128.0.0.1", "http://126.255.255.255", "http://[::2]", "http://0.0.0.0",
		"http://localhost.", "http://[::ffff:127.0.0.1]",
		// Would be loopback only through DNS: the rule is literal.
		"http://localtest.me", "http://localtest.me:8080",
	}
	for name, args := range loopbackCommands() {
		for _, base := range bases {
			t.Run(name+"/"+base, func(t *testing.T) {
				ct := &countingTransport{}
				res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": base, "LITEGAUGE_KEY": canaryKey}, Transport: ct}, args...)
				wantUsage(t, res, badURLSentence, args[0], "-h")
				if ct.Count() != 0 {
					t.Errorf("%d requests left the process", ct.Count())
				}
			})
		}
	}
}
