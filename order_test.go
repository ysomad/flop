package flop

import (
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/ysomad/flop/aip132"
)

func TestSchema_ParseOrder(t *testing.T) {
	t.Parallel()
	type args struct {
		text string
	}
	tests := []struct {
		name    string
		schema  *Schema
		args    args
		want    string
		wantErr errorFunc
	}{
		{
			name:    "single field",
			schema:  testSchema(t),
			args:    args{text: "created_at"},
			want:    "created_at, id",
			wantErr: assert.NoError,
		},
		{
			name:    "descending",
			schema:  testSchema(t),
			args:    args{text: "created_at desc"},
			want:    "created_at desc, id",
			wantErr: assert.NoError,
		},
		{
			name:    "several fields",
			schema:  testSchema(t),
			args:    args{text: "created_at desc, display_name"},
			want:    "created_at desc, display_name, id",
			wantErr: assert.NoError,
		},
		{
			// The tie-breaker is appended once, and never ahead of a clause
			// that already names it.
			name:    "clause already names the unique field",
			schema:  testSchema(t),
			args:    args{text: "id desc, created_at"},
			want:    "id desc, created_at",
			wantErr: assert.NoError,
		},
		{
			name:    "empty clause still orders totally",
			schema:  testSchema(t),
			args:    args{text: ""},
			want:    "id",
			wantErr: assert.NoError,
		},
		{
			name: "no unique field to append",
			schema: NewSchema(
				NewField("created_at").Column("u.created_at").Time().Sortable().Build(),
			).MustBuild(),
			args:    args{text: "created_at"},
			want:    "created_at",
			wantErr: assert.NoError,
		},
		{
			name: "empty clause without a unique field",
			schema: NewSchema(
				NewField("created_at").Column("u.created_at").Time().Sortable().Build(),
			).MustBuild(),
			args:    args{text: ""},
			want:    "",
			wantErr: assert.NoError,
		},
		{
			name:    "multi segment path",
			schema:  NewSchema(NewField("metadata", "tags").Column("m.tags").String().Sortable().Build()).MustBuild(),
			args:    args{text: "metadata.tags desc"},
			want:    "metadata.tags desc",
			wantErr: assert.NoError,
		},

		{name: "undeclared field", schema: testSchema(t), args: args{text: "nope"}, wantErr: assert.Error},
		{
			name:    "declared but not sortable",
			schema:  testSchema(t),
			args:    args{text: "active"},
			wantErr: assert.Error,
		},
		{name: "repeated field", schema: testSchema(t), args: args{text: "id, id"}, wantErr: assert.Error},
		{name: "syntax error", schema: testSchema(t), args: args{text: "created_at desc,"}, wantErr: assert.Error},
		{name: "ascending is not a keyword", schema: testSchema(t), args: args{text: "created_at asc"}, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := test.schema.ParseOrder(test.args.text)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, aip132.OrderByString(got))
		})
	}
}

func TestMergeOrder(t *testing.T) {
	t.Parallel()
	createdAt := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at")}
	createdAtDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at"), Descending: true}
	id := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id")}
	name := aip132.OrderBy{FieldPath: aip132.NewFieldPath("name")}
	aDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("a"), Descending: true}
	b := aip132.OrderBy{FieldPath: aip132.NewFieldPath("b")}
	cDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("c"), Descending: true}

	type args struct {
		def   []aip132.OrderBy
		order []aip132.OrderBy
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "both empty", args: args{}, want: ""},
		{
			name: "default only",
			args: args{def: []aip132.OrderBy{createdAtDesc}},
			want: "created_at desc",
		},
		{
			name: "order only",
			args: args{order: []aip132.OrderBy{name}},
			want: "name",
		},
		{
			name: "order comes first",
			args: args{def: []aip132.OrderBy{createdAtDesc}, order: []aip132.OrderBy{name}},
			want: "name, created_at desc",
		},
		{
			// A field the caller ordered by keeps its direction, and is not
			// repeated by the default.
			name: "order wins over the default direction",
			args: args{
				def:   []aip132.OrderBy{createdAtDesc, id},
				order: []aip132.OrderBy{createdAt},
			},
			want: "created_at, id",
		},
		{
			name: "default order is preserved",
			args: args{def: []aip132.OrderBy{aDesc, b, cDesc}},
			want: "a desc, b, c desc",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, aip132.OrderByString(MergeOrder(test.args.def, test.args.order)))
		})
	}
}

func TestSchema_TotalOrder(t *testing.T) {
	t.Parallel()
	createdAt := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at")}
	createdAtDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at"), Descending: true}
	idDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id"), Descending: true}
	displayName := aip132.OrderBy{FieldPath: aip132.NewFieldPath("display_name")}
	a := aip132.OrderBy{FieldPath: aip132.NewFieldPath("a")}

	type args struct {
		order []aip132.OrderBy
	}
	tests := []struct {
		name   string
		schema *Schema
		args   args
		want   string
	}{
		{
			name:   "appends the unique field",
			schema: testSchema(t),
			args:   args{order: []aip132.OrderBy{createdAtDesc}},
			want:   "created_at desc, id",
		},
		{
			// Composing before appending is the point: the tie-breaker has to
			// end up last, behind whatever the default contributed.
			name:   "appends behind a merged default",
			schema: testSchema(t),
			args:   args{order: MergeOrder([]aip132.OrderBy{createdAtDesc}, []aip132.OrderBy{displayName})},
			want:   "display_name, created_at desc, id",
		},
		{
			name:   "order already names the unique field",
			schema: testSchema(t),
			args:   args{order: []aip132.OrderBy{idDesc, createdAt}},
			want:   "id desc, created_at",
		},
		{
			name:   "empty order",
			schema: testSchema(t),
			args:   args{},
			want:   "id",
		},
		{
			name:   "no unique field declared",
			schema: NewSchema(NewField("a").Column("a").Int().Sortable().Build()).MustBuild(),
			args:   args{order: []aip132.OrderBy{a}},
			want:   "a",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, aip132.OrderByString(test.schema.TotalOrder(test.args.order)))
		})
	}
}
