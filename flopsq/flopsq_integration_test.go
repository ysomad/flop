//go:build integration

package flopsq_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/ysomad/flop"
	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
	"github.com/ysomad/flop/flopsq"
	"github.com/ysomad/flop/internal/assert"
)

type integrationRow struct {
	ID        int64
	Score     int64
	Name      string
	CreatedAt time.Time
}

var integrationSchema = flop.NewSchema(
	flop.NewField("id").Int().Filterable().Unique().Value(func(r integrationRow) any { return r.ID }),
	flop.NewField("score").Int().Filterable().Sortable().Value(func(r integrationRow) any { return r.Score }),
	flop.NewField("name").
		String().
		Filterable().
		Sortable().
		Implicit().
		Value(func(r integrationRow) any { return r.Name }),
	flop.NewField("created_at").Time().Filterable().Sortable().Value(func(r integrationRow) any { return r.CreatedAt }),
	flop.NewField("note").String().Filterable(),
	flop.NewField("active").Bool().Filterable(),
).MustBuild()

// integrationPayment is keyed by two fields, so no single column separates two
// of its rows.
type integrationPayment struct {
	ID         string
	CreatedAt  time.Time
	CapturedAt time.Time
	Amount     int64
}

var integrationPaymentSchema = flop.NewSchema(
	flop.NewField("id").String().Sortable().Value(func(p integrationPayment) any { return p.ID }),
	flop.NewField("created_at").Time().Sortable().Value(func(p integrationPayment) any { return p.CreatedAt }),
	flop.NewField("captured_at").Time().Sortable().Value(func(p integrationPayment) any { return p.CapturedAt }),
	flop.NewField("amount").Int().Sortable().Value(func(p integrationPayment) any { return p.Amount }),
).CompositeKey("id", "created_at").MustBuild()

var integrationPayments = func() []integrationPayment {
	stamp := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	const (
		idA = "00000000-0000-4000-8000-000000000001"
		idB = "00000000-0000-4000-8000-000000000002"
		idC = "00000000-0000-4000-8000-000000000003"
	)
	return []integrationPayment{
		{ID: idA, CreatedAt: stamp, CapturedAt: stamp, Amount: 10},
		{ID: idA, CreatedAt: stamp.Add(time.Second), CapturedAt: stamp, Amount: 10},
		{ID: idB, CreatedAt: stamp, CapturedAt: stamp, Amount: 20},
		{ID: idB, CreatedAt: stamp.Add(time.Second), CapturedAt: stamp, Amount: 20},
		{ID: idA, CreatedAt: stamp.Add(2 * time.Second), CapturedAt: stamp, Amount: 20},
		{ID: idC, CreatedAt: stamp, CapturedAt: stamp, Amount: 10},
	}
}()

// integrationConn is the connection every test in this file shares, opened once
// against the container TestMain runs.
var integrationConn *pgx.Conn

func TestMain(m *testing.M) {
	code, err := runPostgres(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runPostgres(m *testing.M) (int, error) {
	startup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(
		startup,
		"postgres:17-alpine",
		postgres.WithDatabase("flop"),
		postgres.WithUsername("flop"),
		postgres.WithPassword("flop"),
		testcontainers.WithWaitStrategyAndDeadline(90*time.Second,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	)
	if container != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := container.Terminate(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "terminate postgres: %v\n", err)
			}
		}()
	}
	if err != nil {
		return 0, fmt.Errorf("start postgres: %w", err)
	}
	// ConnectionString resolves the dynamically mapped host and port.
	dsn, err := container.ConnectionString(startup, "sslmode=disable")
	if err != nil {
		return 0, fmt.Errorf("resolve dsn: %w", err)
	}
	conn, err := pgx.Connect(startup, dsn)
	if err != nil {
		return 0, fmt.Errorf("connect postgres: %w", err)
	}
	// Close the connection before terminating its container.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := conn.Close(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "close postgres: %v\n", err)
		}
	}()
	if err := conn.Ping(startup); err != nil {
		return 0, fmt.Errorf("ping postgres: %w", err)
	}
	integrationConn = conn
	cancel()
	return m.Run(), nil
}

