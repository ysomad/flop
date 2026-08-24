# flop

[![ci](https://github.com/ysomad/flop/actions/workflows/ci.yml/badge.svg)](https://github.com/ysomad/flop/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/ysomad/flop/graph/badge.svg)](https://codecov.io/gh/ysomad/flop)
[![Go Reference](https://pkg.go.dev/badge/github.com/ysomad/flop.svg)](https://pkg.go.dev/github.com/ysomad/flop)

Declarative AIP-132 ordering, AIP-160 filtering, and cursor or page-number
pagination for Go 1.27+

| Package | Contents |
| --- | --- |
| `flop` | schema, filter compiler, ordering, cursors, page numbers |
| [`flop/aip160`](./aip160) | AIP-160 filter parser and syntax tree |
| [`flop/aip132`](./aip132) | AIP-132 order_by parser |
| [`flop/rawsql`](./rawsql) | SQL text and named arguments |
| [`flop/flopsq`](./flopsq) | [squirrel](https://github.com/Masterminds/squirrel) query builders |

## Schema

```go
var payments = flop.NewSchema(
	flop.NewField("id").String().Unique().Value(func(p payment) any { return p.ID }),
	flop.NewField("amount").Int().Filterable().Sortable(),
	flop.NewField("captured_at").Time().Filterable().Sortable(),
	flop.NewField("processing_time").Duration().Filterable().Sortable(),
	flop.NewField("provider").String().Filterable().Implicit(),
	flop.NewField("tenant_id").Ref("t.id").String().Filterable(),
).MustBuild()
```

A field has a public path clients write, and a backend `Ref` that defaults to it.

| declaration | meaning |
| --- | --- |
| `Filterable()` | may be named in a filter |
| `Sortable()` | may be named in an `order_by` |
| `Unique()` | tie-breaker for cursor paging; implies `Sortable()`, at most one per schema |
| `Implicit()` | a bare filter value searches this field; implies `Filterable()`, string fields only |
| `Value(fn)` | how a row supplies this field to a cursor |

## Filtering

```go
filter, err := payments.ParseFilter(r.FormValue("filter"))
```

| type | operators |
| --- | --- |
| string | `=` `!=` `:` |
| int, float, time, duration | `=` `!=` `<` `<=` `>` `>=` |
| bool | `=` `!=` |

`time` takes RFC 3339, `duration` takes Go syntax (`250ms`, `2h30m`). A `*` in a
string argument makes it a `LIKE` pattern, `null` becomes `IS NULL`.

## Ordering

```go
order, err := payments.ParseOrder(r.FormValue("order_by"))
order = payments.TotalOrder(flop.MergeOrder(defaultOrder, order))
```

## Cursor pagination

```go
after, err := payments.DecodeCursor(req.Cursor, order, filter)

q, err := flopsq.CursorQuery(base, payments, order, filter, after, size, req.Skip)
rows := query(q)

page, err := payments.CursorPage(rows, size, order, filter)
// page.Items, page.NextCursor
```

## Page-number pagination

```go
offset, err := flop.Offset(req.Page, size)
q, err := flopsq.OffsetQuery(base, payments, order, filter, req.Page, size)

page, err := flop.NewOffsetPage(items, req.Page, size, total)
// page.Items, page.Page, page.TotalPages, page.TotalItems
```

## Backends

`Schema.Compile` and `Schema.CompileSeek` turn a filter and a cursor position
into a tree of `And`, `Or`, `Not` and `Cmp` a backend walks.

**flopsq** builds squirrel expressions:

```go
base := psql.Select("id", "amount").From("payments").
	Where(squirrel.Eq{"tenant_id": req.Tenant})

q, err := flopsq.CursorQuery(base, payments, order, filter, after, size, skip)
```

**rawsql** renders SQL text and named arguments; use one `Builder` per query:

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
