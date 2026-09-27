package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ysomad/flop"
	"github.com/ysomad/flop/internal/assert"
)

func Test_parseListRequest(t *testing.T) {
	t.Parallel()
	type args struct {
		order  string
		filter string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr error
	}{
		{name: "defaults", want: "captured_at desc, id, created_at"},
		{
			name: "cursor example",
			args: args{order: "amount desc", filter: "provider = stripe AND amount >= 1000"},
			want: "amount desc, captured_at desc, id, created_at",
		},
		{
			name: "offset example",
			args: args{order: "provider, amount desc", filter: `created_at >= "2024-01-01T00:00:00Z"`},
			want: "provider, amount desc, captured_at desc, id, created_at",
		},
		{
			name: "ascending timestamp",
			args: args{order: "amount, captured_at", filter: `provider = "stripe" AND amount = 0`},
			want: "amount, captured_at, id, created_at",
		},
		{name: "explicit unique", args: args{order: "id desc"}, want: "id desc, captured_at desc, created_at"},
		{name: "explicit time", args: args{order: "created_at desc"}, want: "created_at desc, captured_at desc, id"},
		{name: "unsupported asc", args: args{order: "amount asc"}, wantErr: flop.ErrInvalidOrder},
		{name: "unknown field", args: args{order: "missing"}, wantErr: flop.ErrInvalidOrder},
		{name: "duplicate", args: args{order: "amount, amount desc"}, wantErr: flop.ErrInvalidOrder},
		{
			name:    "unquoted timestamp",
			args:    args{filter: "created_at >= 2024-01-01T00:00:00Z"},
			wantErr: flop.ErrInvalidFilter,
		},
		{name: "bad filter", args: args{filter: "amount = nope"}, wantErr: flop.ErrInvalidFilter},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order, _, err := parseListRequest(listRequest{OrderBy: test.args.order, Filter: test.args.filter})
			if test.wantErr != nil {
				assert.IsError(t, err, test.wantErr)
				return
			}
			assert.NoError(t, err)
			want, err := paymentSchema.ParseOrder(test.want)
			assert.NoError(t, err)
			assert.Equal(t, want, order)
		})
	}
}

func Test_pageSize(t *testing.T) {
	t.Parallel()
	type args struct {
		requested int32
	}
	tests := []struct {
		name string
		args args
		want int32
	}{
		{name: "negative", args: args{requested: -1}, want: 25},
		{name: "default", args: args{}, want: 25},
		{name: "small", args: args{requested: 1}, want: 1},
		{name: "maximum", args: args{requested: 100}, want: 100},
		{name: "clamped", args: args{requested: 101}, want: 100},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, pageSize(test.args.requested))
		})
	}
}

func Test_writeListError(t *testing.T) {
	t.Parallel()
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{name: "filter", args: args{err: flop.ErrInvalidFilter}, want: 400},
		{name: "order", args: args{err: flop.ErrInvalidOrder}, want: 400},
		{name: "cursor", args: args{err: flop.ErrInvalidCursor}, want: 400},
		{name: "mismatch", args: args{err: flop.ErrCursorMismatch}, want: 400},
		{name: "page", args: args{err: flop.ErrInvalidPage}, want: 400},
		{name: "size", args: args{err: flop.ErrInvalidPageSize}, want: 400},
		{name: "skip", args: args{err: flop.ErrInvalidSkip}, want: 400},
		{name: "declaration", args: args{err: flop.ErrDeclaration}, want: 500},
		{name: "database", args: args{err: errors.New("private database error")}, want: 500},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			err := fmt.Errorf("wrapped: %w", test.args.err)
			writeListError(response, err)
			assert.Equal(t, test.want, response.Code)
			assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
			var body map[string]string
			assert.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			want := err.Error()
			if test.want == 500 {
				// An internal failure is not spelled out to the client.
				want = "internal server error"
			}
			assert.Equal(t, want, body["error"])
		})
	}
}

func Test_handleCursorPayments(t *testing.T) {
	t.Parallel()
	type args struct {
		body string
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{name: "malformed JSON", args: args{body: "{"}, want: 400},
		{name: "bad order", args: args{body: `{"order_by":"amount asc"}`}, want: 400},
		{name: "bad filter", args: args{body: `{"filter":"amount = nope"}`}, want: 400},
		{name: "bad cursor", args: args{body: `{"cursor":"*"}`}, want: 400},
		{name: "negative skip", args: args{body: `{"skip":-1}`}, want: 400},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/cursor-payments", strings.NewReader(test.args.body))
			handleCursorPayments(response, request)
			assert.Equal(t, test.want, response.Code)
		})
	}
}

// A cursor the schema below issued, clipped inside its created_at position.
const truncatedCursor = "Af9rJTBXn1fdrer1Co_wsFx5sigFoJXISD_OiFsGAOpgAgAAAAAAAm_zBw8BAAAADtlksdQAAAAAAaQFJGVkMjJhNjUyLWEyNDEtNGIwOC1iMjllLWZjMDY0NGQwMzUyOQcPAQAAAA7egQjzBRxF"

