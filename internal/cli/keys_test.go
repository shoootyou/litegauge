/**
 * @spec-handoff
 * @interface litegauge matrix --models M --keys-file PATH|- -o json, with LITEGAUGE_URL set
 * @behavior
 *   - A line is name=key (split at the first "="); a line without "=" is a bare key named key1, key2... by order among bare keys
 *   - Blank lines and lines starting with "#" are skipped; CRLF endings and a leading BOM are accepted
 *   - Name rule ^[A-Za-z0-9][A-Za-z0-9_.-]{0,23}$, unique, holding no 8-char window of any loaded key; key rule 8..512 of [A-Za-z0-9._~+/=-]
 *   - Any line that breaks a rule exits 2 with "Error: line N of the keys file is not valid." (number only, never the text) before any request
 *   - The file is at most 1 MiB and 500 lines; beyond that the run exits 2 before any request
 * @edge-cases
 *   - --name picks one key from the file and the request carries that key
 * @see ./usage_test.go
 */

package cli

import (
	"slices"
	"strings"
	"testing"
)

func keysGateway(t *testing.T) *fakeGateway {
	t.Helper()
	return newFakeGateway(t, append([]fakeRule{{Path: chatPath, Status: 200, Body: okBody()}}, infoRules("paid")...))
}

func matrixKeys(t *testing.T, g *fakeGateway, stdin string, args ...string) (cliResult, outDoc) {
	t.Helper()
	res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}, Stdin: stdin},
		append([]string{"matrix", "--models", probeModel, "-o", "json"}, args...)...)
	var d outDoc
	if res.Code == 0 {
		d = decodeDoc(t, res.Stdout)
	}
	return res, d
}

func usedKeys(g *fakeGateway) []string {
	var out []string
	for _, c := range g.CallsTo("POST", chatPath) {
		out = append(out, requestKey(c.Header))
	}
	slices.Sort(out)
	return out
}

func TestKeysFile_NameKeyAndBareKeysGetTheirNamesSkippingCommentsAndBlanks(t *testing.T) {
	g := keysGateway(t)
	res, d := matrixKeys(t, g, "", "--keys-file", td("keys", "simple.txt"))
	if res.Code != 0 {
		t.Fatalf("exit = %d stderr=%q", res.Code, res.Stderr)
	}
	if want := []string{"Alpha", "Bravo", "Charlie.Two", "key1", "key2"}; !slices.Equal(d.Keys, want) {
		t.Errorf("key names = %v, want %v", d.Keys, want)
	}
	want := []string{"sk-FAKE-SIMPLE-0001", "sk-FAKE-SIMPLE-0002", "sk-FAKE-SIMPLE-0003", "sk-FAKE-SIMPLE-0004", "sk-FAKE-SIMPLE-0005"}
	if got := usedKeys(g); !slices.Equal(got, want) {
		t.Errorf("keys sent = %v, want %v", got, want)
	}
}

func TestKeysFile_CRLFLinesAreAcceptedAndNoCRReachesTheGateway(t *testing.T) {
	g := keysGateway(t)
	res, d := matrixKeys(t, g, "", "--keys-file", td("keys", "crlf.txt"))
	if res.Code != 0 {
		t.Fatalf("exit = %d stderr=%q", res.Code, res.Stderr)
	}
	if want := []string{"Alpha", "Bravo", "key1"}; !slices.Equal(d.Keys, want) {
		t.Errorf("key names = %v, want %v", d.Keys, want)
	}
	for _, k := range usedKeys(g) {
		if strings.ContainsAny(k, "\r\n") {
			t.Errorf("a key with a line ending reached the gateway: %q", k)
		}
	}
}

func TestKeysFile_LeadingBOMIsAccepted(t *testing.T) {
	g := keysGateway(t)
	res, d := matrixKeys(t, g, "", "--keys-file", td("keys", "bom.txt"))
	if res.Code != 0 {
		t.Fatalf("exit = %d stderr=%q", res.Code, res.Stderr)
	}
	if want := []string{"Alpha", "key1"}; !slices.Equal(d.Keys, want) {
		t.Errorf("key names = %v, want %v", d.Keys, want)
	}
}

