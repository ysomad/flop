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
// nodes. ValidateExpr checks manually assembled trees for adapters. Squirrel
// builders live in github.com/ysomad/flop/flopsq.
//
// # Parsers
//
// ParseOrderBy parses an AIP-132 order_by clause, with the field path support
// of AIP-161. ParseFilter parses an AIP-160 filter expression, whose grammar is
// at https://google.aip.dev/assets/misc/ebnf-filtering.txt; function call syntax
// is not supported. Both parse without a schema; the Schema methods of the same
// name parse and validate against declared fields.
//
// # SQL rendering
//
// WhereSQL, WhereExprSQL, SeekSQL and OrderBySQL render a schema, filter and
// order as SQL text and named arguments, for callers assembling a query by hand.
// Every fragment omits its keyword and is parenthesized, so it drops into a query
// without needing to know what surrounds it. Identifiers come only from the
// columns a schema declares and values only from bound arguments, so nothing a
// client sends reaches the SQL text.
//
// Arguments bind by name, written "@name" and returned as a map. Pass it to a
// driver that reads names, such as pgx.NamedArgs(args), or hand each entry to
// sql.Named. Markers are named after the field they read, so a fragment reads
// the same wherever it lands in the query. Use one SQLBuilder per query when it
// takes more than one fragment: the package-level functions each count from one,
// so combining their output would collide on names.
//
// Cursor pagination selects pageSize+1 rows: the surplus row is what
// [Schema.NewCursorPage] uses to detect a next page.
//
// Invalid client input wraps ErrInvalidFilter, ErrInvalidOrder, ErrInvalidCursor,
// ErrCursorMismatch, ErrInvalidPage, ErrInvalidPageSize or ErrInvalidSkip.
// ErrDeclaration identifies invalid schema, expression or accessor setup.
package flop