func Test_mustPaymentSchema(t *testing.T) {
	t.Parallel()
	first := payment{
		ID:         "00000000-0000-4000-8000-000000000001",
		Amount:     100,
		Provider:   "stripe",
		UserID:     "usr_1",
		CapturedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	first.CreatedAt = first.CapturedAt.Add(-time.Hour)
	values := map[string]any{
		"id":          first.ID,
		"amount":      first.Amount,
		"provider":    first.Provider,
		"user_id":     first.UserID,
		"captured_at": first.CapturedAt,
		"created_at":  first.CreatedAt,
	}

	// Every sortable field has to serve a cursor on its own, since a client may
	// order by any one of them.
	for _, field := range paymentSchema.Fields() {
		name := field.Path().String()
		if _, err := paymentSchema.SortableField(field.Path()); err != nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			order, filter, err := parseListRequest(listRequest{OrderBy: name, Filter: "amount >= 100"})
			assert.NoError(t, err)
			page, err := paymentSchema.NewCursorPage([]payment{first, {ID: "surplus"}}, 1, order, filter)
			assert.NoError(t, err)
			assert.Equal(t, []payment{first}, page.Items)
			if page.NextCursor == "" {
				t.Fatal("missing cursor")
			}
			pos, err := paymentSchema.DecodeCursor(page.NextCursor, order, filter)
			assert.NoError(t, err)
			for _, value := range pos {
				assert.Equal(t, values[value.FieldPath.String()], value.Value)
			}
			_, err = paymentSchema.CompileSeek(order, pos)
			assert.NoError(t, err)
		})
	}

	// The key is composite, so a cursor carries both of its fields and neither
	// alone can seek.
	t.Run("composite key", func(t *testing.T) {
		second := first
		second.CreatedAt = first.CreatedAt.Add(time.Second)
		order, filter, err := parseListRequest(listRequest{})
		assert.NoError(t, err)
		page, err := paymentSchema.NewCursorPage([]payment{first, second}, 1, order, filter)
		assert.NoError(t, err)
		pos, err := paymentSchema.DecodeCursor(page.NextCursor, order, filter)
		assert.NoError(t, err)
		assert.Equal(t, 3, len(pos))
		assert.Equal[any](t, first.ID, pos[1].Value)
		assert.Equal[any](t, first.CreatedAt, pos[2].Value)
		incomplete, err := paymentSchema.ParseOrder("id, captured_at")
		assert.NoError(t, err)
		_, err = paymentSchema.CompileSeek(incomplete, nil)
		assert.IsError(t, err, flop.ErrDeclaration)
	})

	// A token clipped in transit names the position it stops inside.
	t.Run("truncated cursor", func(t *testing.T) {
		req, _, page := paymentCursorFixture(t)
		order, filter, err := parseListRequest(req)
		assert.NoError(t, err)
		_, err = paymentSchema.DecodeCursor(truncatedCursor, order, filter)
		assert.IsError(t, err, flop.ErrInvalidCursor)
		assert.Equal(t,
			`flop: invalid cursor: position "created_at" is truncated: needs 15 bytes, 12 remain`,
			err.Error())
		assert.Equal(t, 152, len(page.NextCursor))
		assert.Equal(t, truncatedCursor, page.NextCursor[:len(truncatedCursor)])
	})
}

func Test_writeJSON(t *testing.T) {
	t.Parallel()
	req, last, page := paymentCursorFixture(t)

	response := httptest.NewRecorder()
	writeJSON(response, 200, cursorResponse{Items: page.Items, NextCursor: page.NextCursor})
	assert.Equal(t, 200, response.Code)
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	var body cursorResponse
	assert.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, page.NextCursor, body.NextCursor)

	// A token survives the round trip a client makes of it, and still addresses
	// the row it was minted for.
	requestJSON, err := json.Marshal(cursorRequest{listRequest: req, Cursor: body.NextCursor})
	assert.NoError(t, err)
	var next cursorRequest
	assert.NoError(t, json.Unmarshal(requestJSON, &next))
	order, filter, err := parseListRequest(next.listRequest)
	assert.NoError(t, err)
	pos, err := paymentSchema.DecodeCursor(next.Cursor, order, filter)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(pos))
	assert.Equal[any](t, last.Amount, pos[0].Value)
	assert.Equal(t, true, last.CapturedAt.Equal(pos[1].Value.(time.Time)))
	assert.Equal[any](t, last.ID, pos[2].Value)
	assert.Equal(t, true, last.CreatedAt.Equal(pos[3].Value.(time.Time)))
	_, err = paymentSchema.CompileSeek(order, pos)
	assert.NoError(t, err)
}

// paymentCursorFixture mints the cursor truncatedCursor is a prefix of. The
// timestamps are chosen so the encoded prefix matches the clipped token without
// assuming what its missing bytes were.
func paymentCursorFixture(t *testing.T) (listRequest, payment, flop.CursorPage[payment]) {
	t.Helper()
	req := listRequest{
		PageSize: 50,
		OrderBy:  "amount desc, captured_at",
		Filter: `(provider = manual OR provider = stripe) AND amount >= 150000 AND amount < 1000000 ` +
			`AND created_at >= "2024-01-01T00:00:00Z" AND created_at < "2025-01-01T00:00:00Z"`,
	}
	zone := time.FixedZone("UTC+7", 7*60*60)
	last := payment{
		ID:         "ed22a652-a241-4b08-b29e-fc0644d03529",
		Amount:     159731,
		CapturedAt: time.Date(2022, 1, 3, 8, 59, 0, 0, time.UTC).In(zone),
		CreatedAt:  time.Date(2024, 9, 21, 18, 35, 31, 0x051c4500, time.UTC).In(zone),
	}
	order, filter, err := parseListRequest(req)
	assert.NoError(t, err)
	rows := make([]payment, req.PageSize+1)
	rows[req.PageSize-1] = last
	page, err := paymentSchema.NewCursorPage(rows, req.PageSize, order, filter)
	assert.NoError(t, err)
	return req, last, page
}