func TestQuery_Postgres(t *testing.T) {
	type args struct {
		filter string
	}
	tests := []struct {
		name string
		args args
		want []int64
	}{
		{name: "numeric and boolean", args: args{filter: "score >= 20 AND active = true"}, want: []int64{1, 4, 6, 8}},
		{name: "or and negation", args: args{filter: "(id = 1 OR id = 2) AND NOT active = true"}, want: []int64{2}},
		{name: "null", args: args{filter: "note = null"}, want: []int64{1, 3, 6}},
		{name: "not null", args: args{filter: "note != null"}, want: []int64{2, 4, 5, 7, 8}},
		{name: "escaped like", args: args{filter: `name:"100%_a\\b"`}, want: []int64{1}},
		{name: "wildcard", args: args{filter: `name = "alpha*"`}, want: []int64{3, 4}},
		{name: "not wildcard", args: args{filter: `name != "alpha*"`}, want: []int64{1, 2, 5, 6, 7, 8}},
		{name: "single quoted text", args: args{filter: `name = 'O\'Reilly'`}, want: []int64{8}},
		{name: "implicit search", args: args{filter: "alpha"}, want: []int64{3, 4}},
		{name: "quoted timestamp", args: args{filter: `created_at >= "2024-01-03T00:00:00Z"`}, want: []int64{1, 2, 8}},
		{name: "empty result", args: args{filter: "score < 0"}, want: []int64{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := integrationFixture(t, integrationItemsFixture)
			order, filter := integrationRequest(t, integrationSchema, "created_at desc", "id", test.args.filter)
			query, err := flopsq.Query(integrationSelect(), integrationSchema, order, filter)
			assert.NoError(t, err)
			assert.Equal(t, test.want, integrationIDs(integrationSelectRows(t, tx, query)))
		})
	}
}

