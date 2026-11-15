// Package apperr holds the error kinds of a run and the one mapping from an error to an exit code.
package apperr

import "errors"

// Usage is a sentence for stderr; the run prints the command's help after it and exits 2.
type Usage string

func (e Usage) Error() string { return string(e) }

// Fatal is a sentence for stderr with exit 1 and no help.
type Fatal string

func (e Fatal) Error() string { return string(e) }

var (
	// ErrInterrupted ends a run silently with exit 130.
	ErrInterrupted = errors.New("interrupted")
	// ErrNotOK means a result was printed and at least one verdict failed: exit 1, nothing more to print.
	ErrNotOK = errors.New("not ok")
)

// ExitUsage is the exit code of a usage error; the caller adds the command's help to its sentence.
const ExitUsage = 2

// Code maps a run's error to the exit code and the sentence for stderr. An error of another kind
// (a cobra parse error can echo the command line) is a usage error with an empty sentence.
func Code(err error) (exit int, stderr string) {
	var f Fatal
	var u Usage
	switch {
	case err == nil:
		return 0, ""
	case errors.Is(err, ErrInterrupted):
		return 130, ""
	case errors.Is(err, ErrNotOK):
		return 1, ""
	case errors.As(err, &f):
		return 1, string(f)
	case errors.As(err, &u):
		return ExitUsage, string(u)
	}
	return ExitUsage, ""
}
