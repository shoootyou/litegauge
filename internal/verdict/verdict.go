// Package verdict turns one gateway response into a verdict.
package verdict

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

type Verdict string

const (
	OK            Verdict = "ok"
	KeyValid      Verdict = "key_valid"
	NoBudget      Verdict = "no_budget"
	Blocked       Verdict = "blocked"
	InvalidKey    Verdict = "invalid_key"
	ModelNotFound Verdict = "model_not_found"
	Error         Verdict = "error"
)

// Passes is the exit-code rule: ok and key_valid (ok*) count as success.
func (v Verdict) Passes() bool { return v == OK || v == KeyValid }

type Price string

const (
	Free    Price = "free"
	Paid    Price = "paid"
	Unknown Price = "unknown"
)

// Result has no free-text field: nothing from a response body can reach an output through it.
type Result struct {
	Key, Model string
	Price      Price
	Verdict    Verdict
	Status     int
	LatencyMS  int
}

// e003Re matches a gateway policy error (code E003) as a whole token in an error message.
var e003Re = regexp.MustCompile(`(?:^|[^A-Za-z0-9])E003(?:[^A-Za-z0-9]|$)`)

type errFields struct {
	typ, code, msg string
	obj            bool
}

// parseError reads body.error; a body that is not JSON gives the zero value.
func parseError(body []byte) (f errFields) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if dec.Decode(&doc) != nil {
		return f
	}
	top, _ := doc.(map[string]any)
	eo, _ := top["error"].(map[string]any)
	if eo == nil {
		return f
	}
	f.obj = true
	f.typ, f.code = scalarString(eo["type"]), scalarString(eo["code"])
	f.msg, _ = eo["message"].(string)
	return f
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

// Classify maps one response to a verdict; the first matching row wins.
// Among the error rows, error.type and the message are checked before the HTTP status (a 401/403 status reaches invalid_key only if no earlier row matched).
// A gateway policy error (code E003) in the message gives blocked; the message is never printed.
func Classify(status int, hdr http.Header, body []byte, price Price) Verdict {
	switch {
	case status >= 300 && status < 400:
		return Error
	case status >= 200 && status < 300:
		if len(hdr.Values("X-Litellm-Cache-Key")) == 0 && price == Paid {
			return OK
		}
		return KeyValid
	}
	f := parseError(body)
	switch {
	case f.typ == "budget_exceeded":
		return NoBudget
	case strings.HasSuffix(f.typ, "_model_access_denied"):
		return Blocked
	case (status == 400 || status == 403) && (f.typ == "" || f.typ == "invalid_request_error") && e003Re.MatchString(f.msg):
		return Blocked
	case slices.Contains([]string{"auth_error", "authentication_error", "expired_key", "token_not_found_in_db"}, f.typ) || status == 401 || status == 403:
		return InvalidKey
	case (status == 404 && f.obj) || f.typ == "not_found_error" || f.code == "model_not_found":
		return ModelNotFound
	}
	return Error
}
