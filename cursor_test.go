package flop

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"

	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
)

// cursorRow is the row the cursor tests address.
type cursorRow struct {
	ID        string
	CreatedAt time.Time
	V         string
	BoolFalse bool
	BoolTrue  bool
	Int       int64
	Uint      uint64
	Float     float64
	String    string
	Bytes     []byte
	Time      time.Time
	Duration  time.Duration
	Null      any
	Bad       any
}

// cursorSchema declares every field the cursor tests order by. Only id is
// unique, so an order that has to be total names it last.
var cursorSchema = NewSchema(
	NewField("id").Column("id").String().Unique().
		Value(func(r cursorRow) any { return r.ID }).Build(),
	NewField("created_at").Column("created_at").Time().Sortable().
		Value(func(r cursorRow) any { return r.CreatedAt }).Build(),
	NewField("active").Column("active").Bool().Filterable().Build(),
	NewField("v").Column("v").String().Sortable().
		Value(func(r cursorRow) any { return r.V }).Build(),
	NewField("bool_false").Column("bool_false").Bool().Sortable().
		Value(func(r cursorRow) any { return r.BoolFalse }).Build(),
	NewField("bool_true").Column("bool_true").Bool().Sortable().
		Value(func(r cursorRow) any { return r.BoolTrue }).Build(),
	NewField("int_value").Column("int_value").Int().Sortable().
		Value(func(r cursorRow) any { return r.Int }).Build(),
	NewField("uint_value").Column("uint_value").Int().Sortable().
		Value(func(r cursorRow) any { return r.Uint }).Build(),
	NewField("float_value").Column("float_value").Float().Sortable().
		Value(func(r cursorRow) any { return r.Float }).Build(),
	NewField("string_value").Column("string_value").String().Sortable().
		Value(func(r cursorRow) any { return r.String }).Build(),
	NewField("bytes_value").Column("bytes_value").String().Sortable().
		Value(func(r cursorRow) any { return r.Bytes }).Build(),
	NewField("time_value").Column("time_value").Time().Sortable().
		Value(func(r cursorRow) any { return r.Time }).Build(),
	NewField("duration_value").Column("duration_value").Int().Sortable().
		Value(func(r cursorRow) any { return r.Duration }).Build(),
	NewField("null_value").Column("null_value").String().Sortable().
		Value(func(r cursorRow) any { return r.Null }).Build(),
	NewField("bad_value").Column("bad_value").String().Sortable().
		Value(func(r cursorRow) any { return r.Bad }).Build(),
	NewField("no_value").Column("no_value").String().Sortable().Build(),
).MustBuild()

var cursorCreatedAt = time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

