package flopsq_test

import (
	"math"
	"testing"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/alecthomas/assert/v2"

	"github.com/ysomad/flop"
	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
	"github.com/ysomad/flop/flopsq"
)

// errorFunc lets a table row name the assertion its error must satisfy,
// assert.NoError or assert.Error.
type errorFunc = func(testing.TB, error, ...any)

// user is the row the query tests address.
type user struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	Rating    float64
}

var schema = flop.NewSchema(
	flop.NewField("id").Column("u.id").Int().Unique().
		Value(func(u user) any { return u.ID }),
	flop.NewField("display_name").Column("u.name").String().Filterable().Sortable().Implicit().
		Value(func(u user) any { return u.Name }),
	flop.NewField("created_at").Column("u.created_at").Time().Filterable().Sortable().
		Value(func(u user) any { return u.CreatedAt }),
	flop.NewField("latency").Column("u.latency").Duration().Filterable(),
	flop.NewField("active").Column("u.active").Bool().Filterable(),
	flop.NewField("rating").Column("u.rating").Float().Filterable().Sortable().
		Value(func(u user) any { return u.Rating }),
	flop.NewField("age").Column("u.age").Int().Filterable(),
).MustBuild()

var createdAt = time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

func TestWhere(t *testing.T) {
	t.Parallel()
	type args struct {
		filter      string
		unvalidated bool
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs []any
		wantErr  errorFunc
	}{
		{name: "empty filter", args: args{filter: ""}, want: "", wantErr: assert.NoError},
		{
			name:     "string equality",
			args:     args{filter: `display_name = "bob"`},
			want:     "u.name = ?",
			wantArgs: []any{"bob"},
			wantErr:  assert.NoError,
		},
		{
			// squirrel's Like writes no ESCAPE clause, so the pattern flop
			// escaped is emitted with one of its own.
			name:     "has becomes an escaped like",
			args:     args{filter: `display_name:"bob"`},
			want:     `u.name LIKE ? ESCAPE '\'`,
			wantArgs: []any{"%bob%"},
			wantErr:  assert.NoError,
		},
		{
			name:     "int comparison",
			args:     args{filter: "age > 30"},
			want:     "u.age > ?",
			wantArgs: []any{int64(30)},
			wantErr:  assert.NoError,
		},
		{
			name:     "ordered comparators",
			args:     args{filter: "age < 1 AND age <= 2"},
			want:     "(u.age < ? AND u.age <= ?)",
			wantArgs: []any{int64(1), int64(2)},
			wantErr:  assert.NoError,
		},
		{
			name:     "float comparison",
			args:     args{filter: "rating >= 4.5"},
			want:     "u.rating >= ?",
			wantArgs: []any{4.5},
			wantErr:  assert.NoError,
		},
		{
			name:     "bool binds rather than inlining",
			args:     args{filter: "active = true"},
			want:     "u.active = ?",
			wantArgs: []any{true},
			wantErr:  assert.NoError,
		},
		{
			name:     "time binds as a time",
			args:     args{filter: `created_at > "2026-08-15T09:00:00Z"`},
			want:     "u.created_at > ?",
			wantArgs: []any{createdAt},
			wantErr:  assert.NoError,
		},
		{
			name:     "duration binds as a duration",
			args:     args{filter: "latency < 250ms"},
			want:     "u.latency < ?",
			wantArgs: []any{250 * time.Millisecond},
			wantErr:  assert.NoError,
		},
		{
			name:    "null is a predicate, not a bound value",
			args:    args{filter: "created_at = null"},
			want:    "u.created_at IS NULL",
			wantErr: assert.NoError,
		},
		{
			name:    "not null",
			args:    args{filter: "created_at != null"},
			want:    "u.created_at IS NOT NULL",
			wantErr: assert.NoError,
		},
		{
			name:     "conjunction",
			args:     args{filter: "active = true AND age = 30"},
			want:     "(u.active = ? AND u.age = ?)",
			wantArgs: []any{true, int64(30)},
			wantErr:  assert.NoError,
		},
		{
			name:     "disjunction",
			args:     args{filter: "age = 30 OR age = 40"},
			want:     "(u.age = ? OR u.age = ?)",
			wantArgs: []any{int64(30), int64(40)},
			wantErr:  assert.NoError,
		},
		{
			name:     "negation",
			args:     args{filter: "-active = true"},
			want:     "NOT (u.active = ?)",
			wantArgs: []any{true},
			wantErr:  assert.NoError,
		},
		{
			name:     "aip precedence",
			args:     args{filter: "age = 1 OR age = 2 AND active = true"},
			want:     "((u.age = ? OR u.age = ?) AND u.active = ?)",
			wantArgs: []any{int64(1), int64(2), true},
			wantErr:  assert.NoError,
		},
		{
			name:     "implicit search",
			args:     args{filter: "bob"},
			want:     `u.name LIKE ? ESCAPE '\'`,
			wantArgs: []any{"%bob%"},
			wantErr:  assert.NoError,
		},

		{
			name:    "undeclared field",
			args:    args{filter: "nope = 1", unvalidated: true},
			wantErr: assert.Error,
		},
		{name: "syntax error", args: args{filter: "age ="}, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var filter *aip160.Filter
			var err error
			if test.args.unvalidated {
				filter, err = aip160.ParseFilter(test.args.filter)
			} else {
				filter, err = schema.ParseFilter(test.args.filter)
			}
			if err != nil {
				test.wantErr(t, err)
				return
			}
			where, gotErr := flopsq.Where(schema, filter)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			if where == nil {
				assert.Equal(t, "", test.want)
				return
			}
			got, gotArgs, err := where.ToSql()
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}
}

func TestOrderBy(t *testing.T) {
	t.Parallel()
	createdAtAsc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at")}
	createdAtDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at"), Descending: true}
	idAsc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id")}
	nopeAsc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("nope")}
	activeAsc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("active")}

	type args struct {
		order []aip132.OrderBy
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr errorFunc
	}{
		{name: "empty", args: args{}, wantErr: assert.NoError},
		{
			name:    "ascending",
			args:    args{order: []aip132.OrderBy{createdAtAsc}},
			want:    []string{"u.created_at"},
			wantErr: assert.NoError,
		},
		{
			name:    "descending",
			args:    args{order: []aip132.OrderBy{createdAtDesc}},
			want:    []string{"u.created_at DESC"},
			wantErr: assert.NoError,
		},
		{
			name:    "several fields keep their order",
			args:    args{order: []aip132.OrderBy{createdAtDesc, idAsc}},
			want:    []string{"u.created_at DESC", "u.id"},
			wantErr: assert.NoError,
		},
		{name: "undeclared field", args: args{order: []aip132.OrderBy{nopeAsc}}, wantErr: assert.Error},
		{
			name:    "declared but not sortable",
			args:    args{order: []aip132.OrderBy{activeAsc}},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := flopsq.OrderBy(schema, test.args.order)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, got)
		})
	}
}

