package flop

import (
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/ysomad/flop/aip132"
)

func TestSchemaBuilder_Build(t *testing.T) {
	t.Parallel()
	type args struct {
		fields []*Field
	}
	tests := []struct {
		name       string
		args       args
		wantUnique string
		wantColumn string
		wantErr    errorFunc
	}{
		{
			name: "every capability",
			args: args{fields: []*Field{
				NewField("id").Column("u.id").Int().Unique().Build(),
				NewField("display_name").Column("u.name").String().Filterable().Sortable().Implicit().Build(),
				NewField("created_at").Column("u.created_at").Time().Filterable().Sortable().Build(),
				NewField("active").Column("u.active").Bool().Filterable().Build(),
				NewField("rating").Column("u.rating").Float().Filterable().Build(),
			}},
			wantUnique: "id",
			wantErr:    assert.NoError,
		},
		{name: "no fields", args: args{}, wantErr: assert.NoError},
		{
			name:    "multi segment path",
			args:    args{fields: []*Field{NewField("metadata", "tags").Column("m.tags").String().Filterable().Build()}},
			wantErr: assert.NoError,
		},
		{
			name: "value on a sortable field",
			args: args{fields: []*Field{
				NewField("id").Column("u.id").String().Unique().
					Value(func(row string) any { return row }).Build(),
			}},
			wantUnique: "id",
			wantErr:    assert.NoError,
		},
		{
			// Only an ordering field is ever read off a row, so a value
			// anywhere else is a mistake rather than something unused.
			name: "value on a field that is not sortable",
			args: args{fields: []*Field{
				NewField("active").Column("u.active").Bool().Filterable().
					Value(func(row string) any { return row }).Build(),
			}},
			wantErr: assert.Error,
		},
		{name: "nil field", args: args{fields: []*Field{nil}}, wantErr: assert.Error},
		{
			name:    "no path",
			args:    args{fields: []*Field{NewField().Column("u.id").Int().Build()}},
			wantErr: assert.Error,
		},
		{
			name:       "no column falls back to the path",
			args:       args{fields: []*Field{NewField("id").Int().Build()}},
			wantColumn: "id",
			wantErr:    assert.NoError,
		},
		{
			name:       "column overrides the path",
			args:       args{fields: []*Field{NewField("id").Column("u.id").Int().Build()}},
			wantColumn: "u.id",
			wantErr:    assert.NoError,
		},
		{
			name:    "no type",
			args:    args{fields: []*Field{NewField("id").Column("u.id").Build()}},
			wantErr: assert.Error,
		},
		{
			name:    "implicit on a non string field",
			args:    args{fields: []*Field{NewField("age").Column("u.age").Int().Implicit().Build()}},
			wantErr: assert.Error,
		},
		{
			name: "duplicate path",
			args: args{fields: []*Field{
				NewField("id").Column("u.id").Int().Build(),
				NewField("id").Column("u.other").Int().Build(),
			}},
			wantErr: assert.Error,
		},
		{
			name: "two unique fields",
			args: args{fields: []*Field{
				NewField("id").Column("u.id").Int().Unique().Build(),
				NewField("uuid").Column("u.uuid").String().Unique().Build(),
			}},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := NewSchema(test.args.fields...).Build()
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, len(test.args.fields), len(got.Fields()))
			if test.wantColumn != "" {
				assert.Equal(t, test.wantColumn, got.Fields()[0].Column())
			}
			if test.wantUnique == "" {
				assert.Equal(t, (*Field)(nil), got.UniqueField())
				return
			}
			assert.Equal(t, test.wantUnique, got.UniqueField().Path().String())
		})
	}
}

func TestSchemaBuilder_MustBuild(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		NewSchema(NewField("id").Build()).MustBuild()
	})
	s := NewSchema(NewField("id").Column("u.id").Int().Build()).MustBuild()
	assert.Equal(t, 1, len(s.Fields()))
}

