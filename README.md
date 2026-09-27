# flop

[![ci](https://github.com/ysomad/flop/actions/workflows/ci.yml/badge.svg)](https://github.com/ysomad/flop/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/ysomad/flop/graph/badge.svg)](https://codecov.io/gh/ysomad/flop)
[![Go Reference](https://pkg.go.dev/badge/github.com/ysomad/flop.svg)](https://pkg.go.dev/github.com/ysomad/flop)

Declarative AIP-132 ordering, AIP-160 filtering, and cursor or page-number
pagination for Go 1.27+. Everything lives in one dependency-free package:
`WhereSQL`, `SeekSQL` and `OrderBySQL` render named SQL arguments, and the
separate `flopsq` module integrates with Squirrel.

## Quickstart

Install `github.com/ysomad/flop/flopsq` with `go get`, save this as `main.go`,
and run `go run .`. It builds a PostgreSQL query and pages sample rows without
connecting to a database. The same example runs in [Go tests](flopsq/example_test.go).

```go
package main

import (
	"fmt"
	"log"

	sq "github.com/Masterminds/squirrel"
	"github.com/ysomad/flop"
	"github.com/ysomad/flop/flopsq"
)

func main() {
	if err := quickstart(); err != nil {
		log.Fatal(err)
	}
}

func quickstart() error {
	type payment struct{ ID, Amount int64 }
	schema, err := flop.NewSchema(
		flop.NewField("id").Int().Unique().Value(func(p payment) any { return p.ID }),
		flop.NewField("amount").Int().Filterable().Sortable().Value(func(p payment) any { return p.Amount }),
	).Build()
	if err != nil {
		return err
	}
	filter, err := schema.ParseFilter("amount >= 100")
	if err != nil {
		return err
	}
	defaults, err := schema.ParseOrder("amount desc")
	if err != nil {
		return err
	}
	requested, err := schema.ParseOrder("")
	if err != nil {
		return err
	}
	order := schema.TotalOrder(flop.MergeOrder(defaults, requested))
	after, err := schema.DecodeCursor("", order, filter) // First page.
	if err != nil {
		return err
	}
	base := sq.Select("id", "amount").From("payments").PlaceholderFormat(sq.Dollar)
	query, err := flopsq.CursorQuery(base, schema, order, filter, after, 2, 0)
	if err != nil {
		return err
	}
	sql, args, err := query.ToSql()
	if err != nil {
		return err
	}
	fmt.Println(sql, args)
	// Replace these sample rows with the query result, including its surplus row.
	rows := []payment{{ID: 1, Amount: 300}, {ID: 2, Amount: 200}, {ID: 3, Amount: 100}}
	page, err := schema.NewCursorPage(rows, 2, order, filter)
	if err != nil {
		return err
	}
	fmt.Println(page.Items, page.NextCursor != "")
	return nil
}
```

Send `page.NextCursor` to `DecodeCursor` on the next request with the same
order and filter. Page size may change. See [the shop example](examples/shop)
for database execution and HTTP handlers.

## Contracts

- Public field paths consist of segments passed to `NewField`; no segment may
  begin with an ASCII digit. `field1` is valid. `Ref` defaults to the path and
  must be a trusted backend reference: adapters embed it directly in SQL.
- `Filterable` and `Sortable` allow client use. `Implicit` searches string
  fields with bare values. `Unique` declares a single-field key and implies
  `Sortable`. For a composite key, declare sortable fields and use
  `NewSchema(...).CompositeKey("id", "created_at")`. It requires at least two
  distinct fields and cannot be combined with `Unique()`. Key fields follow
  the order of the arguments.
  Cursor orders require every key field, non-null columns, and a `Value`
  accessor for every ordering field. Nil callbacks are declaration errors.
- `ParseOrder` parses and validates. Apply `TotalOrder(MergeOrder(defaults, requested))`
  explicitly. Requested terms win; unused defaults precede missing unique-key
  fields. Explicit key-field placement is preserved. Repeated orders fail at
  query and cursor boundaries.
- Cursors retain version 1 and support `bool`, `int64`, `uint64`, `float64`,
  `string`, `[]byte`, `time.Time`, and `time.Duration`. Nulls, non-finite floats,
  zero times and unsupported types are rejected. Bindings detect changed orders
  and filters; they are **not authentication**. Tokens are unsigned, so enforce
  authorization independently.
- Page sizes must be positive. `CursorQuery` fetches one surplus row;
  `NewCursorPage` trims it and encodes the last retained row. For page numbers,
  use `OffsetQuery` and `NewOffsetPage`; zero selects page one. Count with the
  same filter to obtain filtered totals.

## Syntax

Ordering is `amount desc, id`: ascending is the default, with no `asc` suffix.
Use backticks for unusual order path segments, doubling embedded backticks.

| Filter field type | Operators |
| --- | --- |
| string | `=` `!=` `:` |
| int, float, time, duration | `=` `!=` `<` `<=` `>` `>=` |
| bool | `=` `!=` |

Filters support parentheses, `AND`, `OR`, `NOT`, unary `-`, and implicit AND
between terms. As in AIP-160, **OR binds more tightly than AND**. Functions and
collection membership are not supported. Both quote styles accept arbitrary
length strings and Go character escapes, plus escaped quotes and `\*`.
Numbers and booleans must be unquoted; timestamps must be quoted RFC 3339, such
as `created_at >= "2024-01-01T00:00:00Z"`. Durations use Go syntax (`250ms`).

`:` searches for a substring. `*` in a string argument becomes a LIKE wildcard;
`%`, `_`, and backslashes are escaped. Escaping `*` does not make it literal.
Unquoted `null` supports only `=` and `!=`.

## Adapters and errors

`CompileFilter` and `CompileSeek` produce `And`, `Or`, `Not`, and `Cmp` values.
`ValidateExpr` checks manually constructed trees; nil roots match everything,
while nil nested operands, empty groups and pointer nodes are invalid.

Use one `SQLBuilder` per query so named arguments stay unique. Its `Args()`
returns a copy suitable for `pgx.NamedArgs`. `flopsq` preserves Squirrel's
placeholder format and provides `Query`, `OffsetQuery`, and `CursorQuery`.

Use `errors.Is` with `ErrInvalidFilter`, `ErrInvalidOrder`, `ErrInvalidCursor`,
`ErrCursorMismatch`, `ErrInvalidPageSize`, `ErrInvalidPage`, or `ErrInvalidSkip`
for request errors. `ErrDeclaration` identifies schema, accessor or expression
setup mistakes.

## Tests

Run `go build ./...`, `go vet ./...`, and `go test -race ./...` in each of `.`,
`flopsq`, and `examples/shop`. Ordinary tests do not require Docker.

PostgreSQL integration tests live only in `flopsq`. With Docker running:

```sh
cd flopsq
go test -race -tags=integration -count=1 ./...
```

Testcontainers starts `postgres:17-alpine` on a dynamic port and cleans it up.
Container startup failures fail the run. CI runs this suite on a Docker-enabled
Ubuntu runner.

## Credits

Parser code derives from [luci-go](https://github.com/luci/luci-go); see [NOTICE](NOTICE).
The APIs follow [AIP-132](https://google.aip.dev/132),
[AIP-158](https://google.aip.dev/158), and [AIP-160](https://google.aip.dev/160).
