// Package keys reads the keys of a run and holds every rule that stops key material from reaching a command line, a name or an output.
package keys

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/messages"
)

type Entry struct{ Name, Value string }

// Source is where keys come from: the environment, or a file whose "-" means Stdin.
type Source struct {
	Getenv   func(string) string
	Unsetenv func(string) error // nil in tests; the real one drops the key from the process environment once read
	Stdin    io.Reader
}

const maxBytes = 1 << 20

var (
	valueRe   = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{8,512}$`)
	nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,23}$`)
	modelIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$`)
	skRunRe   = regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]{8,}`)
	// A bare sha256: a maximal run of exactly 64 hex characters. A longer all-hex id is not a hash.
	hex64Re = regexp.MustCompile(`(?:^|[^0-9a-fA-F])[0-9a-fA-F]{64}(?:[^0-9a-fA-F]|$)`)
)

// Parse reads name=key lines and bare keys (named key1, key2... by their order among bare keys).
// A line that breaks a rule is reported by its number only, never by its text.
func Parse(r io.Reader) ([]Entry, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return nil, apperr.Usage(messages.Err.KeysFile)
	}
	text := strings.TrimSuffix(string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 500 {
		return nil, apperr.Usage(messages.Err.KeysFile)
	}
	var keys []Entry
	var lineNo []int
	bare := 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		name, value, named := strings.Cut(t, "=")
		if named {
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		} else {
			bare++
			name, value = "key"+strconv.Itoa(bare), t
		}
		dup := slices.ContainsFunc(keys, func(k Entry) bool { return k.Name == name })
		if dup || !nameRe.MatchString(name) || !valueRe.MatchString(value) {
			return nil, badLine(i + 1)
		}
		keys = append(keys, Entry{name, value})
		lineNo = append(lineNo, i+1)
	}
	// A name is printed, so it must not hold 8 characters of any loaded key.
	for i, k := range keys {
		for _, other := range keys {
			if hasWindowOf(strings.ToLower(k.Name), strings.ToLower(other.Value), 8) {
				return nil, badLine(lineNo[i])
			}
		}
	}
	return keys, nil
}

func badLine(n int) error { return apperr.Usage(fmt.Sprintf(messages.Err.BadLine, n)) }

// Load resolves the key source: a file (- is stdin) wins over LITEGAUGE_KEY.
// A file, FIFO or stdin is read in a goroutine, so an interrupt ends the wait even while an open blocks.
func Load(ctx context.Context, src Source, path string) ([]Entry, error) {
	if path == "" {
		v := strings.TrimSpace(src.Getenv("LITEGAUGE_KEY"))
		if src.Unsetenv != nil {
			_ = src.Unsetenv("LITEGAUGE_KEY")
		}
		switch {
		case v == "":
			return nil, apperr.Usage(messages.Err.NoKey)
		case !valueRe.MatchString(v):
			return nil, apperr.Usage(messages.Err.BadEnvKey)
		}
		return []Entry{{"env", v}}, nil
	}
	type result struct {
		keys []Entry
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if recover() != nil { // the panic value can hold file text: report only the fixed sentence
				ch <- result{err: apperr.Fatal(messages.Err.Internal)}
			}
		}()
		in := src.Stdin
		if path != "-" {
			f, err := os.Open(path)
			if err != nil {
				ch <- result{err: apperr.Usage(messages.Err.KeysFile)}
				return
			}
			defer f.Close()
			in = f
		}
		keys, err := Parse(in)
		if err == nil && len(keys) == 0 {
			err = apperr.Usage(messages.Err.NoKey)
		}
		ch <- result{keys, err}
	}()
	select {
	case r := <-ch:
		return r.keys, r.err
	case <-ctx.Done():
		return nil, apperr.ErrInterrupted
	}
}

// Pick selects the one key for probe and models: the named one, or the only one in the source.
func Pick(keys []Entry, name string) (Entry, error) {
	if name == "" {
		if len(keys) != 1 {
			return Entry{}, apperr.Usage(fmt.Sprintf(messages.Err.ManyKeys, len(keys)))
		}
		return keys[0], nil
	}
	for _, k := range keys {
		if k.Name == name {
			return k, nil
		}
	}
	// The name is echoed only when it cannot hold key material.
	if nameRe.MatchString(name) && !LooksKeyLike(name) && !CarriesKeyWindow(name, keys) {
		return Entry{}, apperr.Usage(fmt.Sprintf(messages.Err.NoNamedKey, name))
	}
	return Entry{}, apperr.Usage(messages.Err.BadArg)
}

// LooksKeyLike catches an sk- run in any case or a bare 64-hex run (a sha256 key hash).
func LooksKeyLike(s string) bool { return skRunRe.MatchString(s) || hex64Re.MatchString(s) }

// CarriesKeyWindow reports whether s carries a 6-character window of a key body (the key without a
// leading sk-) or an 8-character window of its sha256 hex, compared case-insensitively.
func CarriesKeyWindow(s string, keys []Entry) bool {
	low := strings.ToLower(s)
	for _, k := range keys {
		sum := sha256.Sum256([]byte(k.Value))
		if hasWindowOf(low, strings.TrimPrefix(strings.ToLower(k.Value), "sk-"), 6) || hasWindowOf(low, hex.EncodeToString(sum[:]), 8) {
			return true
		}
	}
	return false
}

// hasWindowOf reports whether some n-byte window of s is a substring of src.
func hasWindowOf(s, src string, n int) bool {
	for i := 0; i+n <= len(s); i++ {
		if strings.Contains(src, s[i:i+n]) {
			return true
		}
	}
	return false
}

// SafeModelID is the model id rule; keys are the loaded keys.
func SafeModelID(id string, keys []Entry) bool {
	return modelIDRe.MatchString(id) && !LooksKeyLike(id) && !CarriesKeyWindow(id, keys)
}

// SafeModels keeps the ids that pass the model id rule.
func SafeModels(ids []string, keys []Entry) []string {
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return !SafeModelID(id, keys) })
}

// RejectFlags refuses a key-bearing flag before cobra can echo it.
func RejectFlags(args []string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "-k") {
			return apperr.Usage(messages.Err.KeyArg)
		}
		if name, ok := strings.CutPrefix(a, "--"); ok {
			name, _, _ = strings.Cut(name, "=")
			if slices.Contains([]string{"key", "api-key", "apikey", "token", "secret", "password", "auth"}, name) {
				return apperr.Usage(messages.Err.KeyArg)
			}
		}
	}
	return nil
}

// RejectLikeArgs refuses an argument shaped like a key, or equal to a loaded one.
func RejectLikeArgs(args []string, loaded []Entry) error {
	for _, a := range args {
		if LooksKeyLike(a) || slices.ContainsFunc(loaded, func(k Entry) bool { return k.Value == a }) {
			return apperr.Usage(messages.Err.KeyArg)
		}
	}
	return nil
}