func TestSeek(t *testing.T) {
	t.Parallel()
	createdAtPath := aip132.NewFieldPath("created_at")
	idPath := aip132.NewFieldPath("id")

	createdAtAsc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at")}
	createdAtDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at"), Descending: true}
	idAsc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id")}
	idDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id"), Descending: true}

	type args struct {
		order []aip132.OrderBy
		pos   flop.CursorPosition
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs []any
		wantErr  errorFunc
	}{
		{
			name: "single unique field",
			args: args{
				order: []aip132.OrderBy{idAsc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: int64(7)}},
			},
			want:     "u.id > ?",
			wantArgs: []any{int64(7)},
			wantErr:  assert.NoError,
		},
		{
			name: "descending flips the comparison",
			args: args{
				order: []aip132.OrderBy{idDesc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: int64(7)}},
			},
			want:     "u.id < ?",
			wantArgs: []any{int64(7)},
			wantErr:  assert.NoError,
		},
		{
			// Mixed directions are why the comparison is written out rather
			// than as a row value.
			name: "mixed directions",
			args: args{
				order: []aip132.OrderBy{createdAtDesc, idAsc},
				pos: flop.CursorPosition{
					{FieldPath: createdAtPath, Value: createdAt},
					{FieldPath: idPath, Value: int64(7)},
				},
			},
			want:     "(u.created_at < ? OR (u.created_at = ? AND u.id > ?))",
			wantArgs: []any{createdAt, createdAt, int64(7)},
			wantErr:  assert.NoError,
		},
		{
			name: "no position is a first page",
			args: args{order: []aip132.OrderBy{idAsc}},
			want: "", wantErr: assert.NoError,
		},

		{name: "no order", args: args{}, wantErr: assert.Error},
		{
			name: "order without a unique field",
			args: args{
				order: []aip132.OrderBy{createdAtAsc},
				pos:   flop.CursorPosition{{FieldPath: createdAtPath, Value: createdAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "position does not cover the order",
			args: args{
				order: []aip132.OrderBy{createdAtDesc, idAsc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: int64(7)}},
			},
			wantErr: assert.Error,
		},
		{
			name: "position names a field the order does not",
			args: args{
				order: []aip132.OrderBy{idAsc},
				pos:   flop.CursorPosition{{FieldPath: createdAtPath, Value: createdAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "null position value",
			args: args{
				order: []aip132.OrderBy{idAsc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: nil}},
			},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			seek, gotErr := flopsq.Seek(schema, test.args.order, test.args.pos)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			if seek == nil {
				assert.Equal(t, "", test.want)
				return
			}
			got, gotArgs, err := seek.ToSql()
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}
}

func TestQuery(t *testing.T) {
	t.Parallel()
	type args struct {
		filter string
		order  string
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs []any
	}{
		{
			name: "neither",
			want: "SELECT * FROM users u",
		},
		{
			name:     "filter only",
			args:     args{filter: "active = true"},
			want:     "SELECT * FROM users u WHERE u.active = $1",
			wantArgs: []any{true},
		},
		{
			// ParseOrder appends the unique field, so the order is total.
			name: "order only",
			args: args{order: "created_at desc"},
			want: "SELECT * FROM users u ORDER BY u.created_at DESC, u.id",
		},
		{
			name:     "filter and order",
			args:     args{filter: `display_name:"bob"`, order: "created_at desc"},
			want:     `SELECT * FROM users u WHERE u.name LIKE $1 ESCAPE '\' ORDER BY u.created_at DESC, u.id`,
			wantArgs: []any{"%bob%"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			filter, err := schema.ParseFilter(test.args.filter)
			assert.NoError(t, err)
			order, err := schema.ParseOrder(test.args.order)
			assert.NoError(t, err)
			if test.args.order == "" {
				order = nil
			}

			b, err := flopsq.Query(
				sq.Select("*").From("users u").PlaceholderFormat(sq.Dollar),
				schema, order, filter,
			)
			assert.NoError(t, err)
			got, gotArgs, err := b.ToSql()
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}

	t.Run("invalid filter", func(t *testing.T) {
		t.Parallel()
		filter, err := aip160.ParseFilter("nope = 1")
		assert.NoError(t, err)
		_, err = flopsq.Query(sq.Select("*").From("users u"), schema, nil, filter)
		assert.Error(t, err)
	})

	t.Run("invalid order", func(t *testing.T) {
		t.Parallel()
		order, err := aip132.ParseOrderBy("nope")
		assert.NoError(t, err)
		_, err = flopsq.Query(sq.Select("*").From("users u"), schema, order, nil)
		assert.Error(t, err)
	})
}

func TestOffsetQuery(t *testing.T) {
	t.Parallel()
	filter, err := schema.ParseFilter("active = true")
	assert.NoError(t, err)
	order, err := schema.ParseOrder("created_at desc")
	assert.NoError(t, err)
	invalidFilter, err := aip160.ParseFilter("nope = 1")
	assert.NoError(t, err)
	invalidOrder, err := aip132.ParseOrderBy("nope")
	assert.NoError(t, err)

	type args struct {
		order    []aip132.OrderBy
		filter   *aip160.Filter
		page     int32
		pageSize int32
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs []any
		wantErr  errorFunc
	}{
		{
			name: "page",
			args: args{order: order, filter: filter, page: 3, pageSize: 20},
			want: "SELECT * FROM users u WHERE u.active = $1 " +
				"ORDER BY u.created_at DESC, u.id LIMIT 20 OFFSET 40",
			wantArgs: []any{true},
			wantErr:  assert.NoError,
		},
		{
			name:    "negative page",
			args:    args{page: -1, pageSize: 20},
			wantErr: assert.Error,
		},
		{
			name:    "non-positive page size",
			args:    args{page: 1},
			wantErr: assert.Error,
		},
		{
			name:    "invalid filter",
			args:    args{filter: invalidFilter, page: 1, pageSize: 20},
			wantErr: assert.Error,
		},
		{
			name:    "invalid order",
			args:    args{order: invalidOrder, page: 1, pageSize: 20},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, gotErr := flopsq.OffsetQuery(
				sq.Select("*").From("users u").PlaceholderFormat(sq.Dollar),
				schema, test.args.order, test.args.filter, test.args.page, test.args.pageSize,
			)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			got, gotArgs, err := b.ToSql()
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}
}

func TestCursorQuery(t *testing.T) {
	t.Parallel()
	filter, err := schema.ParseFilter("active = true")
	assert.NoError(t, err)
	order, err := schema.ParseOrder("created_at desc")
	assert.NoError(t, err)
	token, err := schema.EncodeCursor(user{ID: 7, CreatedAt: createdAt}, order, filter)
	assert.NoError(t, err)
	after, err := schema.DecodeCursor(token, order, filter)
	assert.NoError(t, err)
	invalidFilter, err := aip160.ParseFilter("nope = 1")
	assert.NoError(t, err)
	invalidOrder, err := aip132.ParseOrderBy("nope")
	assert.NoError(t, err)
	nonUniqueOrder, err := aip132.ParseOrderBy("created_at")
	assert.NoError(t, err)

	type args struct {
		order    []aip132.OrderBy
		filter   *aip160.Filter
		after    flop.CursorPosition
		pageSize int32
		skip     int32
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs []any
		wantErr  errorFunc
	}{
		{
			name: "first page",
			args: args{order: order, filter: filter, pageSize: 2},
			want: "SELECT * FROM users u WHERE u.active = $1 " +
				"ORDER BY u.created_at DESC, u.id LIMIT 3",
			wantArgs: []any{true},
			wantErr:  assert.NoError,
		},
		{
			name: "position and skip",
			args: args{order: order, filter: filter, after: after, pageSize: 2, skip: 5},
			want: "SELECT * FROM users u WHERE u.active = $1 AND " +
				"(u.created_at < $2 OR (u.created_at = $3 AND u.id > $4)) " +
				"ORDER BY u.created_at DESC, u.id LIMIT 3 OFFSET 5",
			wantArgs: []any{true, createdAt, createdAt, int64(7)},
			wantErr:  assert.NoError,
		},
		{
			name:    "maximum page size saturates the surplus row",
			args:    args{order: order, pageSize: math.MaxInt32},
			want:    "SELECT * FROM users u ORDER BY u.created_at DESC, u.id LIMIT 2147483647",
			wantErr: assert.NoError,
		},
		{
			name:    "non-positive page size",
			args:    args{order: order},
			wantErr: assert.Error,
		},
		{
			name:    "negative skip",
			args:    args{order: order, pageSize: 2, skip: -1},
			wantErr: assert.Error,
		},
		{
			name:    "invalid filter",
			args:    args{order: order, filter: invalidFilter, pageSize: 2},
			wantErr: assert.Error,
		},
		{
			name:    "invalid order",
			args:    args{order: invalidOrder, pageSize: 2},
			wantErr: assert.Error,
		},
		{
			name:    "order without a unique field",
			args:    args{order: nonUniqueOrder, pageSize: 2},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, gotErr := flopsq.CursorQuery(
				sq.Select("*").From("users u").PlaceholderFormat(sq.Dollar),
				schema, test.args.order, test.args.filter, test.args.after,
				test.args.pageSize, test.args.skip,
			)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			got, gotArgs, err := b.ToSql()
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}
}
