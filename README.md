# flop

[![ci](https://github.com/ysomad/flop/actions/workflows/ci.yml/badge.svg)](https://github.com/ysomad/flop/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/ysomad/flop/graph/badge.svg)](https://codecov.io/gh/ysomad/flop)
[![Go Reference](https://pkg.go.dev/badge/github.com/ysomad/flop.svg)](https://pkg.go.dev/github.com/ysomad/flop)

Declarative AIP-132 ordering, AIP-160 filtering, and cursor or page-number
pagination for Go 1.22+

| Package | Contents |
| --- | --- |
| `flop` | schema, filter compiler, ordering, cursors, page numbers |
| [`flop/aip160`](./aip160) | AIP-160 filter parser and syntax tree |
| [`flop/aip132`](./aip132) | AIP-132 order_by parser |
| [`flop/rawsql`](./rawsql) | SQL text and named arguments |
| [`flop/flopsq`](./flopsq) | [squirrel](https://github.com/Masterminds/squirrel) query builders |


## Declare a schema

A schema declares the fields a collection exposes and what each may be used
for. Everything is built on it, and each part is optional: an endpoint may
filter without sorting, sort without paging, or page without filtering.

```go
var payments = flop.NewSchema(
	flop.NewField("id").String().Unique().
		Value(func(p payment) any { return p.ID }).Build(),
	flop.NewField("amount").Int().Filterable().Sortable().
		Value(func(p payment) any { return p.Amount }).Build(),
	flop.NewField("captured_at").Time().Filterable().Sortable().
		Value(func(p payment) any { return p.CapturedAt }).Build(),
	flop.NewField("provider").String().Filterable().Implicit().Build(),
	flop.NewField("tenant_id").Column("t.id").String().Filterable().Build(),
).MustBuild()
```

A field carries two names. Its **path** is the public contract, what a client
writes in a filter or `order_by`. Its **column** is a storage detail written
into generated SQL verbatim, so it must be a constant and never user input. The
column defaults to the path, so only a column that differs — qualified, or named
differently in storage — is written out.

| declaration | meaning |
| --- | --- |
| `Filterable()` | may be named in a filter |
| `Sortable()` | may be named in an `order_by` |
| `Unique()` | orders rows totally; the tie-breaker cursor paging needs. Implies `Sortable()`, at most one per schema |
| `Implicit()` | a bare filter value searches this field. Implies `Filterable()`, string fields only |
| `Value(fn)` | how a row supplies this field, which is how a cursor addresses it |

`Value` is only read when a field appears in an order, so a filter-only field
never declares one.

## Filtering

```go
filter, err := payments.ParseFilter(r.FormValue("filter"))
```

The [AIP-160 grammar](https://google.aip.dev/assets/misc/ebnf-filtering.txt)
Parsing validates every restriction against the schema:
the field is declared and filterable, the operator is one its type accepts, and
the argument coerces to that type.

| type | operators |
| --- | --- |
| string | `=` `!=` `:` |
| int, float, time | `=` `!=` `<` `<=` `>` `>=` |
| bool | `=` `!=` |

A `*` in a string argument makes the restriction a pattern match whichever
comparator was written: `id = "*pay_00*"` becomes `LIKE '%pay\_00%'`, and
`provider:"stri*"` becomes `LIKE 'stri%'` rather than the `%…%` the has
operator searches with by default. A bare value with no field name searches
every field declared `Implicit()`. Comparing to `null` with `=` or `!=` becomes
`IS NULL` / `IS NOT NULL`.

## Ordering

```go
order, err := payments.ParseOrder(r.FormValue("order_by"))
```

[AIP-132](https://google.aip.dev/132): a field sorts ascending unless followed
by `desc`, and `asc` is not a keyword. Every term must name a distinct sortable
field. The schema's unique field is appended when the clause does not already
name it, so the order is total — cursor paging needs that to avoid repeating or
dropping rows.

`MergeOrder(def, order)` combines a request with an endpoint's default, and
`Schema.TotalOrder` appends the tie-breaker, for composing an order by hand:

```go
order := payments.TotalOrder(flop.MergeOrder(defaultOrder, requested))
```

## Cursor pagination

The query selects one row more than the page holds; that surplus row is what
reports whether another page follows.

```go
after, err := payments.DecodeCursor(req.Cursor, order, filter)

q, err := flopsq.CursorQuery(base, payments, order, filter, after, size, req.Skip)
rows := query(q)

page, err := payments.CursorPage(rows, size, order, filter)
// page.Items, page.NextCursor
```

`CursorQuery` takes the page size and adds the surplus row itself. A token is
bound to the order and filter it was issued under, so a client cannot carry a
position over to a different query: it comes back as `ErrCursorMismatch`. `Skip`
is the offset from the position the cursor names that
[AIP-158](https://google.aip.dev/158) allows, and is deliberately not part of
that binding.

Every ordering column must be `NOT NULL`. A SQL comparison against NULL is
unknown, which would silently drop rows from the page.

## Page-number pagination

```go
offset, err := flop.Offset(req.Page, size)
q, err := flopsq.OffsetQuery(base, payments, order, filter, req.Page, size)

page, err := flop.NewOffsetPage(items, req.Page, size, total)
// page.Items, page.Page, page.TotalPages, page.TotalItems
```

## Backends

Query generation lives outside the core package. `Schema.Compile` turns a
validated filter into a tree of `And`, `Or`, `Not` and `Cmp` whose values are
already coerced to Go types, and `Schema.CompileSeek` compiles the keyset
comparison into the same tree, so a backend walks one shape for both.

**flopsq** builds squirrel expressions rather than SQL text.

```go
base := psql.Select("id", "amount").From("payments").
	Where(squirrel.Eq{"tenant_id": req.Tenant})

q, err := flopsq.CursorQuery(base, payments, order, filter, after, size, skip)
```

`Query` adds the filter and order with no pagination, which is what a count
query alongside a page wants. `Where`, `OrderBy` and `Seek` return the
individual pieces.

**rawsql** renders SQL text and named arguments for callers assembling a query
by hand. Every fragment omits its keyword and is parenthesized, so it drops into
a query without knowing what surrounds it. Use one `Builder` per query, so that
argument names stay unique across its fragments.

```go
b := rawsql.NewBuilder()
where, err := b.Where(payments, filter)
seek, err := b.Seek(payments, order, after)
orderBy, err := rawsql.OrderBy(payments, order)

rows, err := db.Query(ctx, "SELECT * FROM payments WHERE "+where+
	" AND "+seek+" ORDER BY "+orderBy+" LIMIT $1", pgx.NamedArgs(b.Args()))
```

## Credits

- [luci-go](https://github.com/luci/luci-go)
- [Einride AIP Go implementation](https://github.com/einride/aip-go)
- [Google AIP-132: Ordering](https://google.aip.dev/132)
- [Google AIP-158: Pagination](https://google.aip.dev/158)
- [Google AIP-160: Filtering](https://google.aip.dev/160)
- [AIP-160 filtering EBNF](https://google.aip.dev/assets/misc/ebnf-filtering.txt)
- [GitLab pagination guidelines](https://docs.gitlab.com/development/database/pagination_guidelines/)
