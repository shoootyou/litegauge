/**
 * @spec-handoff
 * @interface package messages: Command{Use, Short, Long, Example}, Root/Models/Probe/Matrix, Flags, Help, Err and every other user-facing string
 * @behavior
 *   - internal/messages holds exactly one non-test .go file, so flipping the language edits one file
 *   - No other non-test .go file in the module holds a string literal made of two words (3+ letters each, split by white space)
 * @edge-cases
 *   - Words joined by "-", "/", "_" or "." (HTTP headers, JSON field names, paths) are one token and never match
 *   - Strings in _test.go files are exempt: tests pin the English text they expect
 * @see ../cli/usage_test.go
 */

package messages

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// twoWords is what a sentence looks like. HTTP headers, JSON field names, paths and URLs are one token joined by
// "-", "/", "_", "." or ":" and never match, and _test.go files are not scanned, so no allow-list is needed.
// shortcut: a protocol literal that really holds two words would need an exact-match allow-list entry here, with its reason.
var twoWords = regexp.MustCompile(`[A-Za-z]{3,}\s+[A-Za-z]{3,}`)

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestT3_MessagesIsASingleFileAndNoOtherGoFileHoldsASentence(t *testing.T) {
	root := moduleRoot(t)
	own := filepath.Join(root, "internal", "messages")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "testdata" || n == "vendor" || path == own {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no production .go file found outside internal/messages: the scan proves nothing")
	}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err == nil && twoWords.MatchString(s) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: sentence outside internal/messages: %q", rel, fset.Position(lit.Pos()).Line, s)
			}
			return true
		})
	}

	entries, err := os.ReadDir(own)
	if err != nil {
		t.Fatal(err)
	}
	var nonTest []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			nonTest = append(nonTest, e.Name())
		}
	}
	if len(nonTest) != 1 {
		t.Errorf("internal/messages has %d non-test files %v, want exactly 1", len(nonTest), nonTest)
	}
}
