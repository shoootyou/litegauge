/**
 * @spec-handoff
 * @interface litegauge under hostile arguments, a panicking key reader and an E003 text with a foreign error type, driven through run(args, env, gateway.Timing)
 * @behavior
 *   - __complete and __completeNoDesc in any argument position give "Error: invalid argument.", exit 2, no request and no BASH_COMP_DEBUG_FILE file
 *   - A panic in the key-reading goroutine ends as "Error: internal error." with exit 1, no stack and no panic text
 *   - A --name holding a loaded key, a window of it or an sk- run exits 2, makes no request and is never printed
 *   - E003 in the message blocks only a 400/403 whose error type is empty or invalid_request_error; any other type falls through the rest of the verdict table
 * @edge-cases
 *   - 400 with type internal_server_error and E003 is error; 403 with the same body is invalid_key (the 403 row, first match)
 * @see ./security_test.go
 */

package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shoootyou/litegauge/internal/messages"
)

func TestS1_CompletionCommandsAreRefusedInAnyArgumentPosition(t *testing.T) {
	cases := map[string][]string{
		"after_output_flag":    {"-o", "json", "__complete", "probe", "foo"},
		"after_command":        {"probe", "__complete"},
		"nodesc_after_keyfile": {"models", "--keys-file", "x", "__completeNoDesc", "y"},
		"after_unknown_flag":   {"--bogus=1", "__complete", "x"},
		"nodesc_last":          {"matrix", "--models", "a", "__completeNoDesc"},
		"nodesc_first":         {"__completeNoDesc", "probe"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			g := keysGateway(t)
			debug := filepath.Join(t.TempDir(), "comp-debug.log")
			t.Setenv("BASH_COMP_DEBUG_FILE", debug)
			res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, args...)
			if res.Code != 2 || res.Stdout != "" || !strings.HasPrefix(res.Stderr, "Error: invalid argument.\n") {
				t.Errorf("exit=%d stdout=%q stderr=%q, want exit 2, no stdout and the bad-argument sentence", res.Code, res.Stdout, res.Stderr)
			}
			if n := len(g.Calls()); n != 0 {
				t.Errorf("%d requests reached the gateway", n)
			}
			if _, err := os.Stat(debug); err == nil {
				t.Error("the completion command wrote BASH_COMP_DEBUG_FILE")
			}
		})
	}
}

// panicReader stands in for a keys source that blows up while being read.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("KEYREADER-CANARY " + canaryKey) }

func TestS8_PanicInTheKeyReadingGoroutineEndsAsInternalError(t *testing.T) {
	env := map[string]string{"LITEGAUGE_URL": "https://gw.example.com"}
	res, ok := within(10*time.Second, func() cliResult {
		return runCLI(t, cliOpts{Env: env, StdinR: panicReader{}}, "models", "--keys-file", "-")
	})
	if !ok {
		t.Fatal("run did not return")
	}
	if res.Code != 1 || res.Stdout != "" || !strings.HasPrefix(res.Stderr, "Error: internal error.") {
		t.Errorf("exit=%d stdout=%q stderr=%q, want exit 1 and \"Error: internal error.\"", res.Code, res.Stdout, res.Stderr)
	}
	assertNoText(t, "output", res.Stdout+res.Stderr, "goroutine ", "panic", "KEYREADER-CANARY", ".go:", canaryKey)
}

func TestS2_NameHoldingKeyMaterialIsRefusedWithoutEcho(t *testing.T) {
	// the window is 8 characters of the loaded key body and passes the name rule on its own
	window := strings.TrimPrefix(canaryKey, "sk-FAKE-")[:8]
	cases := []struct {
		name, value, sentence string
	}{
		{"loaded_key", canaryKey, messages.Err.KeyArg},
		{"key_window", window, messages.Err.BadArg},
		{"sk_shape", "sk-FAKE-Argument-0000001", messages.Err.KeyArg},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := keysGateway(t)
			res := runCLI(t, cliOpts{Env: gwEnv(g, canaryKey)}, "probe", probeModel, "--name", c.value)
			if res.Code != 2 || res.Stdout != "" || !strings.HasPrefix(res.Stderr, c.sentence+"\n") {
				t.Errorf("exit=%d stdout=%q stderr=%q, want exit 2 and %q", res.Code, res.Stdout, res.Stderr, c.sentence)
			}
			assertNoText(t, "output", res.Stdout+res.Stderr, c.value, window)
			if n := len(g.Calls()); n != 0 {
				t.Errorf("%d requests reached the gateway", n)
			}
		})
	}
}

func TestClassify_E003WithAForeignErrorTypeDoesNotBlock(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		verdict string
	}{
		{"400_internal_server_error_is_error", 400, "error"},
		{"403_internal_server_error_is_invalid_key", 403, "invalid_key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := chatGateway(t, "paid", c.status, errJSON("internal_server_error", "", "E003: policy"), http.Header{})
			_, r := probeJSON(t, g, cliOpts{})
			if r.Verdict != c.verdict || r.HTTPStatus != c.status {
				t.Errorf("verdict=%q http_status=%d, want %q %d", r.Verdict, r.HTTPStatus, c.verdict, c.status)
			}
		})
	}
}
