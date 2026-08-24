package flop

import (
	"testing"

	"github.com/ysomad/flop/internal/assert"
	"github.com/ysomad/flop/orderby"
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
		wantErr assert.ErrorFunc
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
				NewField("created_at").Ref("u.created_at").Time().Sortable(),
			).MustBuild(),
			args:    args{text: "created_at"},
			want:    "created_at",
			wantErr: assert.NoError,
		},
		{
			name: "empty clause without a unique field",
			schema: NewSchema(
				NewField("created_at").Ref("u.created_at").Time().Sortable(),
			).MustBuild(),
			args:    args{text: ""},
			want:    "",
			wantErr: assert.NoError,
		},
		{
			name:    "multi segment path",
			schema:  NewSchema(NewField("metadata", "tags").Ref("m.tags").String().Sortable()).MustBuild(),
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
			assert.Equal(t, test.want, orderby.String(got))
		})
	}
}

func TestMergeOrder(t *testing.T) {
	t.Parallel()
	createdAt := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at")}
	createdAtDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at"), Descending: true}
	id := orderby.OrderBy{FieldPath: orderby.NewFieldPath("id")}
	name := orderby.OrderBy{FieldPath: orderby.NewFieldPath("name")}
	aDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("a"), Descending: true}
	b := orderby.OrderBy{FieldPath: orderby.NewFieldPath("b")}
	cDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("c"), Descending: true}

	type args struct {
		def   []orderby.OrderBy
		order []orderby.OrderBy
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "both empty", args: args{}, want: ""},
		{
			name: "default only",
			args: args{def: []orderby.OrderBy{createdAtDesc}},
			want: "created_at desc",
		},
		{
			name: "order only",
			args: args{order: []orderby.OrderBy{name}},
			want: "name",
		},
		{
			name: "order comes first",
			args: args{def: []orderby.OrderBy{createdAtDesc}, order: []orderby.OrderBy{name}},
			want: "name, created_at desc",
		},
		{
			// A field the caller ordered by keeps its direction, and is not
			// repeated by the default.
			name: "order wins over the default direction",
			args: args{
				def:   []orderby.OrderBy{createdAtDesc, id},
				order: []orderby.OrderBy{createdAt},
			},
			want: "created_at, id",
		},
		{
			name: "default order is preserved",
			args: args{def: []orderby.OrderBy{aDesc, b, cDesc}},
			want: "a desc, b, c desc",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, orderby.String(MergeOrder(test.args.def, test.args.order)))
		})
	}
}

func TestSchema_TotalOrder(t *testing.T) {
	t.Parallel()
	createdAt := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at")}
	createdAtDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("created_at"), Descending: true}
	idDesc := orderby.OrderBy{FieldPath: orderby.NewFieldPath("id"), Descending: true}
	displayName := orderby.OrderBy{FieldPath: orderby.NewFieldPath("display_name")}
	a := orderby.OrderBy{FieldPath: orderby.NewFieldPath("a")}

	type args struct {
		order []orderby.OrderBy
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
			args:   args{order: []orderby.OrderBy{createdAtDesc}},
			want:   "created_at desc, id",
		},
		{
			// Composing before appending is the point: the tie-breaker has to
			// end up last, behind whatever the default contributed.
			name:   "appends behind a merged default",
			schema: testSchema(t),
			args:   args{order: MergeOrder([]orderby.OrderBy{createdAtDesc}, []orderby.OrderBy{displayName})},
			want:   "display_name, created_at desc, id",
		},
		{
			name:   "order already names the unique field",
			schema: testSchema(t),
			args:   args{order: []orderby.OrderBy{idDesc, createdAt}},
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
			schema: NewSchema(NewField("a").Ref("a").Int().Sortable()).MustBuild(),
			args:   args{order: []orderby.OrderBy{a}},
			want:   "a",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, orderby.String(test.schema.TotalOrder(test.args.order)))
		})
	}
}
