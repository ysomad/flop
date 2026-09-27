# Shop pagination example

```sh
docker compose up -d --wait
go run .
```

Find manual or Stripe payments created during 2024, with amounts from 150000
up to (but excluding) 1000000. Sort by amount descending, then capture time
ascending:

```sh
curl -sS http://localhost:8080/cursor-payments \
  -H 'content-type: application/json' \
  -d '{
    "page_size": 50,
    "order_by": "amount desc, captured_at",
    "filter": "(provider = manual OR provider = stripe) AND amount >= 150000 AND amount < 1000000 AND created_at >= \"2024-01-01T00:00:00Z\" AND created_at < \"2025-01-01T00:00:00Z\""
  }'
```

For the next page, send this body to the same endpoint, replacing `...` with
the returned `next_cursor`. Keep the order and filter unchanged; page size may
change between requests. Stop when the response omits `next_cursor`.

```json
{
  "page_size": 25,
  "order_by": "amount desc, captured_at",
  "filter": "(provider = manual OR provider = stripe) AND amount >= 150000 AND amount < 1000000 AND created_at >= \"2024-01-01T00:00:00Z\" AND created_at < \"2025-01-01T00:00:00Z\"",
  "cursor": "..."
}
```

Copy `next_cursor` whole: it is base64 with no padding, so a clipped token
still decodes as base64 and is only caught by the position it cuts short, as
`flop: invalid cursor: position "created_at" is truncated`.

For an offset example, exclude manual and PayPal payments, match user IDs
starting with `usr_0`, and restrict amount and capture time. Return page 3,
ordered by provider, amount descending, then creation time descending. The
response includes `total_items` and `total_pages` for this filter:

```sh
curl -sS http://localhost:8080/offset-payments \
  -H 'content-type: application/json' \
  -d '{
    "page": 3,
    "page_size": 25,
    "order_by": "provider, amount desc, created_at desc",
    "filter": "NOT (provider = manual OR provider = paypal) AND user_id = \"usr_0*\" AND amount >= 500000 AND amount < 5000000 AND captured_at >= \"2022-01-02T00:00:00Z\""
  }'
```

Logical operators are case-sensitive: use `AND`, `OR`, and `NOT`. Lowercase
words such as `or` are treated as search terms. Use grouped `OR` comparisons
for set membership; `IN` is unsupported. Quote timestamps and wildcard strings
as shown above; `*` matches any sequence of characters.

Page sizes default to 25 for nonpositive values and are capped at 100.
Orders use ascending direction unless followed by `desc`; `asc` is not a keyword.
The requested fields come first, then the default `captured_at desc`, then any
missing fields of the composite key `(id, created_at)`. Explicit key fields
keep their positions and directions. IDs are UUID v4 values; seeded rows reuse
IDs across creation times and retain timestamp ties to exercise pagination.

Every sortable field declares `Value` so a cursor can record its value from the
last returned payment. Sorting by `amount`, for example, needs the amount as
well as the key fields to resume at the correct position.

Configuration:

- `ADDR` defaults to `:8080`.
- `DATABASE_URL` defaults to
  `postgres://shop@localhost:5432/shop?sslmode=disable`.

Unit tests need no database: `go test -race ./...`. PostgreSQL integration tests
live in `../../flopsq`; with Docker running, use
`cd ../../flopsq && go test -race -tags=integration -count=1 ./...`.
