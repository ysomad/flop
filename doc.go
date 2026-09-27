// Package flop implements AIP-132 ordering, AIP-160 filtering, and cursor or
// page-number pagination for declared collection fields.
//
// A Schema separates public field paths from trusted backend references. Field
// path segments must not begin with an ASCII digit. ParseFilter and ParseOrder
// validate client input; apply TotalOrder(MergeOrder(defaults, requested)) after
// parsing to append a unique tie-breaker behind the defaults.
//
// Cursor ordering columns must be non-null. Each needs a FieldBuilder.Value
// accessor, and the order must include every field declared by CompositeKey (or
// the single field declared Unique). Query for pageSize+1 rows
// and pass them to Schema.NewCursorPage. Cursor bindings detect changed orders
// and filters; they provide neither authentication nor authorization.
//
// Schema.CompileFilter and Schema.CompileSeek produce And, Or, Not and Cmp value
// nodes. ValidateExpr checks manually assembled trees for adapters. Query
// generation lives in github.com/ysomad/flop/rawsql (named SQL arguments) and
// github.com/ysomad/flop/flopsq (squirrel builders).
//
// Invalid client input wraps ErrInvalidFilter, ErrInvalidOrder, ErrInvalidCursor,
// ErrCursorMismatch, ErrInvalidPage, ErrInvalidPageSize or ErrInvalidSkip.
// ErrDeclaration identifies invalid schema, expression or accessor setup.
package flop
