package flop

import (
	"testing"

	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/internal/assert"
)

func TestSchema_ParseOrder(t *testing.T) {
	t.Parallel()
	createdAt := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at")}
	createdAtDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at"), Descending: true}
	displayName := aip132.OrderBy{FieldPath: aip132.NewFieldPath("display_name")}
	id := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id")}
	idDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id"), Descending: true}
	metadataTagsDesc := aip132.OrderBy{
		FieldPath:  aip132.NewFieldPath("metadata", "tags"),
		Descending: true,
	}

	type args struct {
		text string
	}
	tests := []struct {
		name    string
		schema  *Schema
		args    args
		want    []aip132.OrderBy
		wantErr assert.ErrorFunc
	}{
		{
			name:    "single field",
			schema:  testSchema(t),
			args:    args{text: "created_at"},
			want:    []aip132.OrderBy{createdAt, id},
			wantErr: assert.NoError,
		},
		{
			name:    "descending",
			schema:  testSchema(t),
			args:    args{text: "created_at desc"},
			want:    []aip132.OrderBy{createdAtDesc, id},
			wantErr: assert.NoError,
		},
		{
			name:    "several fields",
			schema:  testSchema(t),
			args:    args{text: "created_at desc, display_name"},
			want:    []aip132.OrderBy{createdAtDesc, displayName, id},
			wantErr: assert.NoError,
		},
		{
			// The tie-breaker is appended once, and never ahead of a clause
			// that already names it.
			name:    "clause already names the unique field",
			schema:  testSchema(t),
			args:    args{text: "id desc, created_at"},
			want:    []aip132.OrderBy{idDesc, createdAt},
			wantErr: assert.NoError,
		},
		{
			name:    "empty clause still orders totally",
			schema:  testSchema(t),
			args:    args{text: ""},
			want:    []aip132.OrderBy{id},
			wantErr: assert.NoError,
		},
		{
			name: "no unique field to append",
			schema: NewSchema(
				NewField("created_at").Ref("u.created_at").Time().Sortable(),
			).MustBuild(),
			args:    args{text: "created_at"},
			want:    []aip132.OrderBy{createdAt},
			wantErr: assert.NoError,
		},
		{
			name: "empty clause without a unique field",
			schema: NewSchema(
				NewField("created_at").Ref("u.created_at").Time().Sortable(),
			).MustBuild(),
			args:    args{text: ""},
			want:    nil,
			wantErr: assert.NoError,
		},
		{
			name:    "multi segment path",
			schema:  NewSchema(NewField("metadata", "tags").Ref("m.tags").String().Sortable()).MustBuild(),
			args:    args{text: "metadata.tags desc"},
			want:    []aip132.OrderBy{metadataTagsDesc},
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
			assert.Equal(t, test.want, got)
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
		want []aip132.OrderBy
	}{
		{name: "both empty", args: args{}, want: []aip132.OrderBy{}},
		{
			name: "default only",
			args: args{def: []aip132.OrderBy{createdAtDesc}},
			want: []aip132.OrderBy{createdAtDesc},
		},
		{
			name: "order only",
			args: args{order: []aip132.OrderBy{name}},
			want: []aip132.OrderBy{name},
		},
		{
			name: "order comes first",
			args: args{def: []aip132.OrderBy{createdAtDesc}, order: []aip132.OrderBy{name}},
			want: []aip132.OrderBy{name, createdAtDesc},
		},
		{
			// A field the caller ordered by keeps its direction, and is not
			// repeated by the default.
			name: "order wins over the default direction",
			args: args{
				def:   []aip132.OrderBy{createdAtDesc, id},
				order: []aip132.OrderBy{createdAt},
			},
			want: []aip132.OrderBy{createdAt, id},
		},
		{
			name: "default order is preserved",
			args: args{def: []aip132.OrderBy{aDesc, b, cDesc}},
			want: []aip132.OrderBy{aDesc, b, cDesc},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, MergeOrder(test.args.def, test.args.order))
		})
	}
}

func TestSchema_TotalOrder(t *testing.T) {
	t.Parallel()
	createdAt := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at")}
	createdAtDesc := aip132.OrderBy{FieldPath: aip132.NewFieldPath("created_at"), Descending: true}
	id := aip132.OrderBy{FieldPath: aip132.NewFieldPath("id")}
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
		want   []aip132.OrderBy
	}{
		{
			name:   "appends the unique field",
			schema: testSchema(t),
			args:   args{order: []aip132.OrderBy{createdAtDesc}},
			want:   []aip132.OrderBy{createdAtDesc, id},
		},
		{
			// Composing before appending is the point: the tie-breaker has to
			// end up last, behind whatever the default contributed.
			name:   "appends behind a merged default",
			schema: testSchema(t),
			args:   args{order: MergeOrder([]aip132.OrderBy{createdAtDesc}, []aip132.OrderBy{displayName})},
			want:   []aip132.OrderBy{displayName, createdAtDesc, id},
		},
		{
			name:   "order already names the unique field",
			schema: testSchema(t),
			args:   args{order: []aip132.OrderBy{idDesc, createdAt}},
			want:   []aip132.OrderBy{idDesc, createdAt},
		},
		{
			name:   "empty order",
			schema: testSchema(t),
			args:   args{},
			want:   []aip132.OrderBy{id},
		},
		{
			name:   "no unique field declared",
			schema: NewSchema(NewField("a").Ref("a").Int().Sortable()).MustBuild(),
			args:   args{order: []aip132.OrderBy{a}},
			want:   []aip132.OrderBy{a},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, test.schema.TotalOrder(test.args.order))
		})
	}
}
