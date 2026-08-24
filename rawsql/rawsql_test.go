package rawsql_test

import (
	"testing"
	"time"

	"github.com/ysomad/flop"
	"github.com/ysomad/flop/internal/assert"
	"github.com/ysomad/flop/orderby"
	"github.com/ysomad/flop/rawsql"
)

var schema = flop.NewSchema(
	flop.NewField("id").Ref("u.id").Int().Unique(),
	flop.NewField("display_name").Ref("u.name").String().Filterable().Sortable().Implicit(),
	flop.NewField("created_at").Ref("u.created_at").Time().Filterable().Sortable(),
	flop.NewField("latency").Ref("u.latency").Duration().Filterable().Sortable(),
	flop.NewField("active").Ref("u.active").Bool().Filterable(),
	flop.NewField("rating").Ref("u.rating").Float().Filterable().Sortable(),
	flop.NewField("age").Ref("u.age").Int().Filterable(),
	flop.NewField("metadata", "tags").Ref("u.tags").String().Filterable(),
).MustBuild()

func TestWhere(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

	type args struct {
		filter string
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs map[string]any
		wantErr  assert.ErrorFunc
	}{
		{
			name:     "empty filter",
			args:     args{filter: ""},
			want:     "",
			wantArgs: map[string]any{},
			wantErr:  assert.NoError,
		},
		{
			name:     "string equality",
			args:     args{filter: `display_name = "bob"`},
			want:     "(u.name = @display_name_1)",
			wantArgs: map[string]any{"display_name_1": "bob"},
			wantErr:  assert.NoError,
		},
		{
			name:     "has becomes an escaped like",
			args:     args{filter: `display_name:"bob"`},
			want:     `(u.name LIKE @display_name_1 ESCAPE '\')`,
			wantArgs: map[string]any{"display_name_1": "%bob%"},
			wantErr:  assert.NoError,
		},
		{
			name:     "int comparison",
			args:     args{filter: "age > 30"},
			want:     "(u.age > @age_1)",
			wantArgs: map[string]any{"age_1": int64(30)},
			wantErr:  assert.NoError,
		},
		{
			name:     "float comparison",
			args:     args{filter: "rating >= 4.5"},
			want:     "(u.rating >= @rating_1)",
			wantArgs: map[string]any{"rating_1": 4.5},
			wantErr:  assert.NoError,
		},
		{
			name:     "bool binds rather than inlining",
			args:     args{filter: "active = true"},
			want:     "(u.active = @active_1)",
			wantArgs: map[string]any{"active_1": true},
			wantErr:  assert.NoError,
		},
		{
			name:     "time binds as a time",
			args:     args{filter: `created_at > "2026-08-15T09:00:00Z"`},
			want:     "(u.created_at > @created_at_1)",
			wantArgs: map[string]any{"created_at_1": createdAt},
			wantErr:  assert.NoError,
		},
		{
			name:     "duration binds as a duration",
			args:     args{filter: "latency < 250ms"},
			want:     "(u.latency < @latency_1)",
			wantArgs: map[string]any{"latency_1": 250 * time.Millisecond},
			wantErr:  assert.NoError,
		},
		{
			name:     "null is a predicate, not a bound value",
			args:     args{filter: "created_at = null"},
			want:     "(u.created_at IS NULL)",
			wantArgs: map[string]any{},
			wantErr:  assert.NoError,
		},
		{
			name:     "not null",
			args:     args{filter: "created_at != null"},
			want:     "(u.created_at IS NOT NULL)",
			wantArgs: map[string]any{},
			wantErr:  assert.NoError,
		},
		{
			name:     "conjunction binds left to right",
			args:     args{filter: "active = true AND age = 30"},
			want:     "((u.active = @active_1) AND (u.age = @age_2))",
			wantArgs: map[string]any{"active_1": true, "age_2": int64(30)},
			wantErr:  assert.NoError,
		},
		{
			name:     "disjunction",
			args:     args{filter: "age = 30 OR age = 40"},
			want:     "((u.age = @age_1) OR (u.age = @age_2))",
			wantArgs: map[string]any{"age_1": int64(30), "age_2": int64(40)},
			wantErr:  assert.NoError,
		},
		{
			name:     "negation",
			args:     args{filter: "-active = true"},
			want:     "(NOT (u.active = @active_1))",
			wantArgs: map[string]any{"active_1": true},
			wantErr:  assert.NoError,
		},
		{
			name:     "aip precedence",
			args:     args{filter: "age = 1 OR age = 2 AND active = true"},
			want:     "(((u.age = @age_1) OR (u.age = @age_2)) AND (u.active = @active_3))",
			wantArgs: map[string]any{"age_1": int64(1), "age_2": int64(2), "active_3": true},
			wantErr:  assert.NoError,
		},
		{
			name:     "multi segment path names the argument with an underscore",
			args:     args{filter: `metadata.tags = "blue"`},
			want:     "(u.tags = @metadata_tags_1)",
			wantArgs: map[string]any{"metadata_tags_1": "blue"},
			wantErr:  assert.NoError,
		},
		{
			name:     "implicit search",
			args:     args{filter: "bob"},
			want:     `(u.name LIKE @display_name_1 ESCAPE '\')`,
			wantArgs: map[string]any{"display_name_1": "%bob%"},
			wantErr:  assert.NoError,
		},

		{
			// A wildcard turns an equality into a pattern match, which is what
			// a client filtering on id = "*pay_00*" asks for.
			name:     "equality with wildcards",
			args:     args{filter: `display_name = "*bob*"`},
			want:     `(u.name LIKE @display_name_1 ESCAPE '\')`,
			wantArgs: map[string]any{"display_name_1": "%bob%"},
			wantErr:  assert.NoError,
		},
		{
			name:     "inequality with wildcards",
			args:     args{filter: `display_name != "bob*"`},
			want:     `(NOT (u.name LIKE @display_name_1 ESCAPE '\'))`,
			wantArgs: map[string]any{"display_name_1": "bob%"},
			wantErr:  assert.NoError,
		},
		{name: "undeclared field", args: args{filter: "nope = 1"}, wantErr: assert.Error},
		{name: "syntax error", args: args{filter: "age ="}, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			filter, err := schema.ParseFilter(test.args.filter)
			if err != nil {
				test.wantErr(t, err)
				return
			}
			got, gotArgs, gotErr := rawsql.Where(schema, filter)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}
}

func TestOrderBy(t *testing.T) {
	t.Parallel()
	createdAtAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at")}
	createdAtDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at"), Descending: true}
	idAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("id")}
	nopeAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("nope")}
	activeAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("active")}

	type args struct {
		order []orderby.OrderBy
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr assert.ErrorFunc
	}{
		{name: "empty", args: args{}, want: "", wantErr: assert.NoError},
		{
			name:    "ascending",
			args:    args{order: []orderby.OrderBy{createdAtAsc}},
			want:    "u.created_at",
			wantErr: assert.NoError,
		},
		{
			name:    "descending",
			args:    args{order: []orderby.OrderBy{createdAtDesc}},
			want:    "u.created_at DESC",
			wantErr: assert.NoError,
		},
		{
			name:    "several fields keep their order",
			args:    args{order: []orderby.OrderBy{createdAtDesc, idAsc}},
			want:    "u.created_at DESC, u.id",
			wantErr: assert.NoError,
		},
		{name: "undeclared field", args: args{order: []orderby.OrderBy{nopeAsc}}, wantErr: assert.Error},
		{
			name:    "declared but not sortable",
			args:    args{order: []orderby.OrderBy{activeAsc}},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := rawsql.OrderBy(schema, test.args.order)
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
	createdAtPath := orderby.NewFieldPath("created_at")
	idPath := orderby.NewFieldPath("id")
	nopePath := orderby.NewFieldPath("nope")

	createdAtAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at")}
	createdAtDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at"), Descending: true}
	idAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("id")}
	idDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("id"), Descending: true}
	nopeAsc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("nope")}

	createdAt := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

	type args struct {
		order []orderby.OrderBy
		pos   flop.CursorPosition
	}
	tests := []struct {
		name     string
		args     args
		want     string
		wantArgs map[string]any
		wantErr  assert.ErrorFunc
	}{
		{
			name: "single unique field",
			args: args{
				order: []orderby.OrderBy{idAsc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: int64(7)}},
			},
			want:     "(u.id > @id_1)",
			wantArgs: map[string]any{"id_1": int64(7)},
			wantErr:  assert.NoError,
		},
		{
			name: "descending flips the comparison",
			args: args{
				order: []orderby.OrderBy{idDesc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: int64(7)}},
			},
			want:     "(u.id < @id_1)",
			wantArgs: map[string]any{"id_1": int64(7)},
			wantErr:  assert.NoError,
		},
		{
			// Mixed directions are why the comparison is written out rather
			// than as a row value.
			name: "mixed directions",
			args: args{
				order: []orderby.OrderBy{createdAtDesc, idAsc},
				pos: flop.CursorPosition{
					{FieldPath: createdAtPath, Value: createdAt},
					{FieldPath: idPath, Value: int64(7)},
				},
			},
			want: "((u.created_at < @created_at_1)" +
				" OR ((u.created_at = @created_at_2) AND (u.id > @id_3)))",
			wantArgs: map[string]any{
				"created_at_1": createdAt,
				"created_at_2": createdAt,
				"id_3":         int64(7),
			},
			wantErr: assert.NoError,
		},
		{
			name:     "no position is a first page",
			args:     args{order: []orderby.OrderBy{idAsc}},
			want:     "",
			wantArgs: map[string]any{},
			wantErr:  assert.NoError,
		},

		{name: "no order", args: args{}, wantErr: assert.Error},
		{
			name: "order without a unique field",
			args: args{
				order: []orderby.OrderBy{createdAtAsc},
				pos:   flop.CursorPosition{{FieldPath: createdAtPath, Value: createdAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "position does not cover the order",
			args: args{
				order: []orderby.OrderBy{createdAtDesc, idAsc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: int64(7)}},
			},
			wantErr: assert.Error,
		},
		{
			name: "position names a field the order does not",
			args: args{
				order: []orderby.OrderBy{idAsc},
				pos:   flop.CursorPosition{{FieldPath: createdAtPath, Value: createdAt}},
			},
			wantErr: assert.Error,
		},
		{
			name: "null position value",
			args: args{
				order: []orderby.OrderBy{idAsc},
				pos:   flop.CursorPosition{{FieldPath: idPath, Value: nil}},
			},
			wantErr: assert.Error,
		},
		{
			name: "undeclared ordering field",
			args: args{
				order: []orderby.OrderBy{nopeAsc},
				pos:   flop.CursorPosition{{FieldPath: nopePath, Value: int64(1)}},
			},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotArgs, gotErr := rawsql.Seek(schema, test.args.order, test.args.pos)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.wantArgs, gotArgs)
		})
	}
}