func TestKeysFile_BadLineIsReportedByNumberOnly(t *testing.T) {
	cases := map[string]string{
		"key_too_short":  "Alpha=sk-FAKE-OK-000001\nBravo=SHORTK\n",
		"key_bad_char":   "Alpha=sk-FAKE-OK-000001\nBravo=sk-FAKE BAD-char-02\n",
		"name_bad_char":  "Alpha=sk-FAKE-OK-000001\nBra vo=sk-FAKE-OK-000002\n",
		"name_too_long":  "Alpha=sk-FAKE-OK-000001\nABCDEFGHIJKLMNOPQRSTUVWXY=sk-FAKE-OK-000002\n",
		"name_bad_start": "Alpha=sk-FAKE-OK-000001\n.hidden=sk-FAKE-OK-000002\n",
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			g := keysGateway(t)
			res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}, Stdin: file}, "matrix", "--models", probeModel, "--keys-file", "-")
			if res.Code != 2 || res.Stdout != "" || !strings.HasPrefix(res.Stderr, "Error: line 2 of the keys file is not valid.\n") {
				t.Errorf("exit=%d stdout=%q stderr=%q, want exit 2 and the line-2 sentence first", res.Code, res.Stdout, res.Stderr)
			}
			assertNoText(t, "stderr", res.Stderr, "SHORTK", "Bravo", "BAD-char", "Bra vo", "ABCDEFGHIJ", ".hidden", "000002")
			if n := len(g.Calls()); n != 0 {
				t.Errorf("%d requests were sent before the file was valid", n)
			}
		})
	}
}

func TestKeysFile_DuplicateNameIsRejectedAtTheSecondLine(t *testing.T) {
	g := keysGateway(t)
	file := "Alpha=sk-FAKE-OK-000001\nBravo=sk-FAKE-OK-000002\nAlpha=sk-FAKE-OK-000003\n"
	res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}, Stdin: file}, "matrix", "--models", probeModel, "--keys-file", "-")
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, "Error: line 3 of the keys file is not valid.\n") {
		t.Errorf("exit=%d stderr=%q, want exit 2 and the line-3 sentence", res.Code, res.Stderr)
	}
	if len(g.Calls()) != 0 {
		t.Error("requests were sent despite a duplicate name")
	}
}

func TestKeysFile_NameHoldingAKeyWindowIsRejectedWithoutEchoingIt(t *testing.T) {
	g := keysGateway(t)
	// "Zq7Xk9Rt" is an 8-char window of the first key.
	file := "Alpha=sk-FAKE-Zq7Xk9Rt2LmVwf4k3\nxxZq7Xk9Rtxx=sk-FAKE-OK-000002\n"
	res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}, Stdin: file}, "matrix", "--models", probeModel, "--keys-file", "-")
	if res.Code != 2 || !strings.HasPrefix(res.Stderr, "Error: line 2 of the keys file is not valid.\n") {
		t.Errorf("exit=%d stderr=%q, want exit 2 and the line-2 sentence", res.Code, res.Stderr)
	}
	assertNoText(t, "stderr", res.Stderr, "Zq7Xk9Rt")
	if len(g.Calls()) != 0 {
		t.Error("requests were sent despite a name holding a key window")
	}
}

func TestKeysFile_OverOneMiBExitsWithUsageCodeBeforeAnyRequest(t *testing.T) {
	g := keysGateway(t)
	path := writeTemp(t, t.TempDir(), "big.txt", "# "+strings.Repeat("x", 1<<20)+"\nAlpha=sk-FAKE-OK-000001\n")
	res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "matrix", "--models", probeModel, "--keys-file", path)
	if res.Code != 2 || res.Stdout != "" || len(g.Calls()) != 0 {
		t.Errorf("exit=%d stdout=%q calls=%d, want exit 2, no output, no request", res.Code, res.Stdout, len(g.Calls()))
	}
}

func TestKeysFile_Over500LinesExitsWithUsageCodeBeforeAnyRequest(t *testing.T) {
	g := keysGateway(t)
	path := writeTemp(t, t.TempDir(), "long.txt", strings.Repeat("# note\n", 500)+"Alpha=sk-FAKE-OK-000001\n")
	res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "matrix", "--models", probeModel, "--keys-file", path)
	if res.Code != 2 || res.Stdout != "" || len(g.Calls()) != 0 {
		t.Errorf("exit=%d stdout=%q calls=%d, want exit 2, no output, no request", res.Code, res.Stdout, len(g.Calls()))
	}
}

func TestKeysFile_NameSelectsTheKeyThatIsSent(t *testing.T) {
	g := keysGateway(t)
	res := runCLI(t, cliOpts{Env: map[string]string{"LITEGAUGE_URL": g.URL}}, "probe", probeModel, "--keys-file", td("keys", "simple.txt"), "--name", "Bravo")
	if res.Code != 0 {
		t.Fatalf("exit = %d stderr=%q", res.Code, res.Stderr)
	}
	if got := usedKeys(g); !slices.Equal(got, []string{"sk-FAKE-SIMPLE-0002"}) {
		t.Errorf("keys sent = %v, want only the Bravo key", got)
	}
	if !strings.Contains(res.Stdout, "Bravo") {
		t.Errorf("the result does not name the key: %q", res.Stdout)
	}
}

func TestKeysFile_EnvKeyIsNamedEnv(t *testing.T) {
	g := keysGateway(t)
	_, r := probeJSON(t, g, cliOpts{})
	if r.Key != "env" {
		t.Errorf("key name = %q, want env", r.Key)
	}
}
