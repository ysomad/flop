package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysomad/flop"
	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
	"github.com/ysomad/flop/flopsq"
)

type payment struct {
	ID         string    `json:"id"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
	CapturedAt time.Time `json:"captured_at"`
	Provider   string    `json:"provider"`
	UserID     string    `json:"user_id"`
}

type listRequest struct {
	PageSize int32  `json:"page_size"`
	OrderBy  string `json:"order_by"`
	Filter   string `json:"filter"`
}

type cursorRequest struct {
	listRequest
	Cursor string `json:"cursor"`
	Skip   int32  `json:"skip"`
}

type offsetRequest struct {
	listRequest
	Page int32 `json:"page"`
}

// OrderBy in both responses is the order the paginator resolved, including the
// key columns it appends, not the order the client asked for.
type cursorResponse struct {
	Items      []payment `json:"items"`
	OrderBy    string    `json:"order_by"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type offsetResponse struct {
	Items      []payment `json:"items"`
	OrderBy    string    `json:"order_by"`
	Page       int32     `json:"page"`
	TotalPages int64     `json:"total_pages"`
	TotalItems int64     `json:"total_items"`
}

var (
	psql     = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	payments = mustPaymentSchema()
)

// defaultOrder sorts newest first. A client order takes precedence over it,
// and ParseOrder appends id so that every order is total.
var defaultOrder = []aip132.OrderBy{
	{FieldPath: aip132.NewFieldPath("captured_at"), Descending: true},
}

const (
	defaultPageSize = 25
	maxPageSize     = 100
)

// A column defaults to the field's path, so the demo declares none: it selects
// from one table whose columns are named exactly as clients name the fields. A
// joined query would qualify them, and only the column changes:
//
//	flop.NewField("user_id").Column("u.id").String().Filterable().Sortable().Build(),
func mustPaymentSchema() *flop.Schema {
	return flop.NewSchema(
		flop.NewField("id").String().Filterable().Unique().
			Value(func(p payment) any { return p.ID }).Build(),
		flop.NewField("amount").Int().Filterable().Sortable().Build(),
		flop.NewField("created_at").Time().Filterable().Build(),
		flop.NewField(
			"captured_at",
		).
			Value(func(p payment) any { return p.CapturedAt }).
			Time().
			Filterable().
			Sortable().
			Build(),
		flop.NewField("provider").String().Filterable().Sortable().Implicit().Build(),
		flop.NewField("user_id").String().Filterable().Sortable().Build(),
	).MustBuild()
}

// pageSize clamps what a client asks for. flop takes no view on it: it refuses
// a page size that is not positive and leaves the bounds to the endpoint.
func pageSize(requested int32) int32 {
	switch {
	case requested <= 0:
		return defaultPageSize
	case requested > maxPageSize:
		return maxPageSize
	}
	return requested
}

var db *pgxpool.Pool

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var err error

	db, err = pgxpool.New(ctx, env("DATABASE_URL", "postgres://shop@localhost:5432/shop?sslmode=disable"))
	if err != nil {
		slog.Error("configure database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, 2*time.Minute)
	if err := migrate(migrationCtx, db); err != nil {
		cancelMigration()
		slog.Error("migrate database", "error", err)
		os.Exit(1)
	}
	cancelMigration()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /cursor-payments", handleCursorPayments)
	mux.HandleFunc("POST /offset-payments", handleOffsetPayments)

	server := &http.Server{
		Addr:    env("ADDR", ":8080"),
		Handler: mux,
	}

	slog.Info("server start", "address", server.Addr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		slog.Info("server stop")

		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "error", err)
			os.Exit(1)
		}
	}
}

func handleCursorPayments(w http.ResponseWriter, r *http.Request) {
	var req cursorRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	order, f, err := parseListRequest(req.listRequest)
	if err != nil {
		writeListError(w, err)
		return
	}

	size := pageSize(req.PageSize)

	after, err := payments.DecodeCursor(req.Cursor, order, f)
	if err != nil {
		writeListError(w, err)
		return
	}

	rows, err := listCursorPayments(r.Context(), order, f, after, size, req.Skip)
	if err != nil {
		writeListError(w, err)
		return
	}

	// The query asked for one row more than the page holds, and that surplus
	// row is what says another page follows.
	page, err := payments.CursorPage(rows, size, order, f)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, cursorResponse{
		Items:      page.Items,
		OrderBy:    aip132.OrderByString(order),
		NextCursor: page.NextCursor,
	})
}

