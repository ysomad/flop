// Package flop implements declarative AIP-132 ordering, AIP-160 filtering, and
// cursor or page-number pagination.
//
// A [Schema] declares the fields a collection exposes and which of them may be
// filtered, sorted or paged by. Everything else is built on it, and each part
// is optional: a schema may filter without sorting or sort without paging, and
// page-number pagination needs no schema.
//
//	var users = flop.NewSchema(
//		flop.NewField("id").Ref("u.id").Int().Unique(),
//		flop.NewField("display_name").Ref("u.name").String().Filterable().Sortable().Implicit(),
//		flop.NewField("created_at").Ref("u.created_at").Time().Filterable().Sortable(),
//	).MustBuild()
//
// A field carries a public path and a private backend ref. The path is what a
// client writes in a filter or order_by clause. The ref is an opaque value a
// backend uses to address the field and must be a trusted constant.
//
// [Schema.ParseFilter] and [Schema.ParseOrder] validate a request and return
// the AIP types. flop holds no request state of its own: an order, a filter and
// a decoded cursor position are passed to a backend as they are.
//
//	filter, err := users.ParseFilter(r.FormValue("filter"))
//	order, err := users.ParseOrder(r.FormValue("order_by"))
//	after, err := users.DecodeCursor(r.FormValue("page_token"), order, filter)
//
// A query selects one row more than the page holds. [Schema.CursorPage] splits
// that surplus row off and mints the token the next page continues from.
// Page-number pagination uses [Offset] for the query and [NewOffsetPage] for
// the page it returns.
//
// Query generation lives outside this package. [Schema.Compile] turns a
// validated filter into a tree of [And], [Or], [Not] and [Cmp] nodes whose
// values are already coerced to Go types, which a backend walks to emit
// whatever its database speaks. Two backends ship with flop:
// github.com/ysomad/flop/rawsql for SQL text and named arguments, and
// github.com/ysomad/flop/flopsq for the squirrel query
// builder.
//
// Errors returned for bad input wrap [ErrInvalidFilter], [ErrInvalidOrder],
// [ErrInvalidCursor], [ErrCursorMismatch], [ErrInvalidPage], [ErrInvalidPageSize]
// or [ErrInvalidSkip], so a handler can map them to a 400. [ErrDeclaration]
// marks a mistake in the schema or in how the library was called, which no
// request can provoke.
package flop