// testSchema declares one field per capability, so a lookup row can name the
// field it expects to be refused by.
func testSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(
		NewField("id").Column("u.id").Int().Unique().Build(),
		NewField("display_name").Column("u.name").String().Filterable().Sortable().Implicit().Build(),
		NewField("created_at").Column("u.created_at").Time().Filterable().Sortable().Build(),
		NewField("active").Column("u.active").Bool().Filterable().Build(),
		NewField("rating").Column("u.rating").Float().Filterable().Build(),
		NewField("secret").Column("u.secret").String().Build(),
		NewField("metadata", "tags").Column("m.tags").String().Filterable().Build(),
	).Build()
	assert.NoError(t, err)
	return s
}

func TestSchema_FilterableField(t *testing.T) {
	t.Parallel()
	type args struct {
		path aip132.FieldPath
	}
	tests := []struct {
		name       string
		args       args
		wantColumn string
		wantErr    errorFunc
	}{
		{
			name:       "filterable",
			args:       args{path: aip132.NewFieldPath("display_name")},
			wantColumn: "u.name",
			wantErr:    assert.NoError,
		},
		{
			name:       "multi segment path",
			args:       args{path: aip132.NewFieldPath("metadata", "tags")},
			wantColumn: "m.tags",
			wantErr:    assert.NoError,
		},
		{name: "declared but not filterable", args: args{path: aip132.NewFieldPath("secret")}, wantErr: assert.Error},
		{name: "undeclared", args: args{path: aip132.NewFieldPath("nope")}, wantErr: assert.Error},
		{
			// Lookup is exact: a prefix of a declared path is not the field.
			name:    "prefix of a declared path",
			args:    args{path: aip132.NewFieldPath("metadata")},
			wantErr: assert.Error,
		},
		{
			name:    "declared path with an extra segment",
			args:    args{path: aip132.NewFieldPath("display_name", "extra")},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := testSchema(t).FilterableField(test.args.path)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.wantColumn, got.Column())
		})
	}
}

func TestSchema_SortableField(t *testing.T) {
	t.Parallel()
	type args struct {
		path aip132.FieldPath
	}
	tests := []struct {
		name       string
		args       args
		wantColumn string
		wantErr    errorFunc
	}{
		{
			name:       "sortable",
			args:       args{path: aip132.NewFieldPath("created_at")},
			wantColumn: "u.created_at",
			wantErr:    assert.NoError,
		},
		{
			name:       "unique implies sortable",
			args:       args{path: aip132.NewFieldPath("id")},
			wantColumn: "u.id",
			wantErr:    assert.NoError,
		},
		{name: "filterable but not sortable", args: args{path: aip132.NewFieldPath("active")}, wantErr: assert.Error},
		{name: "undeclared", args: args{path: aip132.NewFieldPath("nope")}, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := testSchema(t).SortableField(test.args.path)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.wantColumn, got.Column())
		})
	}
}

func TestField_accessors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field *Field
		want  Field
	}{
		{
			name:  "every capability",
			field: NewField("display_name").Column("u.name").String().Implicit().Unique().Build(),
			want: Field{
				column: "u.name", typ: TypeString,
				filterable: true, sortable: true, implicit: true, unique: true,
			},
		},
		{
			name:  "nothing declared",
			field: NewField("secret").Column("u.secret").Time().Build(),
			want:  Field{column: "u.secret", typ: TypeTime},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want.column, test.field.Column())
			assert.Equal(t, test.want.typ, test.field.Type())
			assert.Equal(t, test.want.filterable, test.field.Filterable())
			assert.Equal(t, test.want.sortable, test.field.Sortable())
			assert.Equal(t, test.want.implicit, test.field.Implicit())
			assert.Equal(t, test.want.unique, test.field.Unique())
		})
	}
}

func TestType_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		typ  Type
		want string
	}{
		{name: "string", typ: TypeString, want: "string"},
		{name: "int", typ: TypeInt, want: "int"},
		{name: "float", typ: TypeFloat, want: "float"},
		{name: "bool", typ: TypeBool, want: "bool"},
		{name: "time", typ: TypeTime, want: "time"},
		{name: "unset", typ: 0, want: "Type(0)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, test.typ.String())
		})
	}
}
