package flop

import (
	"testing"

	"github.com/ysomad/flop/internal/assert"
)

func TestSchema_ParseOrder(t *testing.T) {
	t.Parallel()
	// Parsing preserves the requested terms. TestSchema_TotalOrder verifies that
	// pagination adds every missing unique-key field.
	createdAt := OrderBy{FieldPath: NewFieldPath("created_at")}
	createdAtDesc := OrderBy{FieldPath: NewFieldPath("created_at"), Descending: true}
	displayName := OrderBy{FieldPath: NewFieldPath("display_name")}
	idDesc := OrderBy{FieldPath: NewFieldPath("id"), Descending: true}
	metadataTagsDesc := OrderBy{
		FieldPath:  NewFieldPath("metadata", "tags"),
		Descending: true,
	}

	type args struct {
		text string
	}
	tests := []struct {
		name    string
		schema  *Schema
		args    args
		want    []OrderBy
		wantErr assert.ErrorFunc
	}{
		{
			name:    "single field",
			schema:  testSchema(t),
			args:    args{text: "created_at"},
			want:    []OrderBy{createdAt},
			wantErr: assert.NoError,
		},
		{
			name:    "descending",
			schema:  testSchema(t),
			args:    args{text: "created_at desc"},
			want:    []OrderBy{createdAtDesc},
			wantErr: assert.NoError,
		},
		{
			name:    "several fields",
			schema:  testSchema(t),
			args:    args{text: "created_at desc, display_name"},
			want:    []OrderBy{createdAtDesc, displayName},
			wantErr: assert.NoError,
		},
		{
			// The tie-breaker is appended once, and never ahead of a clause
			// that already names it.
			name:    "clause already names the unique field",
			schema:  testSchema(t),
			args:    args{text: "id desc, created_at"},
			want:    []OrderBy{idDesc, createdAt},
			wantErr: assert.NoError,
		},
		{
			name:    "empty clause stays empty",
			schema:  testSchema(t),
			args:    args{text: ""},
			want:    nil,
			wantErr: assert.NoError,
		},
		{
			name: "no unique field to append",
			schema: NewSchema(
				NewField("created_at").Ref("u.created_at").Time().Sortable(),
			).MustBuild(),
			args:    args{text: "created_at"},
			want:    []OrderBy{createdAt},
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
			want:    []OrderBy{metadataTagsDesc},
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
		{
			name:    "ascending is not a keyword",
			schema:  testSchema(t),
			args:    args{text: "created_at asc"},
			wantErr: assert.Error,
		},
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
	createdAt := OrderBy{FieldPath: NewFieldPath("created_at")}
	createdAtDesc := OrderBy{FieldPath: NewFieldPath("created_at"), Descending: true}
	id := OrderBy{FieldPath: NewFieldPath("id")}
	name := OrderBy{FieldPath: NewFieldPath("name")}
	a := OrderBy{FieldPath: NewFieldPath("a")}
	aDesc := OrderBy{FieldPath: NewFieldPath("a"), Descending: true}
	b := OrderBy{FieldPath: NewFieldPath("b")}
	cDesc := OrderBy{FieldPath: NewFieldPath("c"), Descending: true}

	type args struct {
		def   []OrderBy
		order []OrderBy
	}
	tests := []struct {
		name string
		args args
		want []OrderBy
	}{
		{name: "both empty", args: args{}, want: []OrderBy{}},
		{
			name: "default only",
			args: args{def: []OrderBy{createdAtDesc}},
			want: []OrderBy{createdAtDesc},
		},
		{
			name: "order only",
			args: args{order: []OrderBy{name}},
			want: []OrderBy{name},
		},
		{
			name: "order comes first",
			args: args{def: []OrderBy{createdAtDesc}, order: []OrderBy{name}},
			want: []OrderBy{name, createdAtDesc},
		},
		{
			// A field the caller ordered by keeps its direction, and is not
			// repeated by the default.
			name: "order wins over the default direction",
			args: args{
				def:   []OrderBy{createdAtDesc, id},
				order: []OrderBy{createdAt},
			},
			want: []OrderBy{createdAt, id},
		},
		{
			name: "default order is preserved",
			args: args{def: []OrderBy{aDesc, b, cDesc}},
			want: []OrderBy{aDesc, b, cDesc},
		},
		{
			name: "repeats within an input collapse",
			args: args{def: []OrderBy{b, b, a}, order: []OrderBy{aDesc, a}},
			want: []OrderBy{aDesc, b},
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
	createdAt := OrderBy{FieldPath: NewFieldPath("created_at")}
	createdAtDesc := OrderBy{FieldPath: NewFieldPath("created_at"), Descending: true}
	id := OrderBy{FieldPath: NewFieldPath("id")}
	idDesc := OrderBy{FieldPath: NewFieldPath("id"), Descending: true}
	displayName := OrderBy{FieldPath: NewFieldPath("display_name")}
	amount := OrderBy{FieldPath: NewFieldPath("amount")}
	amountDesc := OrderBy{FieldPath: NewFieldPath("amount"), Descending: true}
	a := OrderBy{FieldPath: NewFieldPath("a")}

	// A composite key has to end up complete, wherever its fields were asked for.
	composite := NewSchema(
		NewField("id").String().Sortable(),
		NewField("created_at").Time().Sortable(),
		NewField("amount").Int().Sortable(),
	).CompositeKey("id", "created_at").MustBuild()

	type args struct {
		order []OrderBy
	}
	tests := []struct {
		name   string
		schema *Schema
		args   args
		want   []OrderBy
	}{
		{
			name:   "appends the unique field",
			schema: testSchema(t),
			args:   args{order: []OrderBy{createdAtDesc}},
			want:   []OrderBy{createdAtDesc, id},
		},
		{
			// Composing before appending is the point: the tie-breaker has to
			// end up last, behind whatever the default contributed.
			name:   "appends behind a merged default",
			schema: testSchema(t),
			args:   args{order: MergeOrder([]OrderBy{createdAtDesc}, []OrderBy{displayName})},
			want:   []OrderBy{displayName, createdAtDesc, id},
		},
		{
			name:   "order already names the unique field",
			schema: testSchema(t),
			args:   args{order: []OrderBy{idDesc, createdAt}},
			want:   []OrderBy{idDesc, createdAt},
		},
		{
			name:   "merged order already names the unique field",
			schema: testSchema(t),
			args: args{order: MergeOrder(
				[]OrderBy{createdAtDesc},
				[]OrderBy{idDesc, displayName},
			)},
			want: []OrderBy{idDesc, displayName, createdAtDesc},
		},
		{
			name:   "request overrides the default direction",
			schema: testSchema(t),
			args: args{order: MergeOrder(
				[]OrderBy{createdAtDesc},
				[]OrderBy{createdAt},
			)},
			want: []OrderBy{createdAt, id},
		},
		{
			name:   "empty order",
			schema: testSchema(t),
			args:   args{},
			want:   []OrderBy{id},
		},
		{
			name:   "no unique field declared",
			schema: NewSchema(NewField("a").Ref("a").Int().Sortable()).MustBuild(),
			args:   args{order: []OrderBy{a}},
			want:   []OrderBy{a},
		},

		{
			name:   "composite key on an empty order",
			schema: composite,
			args:   args{},
			want:   []OrderBy{id, createdAt},
		},
		{
			name:   "composite key is fully missing",
			schema: composite,
			args:   args{order: []OrderBy{amountDesc}},
			want:   []OrderBy{amountDesc, id, createdAt},
		},
		{
			name:   "composite key misses its time field",
			schema: composite,
			args:   args{order: []OrderBy{idDesc}},
			want:   []OrderBy{idDesc, createdAt},
		},
		{
			name:   "composite key misses its id field",
			schema: composite,
			args:   args{order: []OrderBy{createdAtDesc}},
			want:   []OrderBy{createdAtDesc, id},
		},
		{
			name:   "composite key keeps explicit positions",
			schema: composite,
			args:   args{order: []OrderBy{createdAtDesc, amount, idDesc}},
			want:   []OrderBy{createdAtDesc, amount, idDesc},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, test.schema.TotalOrder(test.args.order))
		})
	}
}
