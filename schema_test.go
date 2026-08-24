package flop

import (
	"testing"

	"github.com/ysomad/flop/internal/assert"
	"github.com/ysomad/flop/orderby"
)

func TestSchemaBuilder_Build(t *testing.T) {
	t.Parallel()
	type args struct {
		fields []*FieldBuilder
	}
	tests := []struct {
		name       string
		args       args
		wantUnique string
		wantRef    string
		wantErr    assert.ErrorFunc
	}{
		{
			name: "every capability",
			args: args{fields: []*FieldBuilder{
				NewField("id").Ref("u.id").Int().Unique(),
				NewField("display_name").Ref("u.name").String().Filterable().Sortable().Implicit(),
				NewField("created_at").Ref("u.created_at").Time().Filterable().Sortable(),
				NewField("active").Ref("u.active").Bool().Filterable(),
				NewField("rating").Ref("u.rating").Float().Filterable(),
			}},
			wantUnique: "id",
			wantErr:    assert.NoError,
		},
		{name: "no fields", args: args{}, wantErr: assert.NoError},
		{
			name:    "multi segment path",
			args:    args{fields: []*FieldBuilder{NewField("metadata", "tags").Ref("m.tags").String().Filterable()}},
			wantErr: assert.NoError,
		},
		{
			name: "value on a sortable field",
			args: args{fields: []*FieldBuilder{
				NewField("id").Ref("u.id").String().Unique().
					Value(func(row string) any { return row }),
			}},
			wantUnique: "id",
			wantErr:    assert.NoError,
		},
		{
			// Only an ordering field is ever read off a row, so a value
			// anywhere else is a mistake rather than something unused.
			name: "value on a field that is not sortable",
			args: args{fields: []*FieldBuilder{
				NewField("active").Ref("u.active").Bool().Filterable().
					Value(func(row string) any { return row }),
			}},
			wantErr: assert.Error,
		},
		{name: "nil field", args: args{fields: []*FieldBuilder{nil}}, wantErr: assert.Error},
		{
			name:    "no path",
			args:    args{fields: []*FieldBuilder{NewField().Ref("u.id").Int()}},
			wantErr: assert.Error,
		},
		{
			name:    "no ref falls back to the path",
			args:    args{fields: []*FieldBuilder{NewField("id").Int()}},
			wantRef: "id",
			wantErr: assert.NoError,
		},
		{
			name:    "ref overrides the path",
			args:    args{fields: []*FieldBuilder{NewField("id").Ref("u.id").Int()}},
			wantRef: "u.id",
			wantErr: assert.NoError,
		},
		{
			name:    "no type",
			args:    args{fields: []*FieldBuilder{NewField("id").Ref("u.id")}},
			wantErr: assert.Error,
		},
		{
			name:    "implicit on a non string field",
			args:    args{fields: []*FieldBuilder{NewField("age").Ref("u.age").Int().Implicit()}},
			wantErr: assert.Error,
		},
		{
			name: "duplicate path",
			args: args{fields: []*FieldBuilder{
				NewField("id").Ref("u.id").Int(),
				NewField("id").Ref("u.other").Int(),
			}},
			wantErr: assert.Error,
		},
		{
			name: "two unique fields",
			args: args{fields: []*FieldBuilder{
				NewField("id").Ref("u.id").Int().Unique(),
				NewField("uuid").Ref("u.uuid").String().Unique(),
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
			if test.wantRef != "" {
				assert.Equal(t, test.wantRef, got.Fields()[0].Ref())
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
		NewSchema(NewField("id")).MustBuild()
	})
	s := NewSchema(NewField("id").Ref("u.id").Int()).MustBuild()
	assert.Equal(t, 1, len(s.Fields()))
}

// testSchema declares one field per capability, so a lookup row can name the
// field it expects to be refused by.
func testSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(
		NewField("id").Ref("u.id").Int().Unique(),
		NewField("display_name").Ref("u.name").String().Filterable().Sortable().Implicit(),
		NewField("created_at").Ref("u.created_at").Time().Filterable().Sortable(),
		NewField("active").Ref("u.active").Bool().Filterable(),
		NewField("rating").Ref("u.rating").Float().Filterable(),
		NewField("secret").Ref("u.secret").String(),
		NewField("metadata", "tags").Ref("m.tags").String().Filterable(),
	).Build()
	assert.NoError(t, err)
	return s
}

func TestSchema_FilterableField(t *testing.T) {
	t.Parallel()
	type args struct {
		path orderby.FieldPath
	}
	tests := []struct {
		name    string
		args    args
		wantRef string
		wantErr assert.ErrorFunc
	}{
		{
			name:    "filterable",
			args:    args{path: orderby.NewFieldPath("display_name")},
			wantRef: "u.name",
			wantErr: assert.NoError,
		},
		{
			name:    "multi segment path",
			args:    args{path: orderby.NewFieldPath("metadata", "tags")},
			wantRef: "m.tags",
			wantErr: assert.NoError,
		},
		{name: "declared but not filterable", args: args{path: orderby.NewFieldPath("secret")}, wantErr: assert.Error},
		{name: "undeclared", args: args{path: orderby.NewFieldPath("nope")}, wantErr: assert.Error},
		{
			// Lookup is exact: a prefix of a declared path is not the field.
			name:    "prefix of a declared path",
			args:    args{path: orderby.NewFieldPath("metadata")},
			wantErr: assert.Error,
		},
		{
			name:    "declared path with an extra segment",
			args:    args{path: orderby.NewFieldPath("display_name", "extra")},
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
			assert.Equal(t, test.wantRef, got.Ref())
		})
	}
}

func TestSchema_SortableField(t *testing.T) {
	t.Parallel()
	type args struct {
		path orderby.FieldPath
	}
	tests := []struct {
		name    string
		args    args
		wantRef string
		wantErr assert.ErrorFunc
	}{
		{
			name:    "sortable",
			args:    args{path: orderby.NewFieldPath("created_at")},
			wantRef: "u.created_at",
			wantErr: assert.NoError,
		},
		{
			name:    "unique implies sortable",
			args:    args{path: orderby.NewFieldPath("id")},
			wantRef: "u.id",
			wantErr: assert.NoError,
		},
		{name: "filterable but not sortable", args: args{path: orderby.NewFieldPath("active")}, wantErr: assert.Error},
		{name: "undeclared", args: args{path: orderby.NewFieldPath("nope")}, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := testSchema(t).SortableField(test.args.path)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.wantRef, got.Ref())
		})
	}
}

func TestField_accessors(t *testing.T) {
	t.Parallel()
	field := &NewField("metadata", "tags").Ref("m.tags").String().field
	assert.Equal(t, orderby.NewFieldPath("metadata", "tags"), field.Path())
	assert.Equal(t, "m.tags", field.Ref())
}

func TestFieldType_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		typ  fieldType
		want string
	}{
		{name: "string", typ: fieldTypeString, want: "string"},
		{name: "int", typ: fieldTypeInt, want: "int"},
		{name: "float", typ: fieldTypeFloat, want: "float"},
		{name: "bool", typ: fieldTypeBool, want: "bool"},
		{name: "time", typ: fieldTypeTime, want: "time"},
		{name: "duration", typ: fieldTypeDuration, want: "duration"},
		{name: "unset", typ: 0, want: "Type(0)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, test.typ.String())
		})
	}
}