func TestCursorQuery_Postgres(t *testing.T) {
	type args struct {
		order  string
		filter string
		// sizes and skips are cycled page by page, so one row can change the
		// page size mid-traversal.
		sizes []int32
		skips []int32
	}
	tests := []struct {
		name      string
		traversal func(*testing.T) integrationTraversal
		args      args
		want      []string
	}{
		{
			name:      "default with ties",
			traversal: integrationItemTraversal,
			args:      args{sizes: []int32{2}},
			want:      integrationItemKeys(1, 2, 8, 3, 4, 5, 6, 7),
		},
		{
			name:      "requested then defaults",
			traversal: integrationItemTraversal,
			args:      args{order: "score desc", sizes: []int32{1}},
			want:      integrationItemKeys(1, 2, 8, 4, 6, 3, 5, 7),
		},
		{
			name:      "mixed directions changing sizes",
			traversal: integrationItemTraversal,
			args:      args{order: "score, created_at desc, id desc", sizes: []int32{3, 1, 2}},
			want:      integrationItemKeys(5, 3, 7, 8, 2, 1, 4, 6),
		},
		{
			name:      "unique first",
			traversal: integrationItemTraversal,
			args:      args{order: "id desc, score", sizes: []int32{3}},
			want:      integrationItemKeys(8, 7, 6, 5, 4, 3, 2, 1),
		},
		{
			name:      "filtered traversal",
			traversal: integrationItemTraversal,
			args:      args{filter: "score >= 20 AND active = true", sizes: []int32{1, 2}},
			want:      integrationItemKeys(1, 8, 4, 6),
		},
		{
			name:      "empty first page",
			traversal: integrationItemTraversal,
			args:      args{filter: "score < 0", sizes: []int32{2}},
			want:      []string{},
		},
		{
			// A skip is applied to every page, so it moves the window without
			// changing the order the cursor walks.
			name:      "skip per page",
			traversal: integrationItemTraversal,
			args:      args{sizes: []int32{1, 2, 2}, skips: []int32{2, 1, 100}},
			want:      integrationItemKeys(8, 4, 5),
		},

		{
			name:      "composite key by default",
			traversal: integrationPaymentTraversal,
			args:      args{sizes: []int32{1, 2}},
			want:      integrationPaymentKeys(0, 1, 4, 2, 3, 5),
		},
		{
			name:      "composite key behind another field",
			traversal: integrationPaymentTraversal,
			args:      args{order: "amount desc", sizes: []int32{1, 2}},
			want:      integrationPaymentKeys(4, 2, 3, 0, 1, 5),
		},
		{
			name:      "composite key fields descending",
			traversal: integrationPaymentTraversal,
			args:      args{order: "created_at desc, id desc", sizes: []int32{1, 2}},
			want:      integrationPaymentKeys(4, 3, 1, 5, 2, 0),
		},
		{
			name:      "composite key id first descending",
			traversal: integrationPaymentTraversal,
			args:      args{order: "id desc", sizes: []int32{1, 2}},
			want:      integrationPaymentKeys(5, 2, 3, 0, 1, 4),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			traversal := test.traversal(t)
			order, filter := integrationRequest(
				t, traversal.schema, traversal.defaults, test.args.order, test.args.filter,
			)
			got := []string{}
			token := ""
			for pageNumber := 0; ; pageNumber++ {
				if pageNumber > len(test.want)+1 {
					t.Fatal("cursor failed to terminate")
				}
				size := test.args.sizes[pageNumber%len(test.args.sizes)]
				skip := int32(0)
				if len(test.args.skips) > 0 {
					skip = test.args.skips[pageNumber%len(test.args.skips)]
				}
				keys, next := traversal.page(order, filter, token, size, skip)
				got = append(got, keys...)
				if next == "" {
					break
				}
				token = next
			}
			assert.Equal(t, test.want, got)
			// A skipping traversal stops before the end, so only a complete one
			// can be paged past its last row.
			if len(got) == 0 || len(test.args.skips) > 0 || traversal.tokenAfterLast == nil {
				return
			}
			keys, next := traversal.page(order, filter, traversal.tokenAfterLast(order, filter), 2, 0)
			assert.Equal(t, 0, len(keys))
			assert.Equal(t, "", next)
		})
	}
}

func TestOffsetQuery_Postgres(t *testing.T) {
	type args struct {
		page int32
	}
	tests := []struct {
		name string
		args args
		want []int64
	}{
		{name: "zero is first", args: args{}, want: []int64{1, 8}},
		{name: "second", args: args{page: 2}, want: []int64{4, 6}},
		{name: "past end", args: args{page: 3}, want: []int64{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := integrationFixture(t, integrationItemsFixture)
			order, filter := integrationRequest(
				t, integrationSchema, "created_at desc", "", "score >= 20 AND active = true",
			)
			// The total a page reports comes from the same filter, so the count
			// runs through flopsq as well.
			countQuery, err := flopsq.Query(
				sq.Select("count(*)").From("items").PlaceholderFormat(sq.Dollar),
				integrationSchema, nil, filter,
			)
			assert.NoError(t, err)
			query, args, err := countQuery.ToSql()
			assert.NoError(t, err)
			var total int64
			assert.NoError(t, tx.QueryRow(t.Context(), query, args...).Scan(&total))
			assert.Equal(t, int64(4), total)

			builder, err := flopsq.OffsetQuery(
				integrationSelect(), integrationSchema, order, filter, test.args.page, 2,
			)
			assert.NoError(t, err)
			rows := integrationSelectRows(t, tx, builder)
			page, err := flop.NewOffsetPage(rows, test.args.page, 2, total)
			assert.NoError(t, err)
			assert.Equal(t, test.want, integrationIDs(page.Items))
			assert.Equal(t, int64(4), page.TotalItems)
			assert.Equal(t, int64(2), page.TotalPages)
		})
	}
}

