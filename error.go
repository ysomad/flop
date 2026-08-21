package flop

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidFilter reports a filter flop refuses: one that does not parse,
	// names an undeclared field, carries a value of the wrong type, uses an
	// operator the field does not accept, or arrives at an endpoint where
	// filtering is disabled.
	ErrInvalidFilter = errors.New("flop: invalid filter")

	// ErrInvalidCursor reports a cursor that is malformed, truncated, or
	// carries a value that cannot address a row.
	ErrInvalidCursor = errors.New("flop: invalid cursor")

	// ErrCursorMismatch reports a well-formed cursor issued for a different
	// order or filter than the one accompanying it.
	ErrCursorMismatch = errors.New("flop: cursor does not match")

	// ErrInvalidOrder reports an order naming an undeclared field, repeating a
	// field, or using a malformed path.
	ErrInvalidOrder = errors.New("flop: invalid order")

	// ErrInvalidPageSize reports a negative page size.
	ErrInvalidPageSize = errors.New("flop: invalid page size")

	// ErrInvalidPage reports a negative page number.
	ErrInvalidPage = errors.New("flop: invalid page number")

	// ErrInvalidSkip reports a negative number of results to skip.
	ErrInvalidSkip = errors.New("flop: invalid skip")

	// ErrDeclaration reports an endpoint declared wrongly. Constructors return
	// it and no input can provoke it, so it always marks a programmer mistake.
	ErrDeclaration = errors.New("flop: invalid declaration")
)

// errorf builds an error that leads with sentinel and details it with format,
// rendering as "flop: invalid order: unknown field \"total\"".
func errorf(sentinel error, format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{sentinel}, args...)...)
}
