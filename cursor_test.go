package flop

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
	"github.com/ysomad/flop/internal/assert"
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
	NewField("id").Ref("id").String().Unique().
		Value(func(r cursorRow) any { return r.ID }),
	NewField("created_at").Ref("created_at").Time().Sortable().
		Value(func(r cursorRow) any { return r.CreatedAt }),
	NewField("active").Ref("active").Bool().Filterable(),
	NewField("v").Ref("v").String().Sortable().
		Value(func(r cursorRow) any { return r.V }),
	NewField("bool_false").Ref("bool_false").Bool().Sortable().
		Value(func(r cursorRow) any { return r.BoolFalse }),
	NewField("bool_true").Ref("bool_true").Bool().Sortable().
		Value(func(r cursorRow) any { return r.BoolTrue }),
	NewField("int_value").Ref("int_value").Int().Sortable().
		Value(func(r cursorRow) any { return r.Int }),
	NewField("uint_value").Ref("uint_value").Int().Sortable().
		Value(func(r cursorRow) any { return r.Uint }),
	NewField("float_value").Ref("float_value").Float().Sortable().
		Value(func(r cursorRow) any { return r.Float }),
	NewField("string_value").Ref("string_value").String().Sortable().
		Value(func(r cursorRow) any { return r.String }),
	NewField("bytes_value").Ref("bytes_value").String().Sortable().
		Value(func(r cursorRow) any { return r.Bytes }),
	NewField("time_value").Ref("time_value").Time().Sortable().
		Value(func(r cursorRow) any { return r.Time }),
	NewField("duration_value").Ref("duration_value").Int().Sortable().
		Value(func(r cursorRow) any { return r.Duration }),
	NewField("null_value").Ref("null_value").String().Sortable().
		Value(func(r cursorRow) any { return r.Null }),
	NewField("bad_value").Ref("bad_value").String().Sortable().
		Value(func(r cursorRow) any { return r.Bad }),
	NewField("no_value").Ref("no_value").String().Sortable(),
).MustBuild()

// cursorCompositeSchema keys rows by two fields, so neither alone can serve a
// cursor.
var cursorCompositeSchema = NewSchema(
	NewField("id").String().Sortable().Value(func(r cursorRow) any { return r.ID }),
	NewField("created_at").Time().Sortable().Value(func(r cursorRow) any { return r.CreatedAt }),
).CompositeKey("id", "created_at").MustBuild()

// cursorSeekSchema keys rows by the field a position value is stated on, so a
// seek over that field alone is total.
var cursorSeekSchema = NewSchema(
	NewField("v").Ref("v").String().Unique().Value(func(r cursorRow) any { return r.V }),
).MustBuild()

var cursorCreatedAt = time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

// cursorPlaintext assembles the bytes a cursor carries: a version, the binding
// it was issued under, and one kind-tagged value per ordering field.
func cursorPlaintext(binding cursorBinding, values ...[]byte) []byte {
	plaintext := append([]byte{cursorVersion}, binding[:]...)
	for _, value := range values {
		plaintext = append(plaintext, value...)
	}
	return plaintext
}

// Version 1: id ascending, no filter, string position "users/7".
const cursorV1Fixture = "AaJu5M4QXvhHN3FUwNp2aBox406cDiTBmiVUtc6yQN6vBQd1c2Vycy83"