func handleOffsetPayments(w http.ResponseWriter, r *http.Request) {
	var req offsetRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	order, f, err := parseListRequest(req.listRequest)
	if err != nil {
		writeListError(w, err)
		return
	}

	size := pageSize(req.PageSize)

	items, total, err := listOffsetPayments(r.Context(), order, f, req.Page, size)
	if err != nil {
		writeListError(w, err)
		return
	}

	page, err := flop.NewOffsetPage(items, req.Page, size, total)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, offsetResponse{
		Items:      page.Items,
		OrderBy:    aip132.OrderByString(order),
		Page:       page.Page,
		TotalPages: page.TotalPages,
		TotalItems: page.TotalItems,
	})
}

func baseQuery() sq.SelectBuilder {
	return psql.
		Select("id", "amount", "created_at", "captured_at", "provider", "user_id").
		From("payments")
}

func listCursorPayments(
	ctx context.Context,
	order []aip132.OrderBy,
	f *aip160.Filter,
	after flop.CursorPosition,
	size, skip int32,
) ([]payment, error) {
	b, err := flopsq.CursorQuery(baseQuery(), payments, order, f, after, size, skip)
	if err != nil {
		return nil, err
	}

	sql, args, err := b.ToSql()
	if err != nil {
		return nil, fmt.Errorf("cursor: %w", err)
	}

	slog.Info("[cursor]", "sql", sql, "args", args)

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	return scanPayments(rows)
}

func listOffsetPayments(
	ctx context.Context,
	order []aip132.OrderBy,
	f *aip160.Filter,
	page, size int32,
) ([]payment, int64, error) {
	b, err := flopsq.OffsetQuery(baseQuery(), payments, order, f, page, size)
	if err != nil {
		return nil, 0, err
	}

	sql, args, err := b.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("offset: %w", err)
	}

	slog.Info("[offset] list payments", "sql", sql, "args", args)

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}

	items, err := scanPayments(rows)
	if err != nil {
		return nil, 0, err
	}

	// The count repeats the filter and nothing else, so rows and totals agree
	// on which payments the page is drawn from.
	count, err := flopsq.Query(
		psql.Select("count(*)").From("payments"),
		payments, nil, f,
	)
	if err != nil {
		return nil, 0, err
	}

	sql, args, err = count.ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("offset count: %w", err)
	}

	slog.Info("[offset] count", "sql", sql, "args", args)

	var total int64

	if err := db.QueryRow(ctx, sql, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func scanPayments(rows pgx.Rows) ([]payment, error) {
	defer rows.Close()
	pp := make([]payment, 0)
	for rows.Next() {
		var p payment
		if err := rows.Scan(&p.ID, &p.Amount, &p.CreatedAt, &p.CapturedAt, &p.Provider, &p.UserID); err != nil {
			return nil, err
		}
		pp = append(pp, p)
	}
	return pp, rows.Err()
}

// parseListRequest validates the order and filter a client sent against the
// schema, so an endpoint rejects a bad one before it reaches the database.
func parseListRequest(req listRequest) ([]aip132.OrderBy, *aip160.Filter, error) {
	f, err := payments.ParseFilter(req.Filter)
	if err != nil {
		return nil, nil, err
	}

	requested, err := aip132.ParseOrderBy(req.OrderBy)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", flop.ErrInvalidOrder, err)
	}

	// The default fills in the fields the client did not name, and the
	// tie-breaker is appended afterwards so that it sorts last.
	order := payments.TotalOrder(flop.MergeOrder(defaultOrder, requested))

	if err := payments.ValidateOrder(order); err != nil {
		return nil, nil, err
	}

	return order, f, nil
}

func writeListError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, flop.ErrInvalidFilter),
		errors.Is(err, flop.ErrInvalidOrder),
		errors.Is(err, flop.ErrInvalidCursor),
		errors.Is(err, flop.ErrCursorMismatch),
		errors.Is(err, flop.ErrInvalidPageSize),
		errors.Is(err, flop.ErrInvalidPage),
		errors.Is(err, flop.ErrInvalidSkip):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeInternalError(w, err)
	}
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, errors.New("internal server error"))
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