// integrationTraversal is one fixture a cursor row walks: the schema and default
// order its request is parsed against, and how to page it.
type integrationTraversal struct {
	schema   *flop.Schema
	defaults string
	page     func(order []aip132.OrderBy, filter *aip160.Filter, token string, size, skip int32) ([]string, string)
	// tokenAfterLast mints a token from the last row the pager handed out, so a
	// traversal can be walked past its end.
	tokenAfterLast func(order []aip132.OrderBy, filter *aip160.Filter) string
}

func integrationItemTraversal(t *testing.T) integrationTraversal {
	t.Helper()
	tx := integrationFixture(t, integrationItemsFixture)
	var last integrationRow
	return integrationTraversal{
		schema:   integrationSchema,
		defaults: "created_at desc",
		page: func(order []aip132.OrderBy, filter *aip160.Filter, token string, size, skip int32) ([]string, string) {
			after, err := integrationSchema.DecodeCursor(token, order, filter)
			assert.NoError(t, err)
			query, err := flopsq.CursorQuery(
				integrationSelect(), integrationSchema, order, filter, after, size, skip,
			)
			assert.NoError(t, err)
			page, err := integrationSchema.NewCursorPage(
				integrationSelectRows(t, tx, query), size, order, filter,
			)
			assert.NoError(t, err)
			if len(page.Items) > int(size) {
				t.Fatalf("page exceeds size %d", size)
			}
			if len(page.Items) > 0 {
				last = page.Items[len(page.Items)-1]
			}
			if page.NextCursor != "" {
				assert.Equal(t, int(size), len(page.Items))
				// The token addresses the last row of the page it came with.
				position, err := integrationSchema.DecodeCursor(page.NextCursor, order, filter)
				assert.NoError(t, err)
				values := map[string]any{
					"id":         last.ID,
					"score":      last.Score,
					"name":       last.Name,
					"created_at": last.CreatedAt,
				}
				for _, value := range position {
					assert.Equal(t, values[value.FieldPath.String()], value.Value)
				}
			}
			keys := make([]string, 0, len(page.Items))
			for _, row := range page.Items {
				keys = append(keys, strconv.FormatInt(row.ID, 10))
			}
			return keys, page.NextCursor
		},
		tokenAfterLast: func(order []aip132.OrderBy, filter *aip160.Filter) string {
			token, err := integrationSchema.EncodeCursor(last, order, filter)
			assert.NoError(t, err)
			return token
		},
	}
}

func integrationPaymentTraversal(t *testing.T) integrationTraversal {
	t.Helper()
	tx := integrationFixture(t, integrationPaymentsFixture)
	base := sq.Select("id", "created_at", "captured_at", "amount").
		From("payments").
		PlaceholderFormat(sq.Dollar)
	return integrationTraversal{
		schema:   integrationPaymentSchema,
		defaults: "captured_at desc",
		page: func(order []aip132.OrderBy, filter *aip160.Filter, token string, size, skip int32) ([]string, string) {
			after, err := integrationPaymentSchema.DecodeCursor(token, order, filter)
			assert.NoError(t, err)
			query, err := flopsq.CursorQuery(
				base, integrationPaymentSchema, order, filter, after, size, skip,
			)
			assert.NoError(t, err)
			sql, args, err := query.ToSql()
			assert.NoError(t, err)
			result, err := tx.Query(t.Context(), sql, args...)
			assert.NoError(t, err)
			rows, err := pgx.CollectRows(result, pgx.RowToStructByPos[integrationPayment])
			assert.NoError(t, err)
			page, err := integrationPaymentSchema.NewCursorPage(rows, size, order, filter)
			assert.NoError(t, err)
			keys := make([]string, 0, len(page.Items))
			for _, row := range page.Items {
				keys = append(keys, integrationPaymentKey(row))
			}
			return keys, page.NextCursor
		},
	}
}