// TestBuilder covers what the one-shot functions cannot: a query taking more
// than one fragment must keep its argument names unique across all of them.
func TestBuilder(t *testing.T) {
	t.Parallel()
	createdAtPath := orderby.NewFieldPath("created_at")
	idPath := orderby.NewFieldPath("id")

	filter, err := schema.ParseFilter("active = true")
	assert.NoError(t, err)
	order, err := schema.ParseOrder("created_at desc")
	assert.NoError(t, err)
	createdAt := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	pos := flop.CursorPosition{
		{FieldPath: createdAtPath, Value: createdAt},
		{FieldPath: idPath, Value: int64(7)},
	}

	b := rawsql.NewBuilder()
	where, err := b.Where(schema, filter)
	assert.NoError(t, err)
	seek, err := b.Seek(schema, order, pos)
	assert.NoError(t, err)

	assert.Equal(t, "(u.active = @active_1)", where)
	assert.Equal(
		t,
		"((u.created_at < @created_at_2) OR ((u.created_at = @created_at_3) AND (u.id > @id_4)))",
		seek,
	)
	assert.Equal(t, map[string]any{
		"active_1":     true,
		"created_at_2": createdAt,
		"created_at_3": createdAt,
		"id_4":         int64(7),
	}, b.Args())
}

func TestWhereExpr(t *testing.T) {
	t.Parallel()
	filter, err := schema.ParseFilter("age = 30")
	assert.NoError(t, err)
	expr, err := schema.Compile(filter)
	assert.NoError(t, err)

	got, gotArgs, gotErr := rawsql.WhereExpr(expr)
	assert.NoError(t, gotErr)
	assert.Equal(t, "(u.age = @age_1)", got)
	assert.Equal(t, map[string]any{"age_1": int64(30)}, gotArgs)

	got, gotArgs, gotErr = rawsql.WhereExpr(nil)
	assert.NoError(t, gotErr)
	assert.Equal(t, "", got)
	assert.Equal(t, map[string]any{}, gotArgs)
}
