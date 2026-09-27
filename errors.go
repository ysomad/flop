package flop

import "fmt"

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrInvalidFilter   = Error("flop: invalid filter")
	ErrInvalidCursor   = Error("flop: invalid cursor")
	ErrCursorMismatch  = Error("flop: cursor does not match")
	ErrInvalidOrder    = Error("flop: invalid order")
	ErrInvalidPageSize = Error("flop: invalid page size")
	ErrInvalidPage     = Error("flop: invalid page number")
	ErrInvalidSkip     = Error("flop: invalid skip")
	ErrDeclaration     = Error("flop: invalid declaration")
)

func errorf(err error, format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{err}, args...)...)
}