func TestSchema_DecodeCursor(t *testing.T) {
	t.Parallel()
	orderBy, err := aip132.ParseOrderBy("created_at desc, id")
	assert.NoError(t, err)
	activeFilter, err := aip160.ParseFilter("active = true")
	assert.NoError(t, err)
	row := cursorRow{ID: "users/1", CreatedAt: cursorCreatedAt}
	position := CursorPosition{
		{FieldPath: aip132.NewFieldPath("created_at"), Value: cursorCreatedAt},
		{FieldPath: aip132.NewFieldPath("id"), Value: "users/1"},
	}
	token, err := cursorSchema.EncodeCursor(row, orderBy, activeFilter)
	assert.NoError(t, err)
	// A null is not a valid position value, so every other kind is covered here
	// and the null rejection is exercised by TestSchema_EncodeCursor.
	allValuesRow := cursorRow{
		BoolTrue: true,
		Int:      math.MinInt64,
		Uint:     math.MaxUint64,
		Float:    1.5,
		String:   "value",
		Bytes:    []byte("value"),
		Time:     cursorCreatedAt,
		Duration: -time.Second,
	}
	allValues := CursorPosition{
		{FieldPath: aip132.NewFieldPath("bool_false"), Value: false},
		{FieldPath: aip132.NewFieldPath("bool_true"), Value: true},
		{FieldPath: aip132.NewFieldPath("int_value"), Value: int64(math.MinInt64)},
		{FieldPath: aip132.NewFieldPath("uint_value"), Value: uint64(math.MaxUint64)},
		{FieldPath: aip132.NewFieldPath("float_value"), Value: float64(1.5)},
		{FieldPath: aip132.NewFieldPath("string_value"), Value: "value"},
		{FieldPath: aip132.NewFieldPath("bytes_value"), Value: []byte("value")},
		{FieldPath: aip132.NewFieldPath("time_value"), Value: cursorCreatedAt},
		{FieldPath: aip132.NewFieldPath("duration_value"), Value: -time.Second},
	}
	allValuesOrder, err := aip132.ParseOrderBy(
		"bool_false, bool_true, int_value, uint_value, float_value," +
			" string_value, bytes_value, time_value, duration_value",
	)
	assert.NoError(t, err)
	allValuesToken, err := cursorSchema.EncodeCursor(allValuesRow, allValuesOrder, activeFilter)
	assert.NoError(t, err)
	otherFilter, err := aip160.ParseFilter("active = false")
	assert.NoError(t, err)
	otherOrderBy, err := aip132.ParseOrderBy("id")
	assert.NoError(t, err)
	unsortableOrder, err := aip132.ParseOrderBy("active")
	assert.NoError(t, err)
	emptyBinding := newCursorBinding(nil, nil)
	// A single-field order the hand-written tokens below are issued under, so
	// that they reach the value they are testing instead of failing the binding.
	valueOrder, err := aip132.ParseOrderBy("v")
	assert.NoError(t, err)
	valueBinding := newCursorBinding(valueOrder, nil)
	encode := func(plaintext []byte) string { return base64.RawURLEncoding.EncodeToString(plaintext) }
	value := func(raw ...byte) string { return encode(cursorPlaintext(valueBinding, raw)) }
	trailing := append(cursorPlaintext(valueBinding, []byte{byte(boolKind), 1}), 0)
	nonFinite := binary.BigEndian.AppendUint64([]byte{byte(float64Kind)}, math.Float64bits(math.NaN()))

	type args struct {
		token   string
		orderBy []aip132.OrderBy
		filter  *aip160.Filter
	}
	tests := []struct {
		name    string
		args    args
		want    CursorPosition
		wantErr errorFunc
	}{
		{
			name:    "first page has no token",
			args:    args{orderBy: orderBy, filter: activeFilter},
			wantErr: assert.NoError,
		},
		{
			name:    "token",
			args:    args{token: token, orderBy: orderBy, filter: activeFilter},
			want:    position,
			wantErr: assert.NoError,
		},
		{
			name:    "all cursor value kinds",
			args:    args{token: allValuesToken, orderBy: allValuesOrder, filter: activeFilter},
			want:    allValues,
			wantErr: assert.NoError,
		},
		{
			name:    "changed order",
			args:    args{token: token, orderBy: otherOrderBy, filter: activeFilter},
			wantErr: assert.Error,
		},
		{
			name:    "changed filter",
			args:    args{token: token, orderBy: orderBy, filter: otherFilter},
			wantErr: assert.Error,
		},
		{
			name:    "order names an unsortable field",
			args:    args{token: token, orderBy: unsortableOrder, filter: activeFilter},
			wantErr: assert.Error,
		},
		{name: "invalid base64", args: args{token: "*"}, wantErr: assert.Error},
		{name: "unsupported version", args: args{token: encode([]byte{2})}, wantErr: assert.Error},
		{name: "short binding", args: args{token: encode([]byte{1})}, wantErr: assert.Error},
		{
			// The binding is all the cursor carries of the request it was
			// issued for, so a token bearing another one is rejected before any
			// of its values are read.
			name: "binding mismatch",
			args: args{
				token:   encode(cursorPlaintext(emptyBinding, []byte{byte(boolKind), 1})),
				orderBy: valueOrder,
			},
			wantErr: assert.Error,
		},
		{
			name:    "missing position value",
			args:    args{token: encode(cursorPlaintext(valueBinding)), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "trailing data",
			args:    args{token: encode(trailing), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "token without an order",
			args:    args{token: encode(cursorPlaintext(emptyBinding))},
			wantErr: assert.Error,
		},
		{
			name:    "invalid bool",
			args:    args{token: value(byte(boolKind), 2), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "short int64",
			args:    args{token: value(byte(int64Kind), 0), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "short uint64",
			args:    args{token: value(byte(uint64Kind), 0), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "short float64",
			args:    args{token: value(byte(float64Kind), 0), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "non-finite float64",
			args:    args{token: value(nonFinite...), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "invalid string length",
			args:    args{token: value(byte(stringKind), 2, 'a'), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "invalid bytes length",
			args:    args{token: value(byte(bytesKind), 2, 'a'), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "invalid time length",
			args:    args{token: value(byte(timeKind), 2, 'a'), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "invalid time",
			args:    args{token: value(byte(timeKind), 3, 'b', 'a', 'd'), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "short duration",
			args:    args{token: value(byte(durationKind), 0), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "unknown value kind",
			args:    args{token: value(math.MaxUint8), orderBy: valueOrder},
			wantErr: assert.Error,
		},
		{
			// No kind is tagged zero, so the padding a truncated token reads as
			// never decodes into a value.
			name:    "zero value kind",
			args:    args{token: value(0), orderBy: valueOrder},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := cursorSchema.DecodeCursor(
				test.args.token,
				test.args.orderBy,
				test.args.filter,
			)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, got)
		})
	}
}

func TestSchema_EncodeCursor(t *testing.T) {
	t.Parallel()
	idOrder, err := aip132.ParseOrderBy("id")
	assert.NoError(t, err)
	unsortableOrder, err := aip132.ParseOrderBy("active")
	assert.NoError(t, err)
	noValueOrder, err := aip132.ParseOrderBy("no_value")
	assert.NoError(t, err)
	nullOrder, err := aip132.ParseOrderBy("null_value")
	assert.NoError(t, err)
	badOrder, err := aip132.ParseOrderBy("bad_value")
	assert.NoError(t, err)
	floatOrder, err := aip132.ParseOrderBy("float_value")
	assert.NoError(t, err)
	timeOrder, err := aip132.ParseOrderBy("time_value")
	assert.NoError(t, err)
	row := cursorRow{ID: "users/1", Time: cursorCreatedAt}

	type args struct {
		row     any
		orderBy []aip132.OrderBy
	}
	tests := []struct {
		name    string
		args    args
		want    CursorPosition
		wantErr errorFunc
	}{
		{
			name: "row",
			args: args{row: row, orderBy: idOrder},
			want: CursorPosition{
				{FieldPath: aip132.NewFieldPath("id"), Value: "users/1"},
			},
			wantErr: assert.NoError,
		},
		{
			name: "time value",
			args: args{row: row, orderBy: timeOrder},
			want: CursorPosition{
				{FieldPath: aip132.NewFieldPath("time_value"), Value: cursorCreatedAt},
			},
			wantErr: assert.NoError,
		},
		{
			// The order is what names the fields a cursor addresses, so there
			// is nothing to issue one against without it.
			name:    "no order",
			args:    args{row: row},
			wantErr: assert.Error,
		},
		{
			name:    "order names an unsortable field",
			args:    args{row: row, orderBy: unsortableOrder},
			wantErr: assert.Error,
		},
		{
			name:    "ordering field declares no value",
			args:    args{row: row, orderBy: noValueOrder},
			wantErr: assert.Error,
		},
		{
			name:    "row of another type",
			args:    args{row: struct{ ID string }{ID: "users/1"}, orderBy: idOrder},
			wantErr: assert.Error,
		},
		{
			name:    "null value",
			args:    args{row: row, orderBy: nullOrder},
			wantErr: assert.Error,
		},
		{
			name:    "unsupported value type",
			args:    args{row: cursorRow{Bad: struct{}{}}, orderBy: badOrder},
			wantErr: assert.Error,
		},
		{
			name:    "non-finite value",
			args:    args{row: cursorRow{Float: math.Inf(1)}, orderBy: floatOrder},
			wantErr: assert.Error,
		},
		{
			name:    "zero time value",
			args:    args{row: cursorRow{}, orderBy: timeOrder},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := cursorSchema.EncodeCursor(test.args.row, test.args.orderBy, nil)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			// A token is only worth minting if it reads back as the position it
			// was minted from, under the order and filter it was bound to.
			back, err := cursorSchema.DecodeCursor(got, test.args.orderBy, nil)
			assert.NoError(t, err)
			assert.Equal(t, test.want, back)
		})
	}
}

func TestSchema_CursorPage(t *testing.T) {
	t.Parallel()
	idOrder, err := aip132.ParseOrderBy("id")
	assert.NoError(t, err)
	noValueOrder, err := aip132.ParseOrderBy("no_value")
	assert.NoError(t, err)
	rows := []cursorRow{{ID: "1"}, {ID: "2"}, {ID: "3"}}

	type args struct {
		rows     []cursorRow
		pageSize int32
		orderBy  []aip132.OrderBy
	}
	type page struct {
		items     []cursorRow
		hasCursor bool
	}
	tests := []struct {
		name    string
		args    args
		want    page
		wantErr errorFunc
	}{
		{
			name:    "last page mints no token",
			args:    args{rows: rows[:2], pageSize: 2, orderBy: idOrder},
			want:    page{items: rows[:2]},
			wantErr: assert.NoError,
		},
		{
			name:    "empty page",
			args:    args{rows: []cursorRow{}, pageSize: 2, orderBy: idOrder},
			want:    page{items: []cursorRow{}},
			wantErr: assert.NoError,
		},
		{
			name:    "surplus row is trimmed and mints a token",
			args:    args{rows: rows, pageSize: 2, orderBy: idOrder},
			want:    page{items: rows[:2], hasCursor: true},
			wantErr: assert.NoError,
		},
		{
			name:    "zero page size keeps every row",
			args:    args{rows: rows, orderBy: idOrder},
			want:    page{items: rows},
			wantErr: assert.NoError,
		},
		{
			// The token is only minted for a next page, so an order that
			// cannot mint one still serves the last page.
			name:    "last page under an order that cannot mint a token",
			args:    args{rows: rows[:2], pageSize: 2, orderBy: noValueOrder},
			want:    page{items: rows[:2]},
			wantErr: assert.NoError,
		},
		{
			name:    "next page under an order that cannot mint a token",
			args:    args{rows: rows, pageSize: 2, orderBy: noValueOrder},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := cursorSchema.CursorPage(
				test.args.rows,
				test.args.pageSize,
				test.args.orderBy,
				nil,
			)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want.items, got.Items)
			assert.Equal(t, test.want.hasCursor, got.NextCursor != "")
		})
	}
}

// cursorPlaintext assembles the bytes a cursor carries: a version, the binding
// it was issued under, and one kind-tagged value per ordering field.
func cursorPlaintext(binding cursorBinding, values ...[]byte) []byte {
	plaintext := append([]byte{cursorVersion}, binding[:]...)
	for _, value := range values {
		plaintext = append(plaintext, value...)
	}
	return plaintext
}