const integrationItemsFixture = `CREATE TEMP TABLE items (
	id bigint PRIMARY KEY, score bigint NOT NULL, name text NOT NULL,
	created_at timestamptz NOT NULL, note text, active boolean NOT NULL
);
INSERT INTO items VALUES
	(1,20,'100%_a\b','2024-01-03 00:00:00+00',NULL,true),
	(2,20,'100XXa\b','2024-01-03 00:00:00+00','ok',false),
	(3,10,'alpha','2024-01-02 00:00:00+00',NULL,true),
	(4,20,'alphabet','2024-01-02 00:00:00+00','',true),
	(5,10,'beta','2024-01-02 00:00:00+00','x',false),
	(6,20,'gamma','2024-01-01 00:00:00+00',NULL,true),
	(7,10,'delta','2024-01-01 00:00:00+00','y',true),
	(8,20,'O''Reilly','2024-01-03 00:00:00+00','z',true);`

const integrationPaymentsFixture = `CREATE TEMP TABLE payments (
	id uuid NOT NULL, created_at timestamptz NOT NULL,
	captured_at timestamptz NOT NULL, amount bigint NOT NULL,
	PRIMARY KEY (id, created_at)
);`

func integrationFixture(t *testing.T, ddl string) pgx.Tx {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	tx, err := integrationConn.Begin(ctx)
	assert.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tx.Rollback(ctx); err != nil {
			t.Errorf("rollback fixture: %v", err)
		}
	})
	_, err = tx.Exec(ctx, ddl)
	assert.NoError(t, err)
	if ddl != integrationPaymentsFixture {
		return tx
	}
	for _, row := range integrationPayments {
		_, err := tx.Exec(ctx,
			"INSERT INTO payments VALUES ($1,$2,$3,$4)",
			row.ID, row.CreatedAt, row.CapturedAt, row.Amount,
		)
		assert.NoError(t, err)
	}
	return tx
}

func integrationRequest(
	t *testing.T,
	schema *flop.Schema,
	defaultOrder, orderText, filterText string,
) ([]aip132.OrderBy, *aip160.Filter) {
	t.Helper()
	requested, err := schema.ParseOrder(orderText)
	assert.NoError(t, err)
	defaults, err := schema.ParseOrder(defaultOrder)
	assert.NoError(t, err)
	filter, err := schema.ParseFilter(filterText)
	assert.NoError(t, err)
	return schema.TotalOrder(flop.MergeOrder(defaults, requested)), filter
}

func integrationSelect() sq.SelectBuilder {
	return sq.Select("id", "score", "name", "created_at").From("items").PlaceholderFormat(sq.Dollar)
}

func integrationSelectRows(t *testing.T, tx pgx.Tx, builder sq.SelectBuilder) []integrationRow {
	t.Helper()
	query, args, err := builder.ToSql()
	assert.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	rows, err := tx.Query(ctx, query, args...)
	assert.NoError(t, err)
	defer rows.Close()
	result := []integrationRow{}
	for rows.Next() {
		var row integrationRow
		assert.NoError(t, rows.Scan(&row.ID, &row.Score, &row.Name, &row.CreatedAt))
		result = append(result, row)
	}
	assert.NoError(t, rows.Err())
	return result
}

func integrationIDs(rows []integrationRow) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// integrationItemKeys names the items a traversal row expects, in order.
func integrationItemKeys(ids ...int64) []string {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, strconv.FormatInt(id, 10))
	}
	return keys
}

// integrationPaymentKeys names the payment fixtures, by index, that a traversal
// row expects in order.
func integrationPaymentKeys(indices ...int) []string {
	keys := make([]string, 0, len(indices))
	for _, index := range indices {
		keys = append(keys, integrationPaymentKey(integrationPayments[index]))
	}
	return keys
}

func integrationPaymentKey(p integrationPayment) string {
	return p.ID + "/" + p.CreatedAt.UTC().Format(time.RFC3339Nano)
}