func TestSchema_CompileSeek(t *testing.T) {
	t.Parallel()
	createdAtPath := aip132.NewFieldPath("created_at")
	idPath := aip132.NewFieldPath("id")
	vPath := aip132.NewFieldPath("v")
	createdAtField, err := cursorSchema.SortableField(createdAtPath)
	assert.NoError(t, err)
	idField, err := cursorSchema.SortableField(idPath)
	assert.NoError(t, err)
	vField, err := cursorSeekSchema.SortableField(vPath)
	assert.NoError(t, err)

	createdAtAsc := aip132.OrderBy{FieldPath: createdAtPath}
	createdAtDesc := aip132.OrderBy{FieldPath: createdAtPath, Descending: true}
	idAsc := aip132.OrderBy{FieldPath: idPath}
	idDesc := aip132.OrderBy{FieldPath: idPath, Descending: true}
	vAsc := aip132.OrderBy{FieldPath: vPath}
	duplicateOrder := []aip132.OrderBy{idAsc, idAsc}

	// A value the caller reaches a seek with, rather than one decoded from a
	// token, still has to be one a cursor can carry.
	vPos := func(value any) CursorPosition {
		return CursorPosition{{FieldPath: vPath, Value: value}}
	}
	vSeek := func(value any) Expr {
		return Cmp{Field: vField, Op: OpGt, Value: value}
	}

	compositeID, err := cursorCompositeSchema.SortableField(idPath)
	assert.NoError(t, err)
	compositeCreatedAt, err := cursorCompositeSchema.SortableField(createdAtPath)
	assert.NoError(t, err)
	compositeOrder, err := cursorCompositeSchema.ParseOrder("id, created_at desc")
	assert.NoError(t, err)
	idOnlyOrder, err := cursorCompositeSchema.ParseOrder("id")
	assert.NoError(t, err)
	createdAtOnlyOrder, err := cursorCompositeSchema.ParseOrder("created_at")
	assert.NoError(t, err)

	type args struct {
		order []aip132.OrderBy
		pos   CursorPosition
	}
	tests := []struct {
		name         string
		schema       *Schema
		args         args
		want         Expr
		wantSentinel error
		wantErr      assert.ErrorFunc
	}{
		{
			name:    "first page",
			args:    args{order: []aip132.OrderBy{idAsc}},
			wantErr: assert.NoError,
		},
		{
			name: "single ascending field",
			args: args{
				order: []aip132.OrderBy{idAsc},
				pos:   CursorPosition{{FieldPath: idPath, Value: "users/7"}},
			},
			want:    Cmp{Field: idField, Op: OpGt, Value: "users/7"},
			wantErr: assert.NoError,
		},
		{
			name: "single descending field",
			args: args{
				order: []aip132.OrderBy{idDesc},
				pos:   CursorPosition{{FieldPath: idPath, Value: "users/7"}},
			},
			want:    Cmp{Field: idField, Op: OpLt, Value: "users/7"},
			wantErr: assert.NoError,
		},
		{
			name: "mixed directions",
			args: args{
				order: []aip132.OrderBy{createdAtDesc, idAsc},
				pos: CursorPosition{
					{FieldPath: createdAtPath, Value: cursorCreatedAt},
					{FieldPath: idPath, Value: "users/7"},
				},
			},
			want: Or{Exprs: []Expr{
				Cmp{Field: createdAtField, Op: OpLt, Value: cursorCreatedAt},
				And{Exprs: []Expr{
					Cmp{Field: createdAtField, Op: OpEq, Value: cursorCreatedAt},
					Cmp{Field: idField, Op: OpGt, Value: "users/7"},
				}},
			}},
			wantErr: assert.NoError,
		},
		{name: "no order", args: args{}, wantErr: assert.Error},
		{
			name:    "unknown ordering field",
			args:    args{order: []aip132.OrderBy{{FieldPath: aip132.NewFieldPath("nope")}}},
			wantErr: assert.Error,
		},
		{
			name: "order without a unique field",
			args: args{
				order: []aip132.OrderBy{createdAtAsc},
				pos:   CursorPosition{{FieldPath: createdAtPath, Value: cursorCreatedAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "position does not cover the order",
			args: args{
				order: []aip132.OrderBy{createdAtDesc, idAsc},
				pos:   CursorPosition{{FieldPath: createdAtPath, Value: cursorCreatedAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "position names another field",
			args: args{
				order: []aip132.OrderBy{idAsc},
				pos:   CursorPosition{{FieldPath: createdAtPath, Value: cursorCreatedAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "null ordering value",
			args: args{
				order: []aip132.OrderBy{idAsc},
				pos:   CursorPosition{{FieldPath: idPath, Value: nil}},
			},
			wantErr: assert.Error,
		},
		{
			name:         "repeated ordering field",
			args:         args{order: duplicateOrder},
			wantSentinel: ErrInvalidOrder,
			wantErr:      assert.Error,
		},

		{name: "false value", schema: cursorSeekSchema, args: args{order: []aip132.OrderBy{vAsc}, pos: vPos(false)}, want: vSeek(false), wantErr: assert.NoError},
		{
			name:    "zero int value",
			schema:  cursorSeekSchema,
			args:    args{order: []aip132.OrderBy{vAsc}, pos: vPos(int64(0))},
			want:    vSeek(int64(0)),
			wantErr: assert.NoError,
		},
		{
			name:    "uint value",
			schema:  cursorSeekSchema,
			args:    args{order: []aip132.OrderBy{vAsc}, pos: vPos(uint64(math.MaxUint64))},
			want:    vSeek(uint64(math.MaxUint64)),
			wantErr: assert.NoError,
		},
		{
			name:    "zero float value",
			schema:  cursorSeekSchema,
			args:    args{order: []aip132.OrderBy{vAsc}, pos: vPos(float64(0))},
			want:    vSeek(float64(0)),
			wantErr: assert.NoError,
		},
		{name: "empty string value", schema: cursorSeekSchema, args: args{order: []aip132.OrderBy{vAsc}, pos: vPos("")}, want: vSeek(""), wantErr: assert.NoError},
		{
			name:    "empty bytes value",
			schema:  cursorSeekSchema,
			args:    args{order: []aip132.OrderBy{vAsc}, pos: vPos([]byte{})},
			want:    vSeek([]byte{}),
			wantErr: assert.NoError,
		},
		{
			name:    "time value",
			schema:  cursorSeekSchema,
			args:    args{order: []aip132.OrderBy{vAsc}, pos: vPos(cursorCreatedAt)},
			want:    vSeek(cursorCreatedAt),
			wantErr: assert.NoError,
		},
		{
			name:    "duration value",
			schema:  cursorSeekSchema,
			args:    args{order: []aip132.OrderBy{vAsc}, pos: vPos(-time.Second)},
			want:    vSeek(-time.Second),
			wantErr: assert.NoError,
		},
		{
			name:         "unsupported int value",
			schema:       cursorSeekSchema,
			args:         args{order: []aip132.OrderBy{vAsc}, pos: vPos(int(1))},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "typed nil pointer value",
			schema:       cursorSeekSchema,
			args:         args{order: []aip132.OrderBy{vAsc}, pos: vPos((*string)(nil))},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "not a number value",
			schema:       cursorSeekSchema,
			args:         args{order: []aip132.OrderBy{vAsc}, pos: vPos(math.NaN())},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "positive infinity value",
			schema:       cursorSeekSchema,
			args:         args{order: []aip132.OrderBy{vAsc}, pos: vPos(math.Inf(1))},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "negative infinity value",
			schema:       cursorSeekSchema,
			args:         args{order: []aip132.OrderBy{vAsc}, pos: vPos(math.Inf(-1))},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "zero time value",
			schema:       cursorSeekSchema,
			args:         args{order: []aip132.OrderBy{vAsc}, pos: vPos(time.Time{})},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},

		{
			name:   "composite key",
			schema: cursorCompositeSchema,
			args: args{
				order: compositeOrder,
				pos: CursorPosition{
					{FieldPath: idPath, Value: "same-id"},
					{FieldPath: createdAtPath, Value: cursorCreatedAt},
				},
			},
			want: Or{Exprs: []Expr{
				Cmp{Field: compositeID, Op: OpGt, Value: "same-id"},
				And{Exprs: []Expr{
					Cmp{Field: compositeID, Op: OpEq, Value: "same-id"},
					Cmp{Field: compositeCreatedAt, Op: OpLt, Value: cursorCreatedAt},
				}},
			}},
			wantErr: assert.NoError,
		},
		{
			name:         "composite key without its time field",
			schema:       cursorCompositeSchema,
			args:         args{order: idOnlyOrder},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{
			name:         "composite key without its id field",
			schema:       cursorCompositeSchema,
			args:         args{order: createdAtOnlyOrder},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schema := test.schema
			if schema == nil {
				schema = cursorSchema
			}
			got, gotErr := schema.CompileSeek(test.args.order, test.args.pos)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
				return
			}
			assert.Equal(t, test.want, got)
		})
	}
}

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
	// The empty and zero spellings of the same kinds, which a length-prefixed
	// value and a zero-valued fixed64 have to survive.
	zeroValuesOrder, err := aip132.ParseOrderBy("int_value, float_value, string_value, bytes_value")
	assert.NoError(t, err)
	zeroValuesToken, err := cursorSchema.EncodeCursor(
		cursorRow{Bytes: []byte{}}, zeroValuesOrder, activeFilter,
	)
	assert.NoError(t, err)
	zeroValues := CursorPosition{
		{FieldPath: aip132.NewFieldPath("int_value"), Value: int64(0)},
		{FieldPath: aip132.NewFieldPath("float_value"), Value: float64(0)},
		{FieldPath: aip132.NewFieldPath("string_value"), Value: ""},
		{FieldPath: aip132.NewFieldPath("bytes_value"), Value: []byte{}},
	}
	otherFilter, err := aip160.ParseFilter("active = false")
	assert.NoError(t, err)
	otherOrderBy, err := aip132.ParseOrderBy("id")
	assert.NoError(t, err)
	unsortableOrder, err := aip132.ParseOrderBy("active")
	assert.NoError(t, err)
	duplicateOrder := append(append([]aip132.OrderBy{}, otherOrderBy...), otherOrderBy...)
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
	zeroTime, err := (time.Time{}).MarshalBinary()
	assert.NoError(t, err)

	type args struct {
		token   string
		orderBy []aip132.OrderBy
		filter  *aip160.Filter
	}
	tests := []struct {
		name string
		args args
		want CursorPosition
		// wantErrMsg is a substring of the error, checked only when set.
		wantErrMsg   string
		wantSentinel error
		wantErr      assert.ErrorFunc
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
			name:    "empty and zero cursor value kinds",
			args:    args{token: zeroValuesToken, orderBy: zeroValuesOrder, filter: activeFilter},
			want:    zeroValues,
			wantErr: assert.NoError,
		},
		{
			// The wire format is versioned, so a token minted by an older
			// release still reads back.
			name:    "version 1 token",
			args:    args{token: cursorV1Fixture, orderBy: otherOrderBy},
			want:    CursorPosition{{FieldPath: aip132.NewFieldPath("id"), Value: "users/7"}},
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
		{
			name:         "repeated ordering field without a token",
			args:         args{orderBy: duplicateOrder},
			wantSentinel: ErrInvalidOrder,
			wantErr:      assert.Error,
		},
		{
			name:         "repeated ordering field with a token",
			args:         args{token: cursorV1Fixture, orderBy: duplicateOrder},
			wantSentinel: ErrInvalidOrder,
			wantErr:      assert.Error,
		},
		{name: "invalid base64", args: args{token: "*"}, wantErr: assert.Error},
		{name: "unsupported version", args: args{token: encode([]byte{2})}, wantErr: assert.Error},
		{
			// The version and the binding are read before any field is named,
			// so their failures stay unqualified.
			name:       "short binding",
			args:       args{token: encode([]byte{1})},
			wantErrMsg: "is truncated: needs 32 bytes, 0 remain",
			wantErr:    assert.Error,
		},
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
			name:       "short int64",
			args:       args{token: value(byte(int64Kind), 0), orderBy: valueOrder},
			wantErrMsg: `position "v" is truncated: needs 8 bytes, 1 remain`,
			wantErr:    assert.Error,
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
			name:         "positive infinity float64",
			args:         args{token: value(appendFixed64(nil, float64Kind, math.Float64bits(math.Inf(1)))...), orderBy: valueOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "negative infinity float64",
			args:         args{token: value(appendFixed64(nil, float64Kind, math.Float64bits(math.Inf(-1)))...), orderBy: valueOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "zero time",
			args:         args{token: value(appendVarbytes(nil, timeKind, zeroTime)...), orderBy: valueOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:       "invalid string length",
			args:       args{token: value(byte(stringKind), 2, 'a'), orderBy: valueOrder},
			wantErrMsg: `position "v" is truncated: needs 2 bytes, 1 remain`,
			wantErr:    assert.Error,
		},
		{
			name:       "invalid bytes length",
			args:       args{token: value(byte(bytesKind), 2, 'a'), orderBy: valueOrder},
			wantErrMsg: `position "v" is truncated: needs 2 bytes, 1 remain`,
			wantErr:    assert.Error,
		},
		{
			name:       "invalid time length",
			args:       args{token: value(byte(timeKind), 2, 'a'), orderBy: valueOrder},
			wantErrMsg: `position "v" is truncated: needs 2 bytes, 1 remain`,
			wantErr:    assert.Error,
		},
		{
			// What a token clipped in transit reads as: the payload is shorter
			// than the length it declares.
			name:       "truncated time payload",
			args:       args{token: value(byte(timeKind), 15, 1, 2, 3), orderBy: valueOrder},
			wantErrMsg: `position "v" is truncated: needs 15 bytes, 3 remain`,
			wantErr:    assert.Error,
		},
		{
			// A uvarint that never terminates does not decode into a length at
			// all, which is a different failure from a short payload.
			name: "unreadable length",
			args: args{
				token:   value(byte(stringKind), 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80),
				orderBy: valueOrder,
			},
			wantErrMsg: `position "v" has an unreadable length`,
			wantErr:    assert.Error,
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
			name:       "unknown value kind",
			args:       args{token: value(math.MaxUint8), orderBy: valueOrder},
			wantErrMsg: `position "v" has unknown value kind 255`,
			wantErr:    assert.Error,
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
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
				if test.wantErrMsg != "" && !strings.Contains(gotErr.Error(), test.wantErrMsg) {
					t.Errorf("error %q does not contain %q", gotErr, test.wantErrMsg)
				}
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
	duplicateOrder := append(append([]aip132.OrderBy{}, idOrder...), idOrder...)
	row := cursorRow{ID: "users/1", Time: cursorCreatedAt}

	type args struct {
		row     any
		orderBy []aip132.OrderBy
	}
	tests := []struct {
		name string
		args args
		want CursorPosition
		// wantToken is the exact token, checked only when set.
		wantToken    string
		wantSentinel error
		wantErr      assert.ErrorFunc
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
			// The wire format is versioned, so the token minted for a position
			// stays byte for byte what release 1 minted.
			name: "version 1 token",
			args: args{row: cursorRow{ID: "users/7"}, orderBy: idOrder},
			want: CursorPosition{
				{FieldPath: aip132.NewFieldPath("id"), Value: "users/7"},
			},
			wantToken: cursorV1Fixture,
			wantErr:   assert.NoError,
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
			name:         "repeated ordering field",
			args:         args{row: row, orderBy: duplicateOrder},
			wantSentinel: ErrInvalidOrder,
			wantErr:      assert.Error,
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
			name:         "null value",
			args:         args{row: row, orderBy: nullOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "unsupported value type",
			args:         args{row: cursorRow{Bad: struct{}{}}, orderBy: badOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "unsupported int value",
			args:         args{row: cursorRow{Bad: int(1)}, orderBy: badOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "typed nil pointer value",
			args:         args{row: cursorRow{Bad: (*string)(nil)}, orderBy: badOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "not a number value",
			args:         args{row: cursorRow{Float: math.NaN()}, orderBy: floatOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "non-finite value",
			args:         args{row: cursorRow{Float: math.Inf(1)}, orderBy: floatOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "negative infinity value",
			args:         args{row: cursorRow{Float: math.Inf(-1)}, orderBy: floatOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
		{
			name:         "zero time value",
			args:         args{row: cursorRow{}, orderBy: timeOrder},
			wantSentinel: ErrInvalidCursor,
			wantErr:      assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := cursorSchema.EncodeCursor(test.args.row, test.args.orderBy, nil)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
				return
			}
			if test.wantToken != "" {
				assert.Equal(t, test.wantToken, got)
			}
			// A token is only worth minting if it reads back as the position it
			// was minted from, under the order and filter it was bound to.
			back, err := cursorSchema.DecodeCursor(got, test.args.orderBy, nil)
			assert.NoError(t, err)
			assert.Equal(t, test.want, back)
		})
	}
}

func TestSchema_NewCursorPage(t *testing.T) {
	t.Parallel()
	idOrder, err := aip132.ParseOrderBy("id")
	assert.NoError(t, err)
	noValueOrder, err := aip132.ParseOrderBy("no_value")
	assert.NoError(t, err)
	duplicateOrder := append(append([]aip132.OrderBy{}, idOrder...), idOrder...)
	compositeOrder, err := cursorCompositeSchema.ParseOrder("id, created_at desc")
	assert.NoError(t, err)
	rows := []cursorRow{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	// One key field is shared, so only the second one separates the two rows.
	tied := []cursorRow{
		{ID: "same-id", CreatedAt: cursorCreatedAt},
		{ID: "same-id", CreatedAt: cursorCreatedAt.Add(-time.Second)},
	}

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
		name   string
		schema *Schema
		args   args
		want   page
		// wantCursorValues is the position the minted token decodes to, checked
		// only when set.
		wantCursorValues []any
		wantSentinel     error
		wantErr          assert.ErrorFunc
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
			name:    "page size wider than the rows",
			args:    args{rows: rows[:1], pageSize: math.MaxInt32, orderBy: idOrder},
			want:    page{items: rows[:1]},
			wantErr: assert.NoError,
		},
		{
			name:         "zero page size is rejected",
			args:         args{rows: rows, orderBy: idOrder},
			wantSentinel: ErrInvalidPageSize,
			wantErr:      assert.Error,
		},
		{
			name:         "negative page size is rejected",
			args:         args{rows: rows, pageSize: -1, orderBy: idOrder},
			wantSentinel: ErrInvalidPageSize,
			wantErr:      assert.Error,
		},
		{
			name:         "repeated ordering field",
			args:         args{rows: []cursorRow{}, pageSize: 1, orderBy: duplicateOrder},
			wantSentinel: ErrInvalidOrder,
			wantErr:      assert.Error,
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
		{
			name:             "composite key carries every key field",
			schema:           cursorCompositeSchema,
			args:             args{rows: tied, pageSize: 1, orderBy: compositeOrder},
			want:             page{items: tied[:1], hasCursor: true},
			wantCursorValues: []any{"same-id", cursorCreatedAt},
			wantErr:          assert.NoError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schema := test.schema
			if schema == nil {
				schema = cursorSchema
			}
			got, gotErr := schema.NewCursorPage(
				test.args.rows,
				test.args.pageSize,
				test.args.orderBy,
				nil,
			)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
				return
			}
			assert.Equal(t, test.want.items, got.Items)
			assert.Equal(t, test.want.hasCursor, got.NextCursor != "")
			if got.NextCursor == "" {
				return
			}
			pos, err := schema.DecodeCursor(got.NextCursor, test.args.orderBy, nil)
			assert.NoError(t, err)
			if test.wantCursorValues == nil {
				assert.Equal[any](t, got.Items[len(got.Items)-1].ID, pos[0].Value)
				return
			}
			assert.Equal(t, len(test.wantCursorValues), len(pos))
			for i, want := range test.wantCursorValues {
				assert.Equal(t, want, pos[i].Value)
			}
		})
	}
}

func FuzzDecodeCursor(f *testing.F) {
	order := []aip132.OrderBy{{FieldPath: aip132.NewFieldPath("id")}}
	f.Add("")
	f.Add("*")
	f.Add(cursorV1Fixture)
	raw, err := base64.RawURLEncoding.DecodeString(cursorV1Fixture)
	if err != nil {
		f.Fatal(err)
	}
	for i := range len(raw) {
		f.Add(base64.RawURLEncoding.EncodeToString(raw[:i]))
	}
	f.Fuzz(func(t *testing.T, token string) {
		pos, err := cursorSchema.DecodeCursor(token, order, nil)
		if err != nil {
			if !errors.Is(err, ErrInvalidCursor) && !errors.Is(err, ErrCursorMismatch) {
				t.Fatalf("unexpected error category: %v", err)
			}
			return
		}
		_, err = cursorSchema.CompileSeek(order, pos)
		assert.NoError(t, err)
	})
}
