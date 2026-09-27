package flop

import (
	"testing"

	"github.com/ysomad/flop/internal/assert"
)

func TestNewSchema(t *testing.T) {
	t.Parallel()
	segments := []string{"profile", "name"}
	field := NewField(segments...).String().Filterable().Sortable().Value(func(row string) any { return row })
	fields := []*FieldBuilder{field}
	builder := NewSchema(fields...)

	// A caller changing what it passed in afterwards cannot reach the builder.
	segments[0] = "changed"
	fields[0] = nil
	schema := builder.MustBuild()
	assert.Equal(t, "profile.name", schema.Fields()[0].Ref())

	// The field builder itself stays live, so a later declaration is picked up.
	field.Ref("other").Int().Value(func(row int64) any { return row })
	assert.Equal(t, "other", builder.MustBuild().Fields()[0].Ref())
}

func TestSchemaBuilder_Build(t *testing.T) {
	t.Parallel()
	type args struct {
		fields []*FieldBuilder
	}
	tests := []struct {
		name         string
		args         args
		wantUnique   string
		wantRef      string
		wantSentinel error
		wantErr      assert.ErrorFunc
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

		{
			name:         "nil callback",
			args:         args{fields: []*FieldBuilder{NewField("id").String().Unique().Value[string](nil)}},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{
			name:         "digit leading path",
			args:         args{fields: []*FieldBuilder{NewField("1field").String()}},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{
			name:         "digit only path",
			args:         args{fields: []*FieldBuilder{NewField("9").Int()}},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{
			name:         "quoted segment",
			args:         args{fields: []*FieldBuilder{NewField("1 field").String()}},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{
			name:         "nested digit leading segment",
			args:         args{fields: []*FieldBuilder{NewField("outer", "2inner").String()}},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{
			// A safe reference does not excuse the path clients name.
			name:         "digit leading path with an explicit reference",
			args:         args{fields: []*FieldBuilder{NewField("3field").Ref("safe_column").String()}},
			wantSentinel: ErrDeclaration,
			wantErr:      assert.Error,
		},
		{name: "trailing digit", args: args{fields: []*FieldBuilder{NewField("field1").String()}}, wantErr: assert.NoError},
		{name: "underscore leading", args: args{fields: []*FieldBuilder{NewField("_1field").String()}}, wantErr: assert.NoError},
		{name: "unicode path", args: args{fields: []*FieldBuilder{NewField("世界").String()}}, wantErr: assert.NoError},
		{name: "dot inside a segment", args: args{fields: []*FieldBuilder{NewField("odd.name").String()}}, wantErr: assert.NoError},
		{name: "unicode digit", args: args{fields: []*FieldBuilder{NewField("９field").String()}}, wantErr: assert.NoError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := NewSchema(test.args.fields...).Build()
			test.wantErr(t, gotErr)
			if gotErr != nil {
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
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

func TestSchemaBuilder_CompositeKey(t *testing.T) {
	t.Parallel()
	type args struct {
		paths []string
	}
	tests := []struct {
		name    string
		fields  []*FieldBuilder
		args    args
		wantErr assert.ErrorFunc
	}{
		{name: "composite", args: args{paths: []string{"id", "created_at"}}, wantErr: assert.NoError},
		{name: "key order", args: args{paths: []string{"created_at", "id"}}, wantErr: assert.NoError},
		{name: "single field", args: args{paths: []string{"id"}}, wantErr: assert.Error},
		{name: "empty", args: args{}, wantErr: assert.Error},
		{name: "duplicate", args: args{paths: []string{"id", "id"}}, wantErr: assert.Error},
		{name: "undeclared", args: args{paths: []string{"id", "missing"}}, wantErr: assert.Error},
		{
			name: "nested public path",
			args: args{paths: []string{"user.id", "created_at"}},
			fields: []*FieldBuilder{
				NewField("user", "id").Ref("u.id").String().Sortable(),
				NewField("created_at").Time().Sortable(),
			},
			wantErr: assert.NoError,
		},
		{
			name: "backend ref is not a public path",
			args: args{paths: []string{"u.id", "created_at"}},
			fields: []*FieldBuilder{
				NewField("id").Ref("u.id").String().Sortable(),
				NewField("created_at").Time().Sortable(),
			},
			wantErr: assert.Error,
		},
		{
			name: "not sortable",
			args: args{paths: []string{"id", "created_at"}},
			fields: []*FieldBuilder{
				NewField("id").String(),
				NewField("created_at").Time().Sortable(),
			},
			wantErr: assert.Error,
		},
		{
			name: "conflicting declaration",
			args: args{paths: []string{"id", "created_at"}},
			fields: []*FieldBuilder{
				NewField("id").String().Unique(),
				NewField("created_at").Time().Sortable(),
			},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fields := test.fields
			if fields == nil {
				fields = []*FieldBuilder{NewField("id").String().Sortable(), NewField("created_at").Time().Sortable()}
			}
			schema, err := NewSchema(fields...).CompositeKey(test.args.paths...).Build()
			test.wantErr(t, err)
			if err != nil {
				assert.IsError(t, err, ErrDeclaration)
				return
			}
			assert.Equal(t, len(test.args.paths), len(schema.UniqueFields()))
			for i, field := range schema.UniqueFields() {
				assert.Equal(t, test.args.paths[i], field.Path().String())
			}
			assert.Equal(t, (*Field)(nil), schema.UniqueField())
		})
	}

	// The key is snapshotted, and the same builder can be keyed again.
	paths := []string{"id", "created_at"}
	builder := NewSchema(
		NewField("id").String().Sortable(),
		NewField("created_at").Time().Sortable(),
	).CompositeKey(paths...)
	paths[0] = "created_at"
	schema := builder.MustBuild()
	reordered := builder.CompositeKey("created_at", "id").MustBuild()
	assert.Equal(t, "created_at", reordered.UniqueFields()[0].Path().String())
	assert.Equal(t, "id", reordered.UniqueFields()[1].Path().String())
	assert.Equal(t, "id", schema.UniqueFields()[0].Path().String())
	assert.Equal(t, "created_at", schema.UniqueFields()[1].Path().String())
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

func TestSchema_Fields(t *testing.T) {
	t.Parallel()
	schema := NewSchema(NewField("profile", "name").String().Filterable()).MustBuild()
	fields := schema.Fields()
	fields[0] = nil
	schema.Fields()[0].Path().Segments()[0] = "changed"
	assert.Equal(t, 1, len(schema.Fields()))
	assert.Equal(t, []string{"profile", "name"}, schema.Fields()[0].Path().Segments())
}

func TestSchema_UniqueFields(t *testing.T) {
	t.Parallel()
	schema := NewSchema(
		NewField("id").String().Sortable(),
		NewField("created_at").Time().Sortable(),
	).CompositeKey("id", "created_at").MustBuild()
	schema.UniqueFields()[0] = nil
	assert.Equal(t, "id", schema.UniqueFields()[0].Path().String())
	assert.Equal(t, "created_at", schema.UniqueFields()[1].Path().String())
}

func TestSchema_FilterableField(t *testing.T) {
	t.Parallel()
	type args struct {
		path FieldPath
	}
	tests := []struct {
		name    string
		args    args
		wantRef string
		wantErr assert.ErrorFunc
	}{
		{
			name:    "filterable",
			args:    args{path: NewFieldPath("display_name")},
			wantRef: "u.name",
			wantErr: assert.NoError,
		},
		{
			name:    "multi segment path",
			args:    args{path: NewFieldPath("metadata", "tags")},
			wantRef: "m.tags",
			wantErr: assert.NoError,
		},
		{name: "declared but not filterable", args: args{path: NewFieldPath("secret")}, wantErr: assert.Error},
		{name: "undeclared", args: args{path: NewFieldPath("nope")}, wantErr: assert.Error},
		{
			// Lookup is exact: a prefix of a declared path is not the field.
			name:    "prefix of a declared path",
			args:    args{path: NewFieldPath("metadata")},
			wantErr: assert.Error,
		},
		{
			name:    "declared path with an extra segment",
			args:    args{path: NewFieldPath("display_name", "extra")},
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
		path FieldPath
	}
	tests := []struct {
		name    string
		args    args
		wantRef string
		wantErr assert.ErrorFunc
	}{
		{
			name:    "sortable",
			args:    args{path: NewFieldPath("created_at")},
			wantRef: "u.created_at",
			wantErr: assert.NoError,
		},
		{
			name:    "unique implies sortable",
			args:    args{path: NewFieldPath("id")},
			wantRef: "u.id",
			wantErr: assert.NoError,
		},
		{name: "filterable but not sortable", args: args{path: NewFieldPath("active")}, wantErr: assert.Error},
		{name: "undeclared", args: args{path: NewFieldPath("nope")}, wantErr: assert.Error},
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

func TestSchema_SortableFields(t *testing.T) {
	t.Parallel()
	id := OrderBy{FieldPath: NewFieldPath("id")}
	createdAt := OrderBy{FieldPath: NewFieldPath("created_at")}

	type args struct {
		order []OrderBy
	}
	tests := []struct {
		name         string
		args         args
		wantRefs     []string
		wantSentinel error
		wantErr      assert.ErrorFunc
	}{
		{name: "empty order", args: args{}, wantRefs: []string{}, wantErr: assert.NoError},
		{
			name:     "every clause",
			args:     args{order: []OrderBy{createdAt, id}},
			wantRefs: []string{"u.created_at", "u.id"},
			wantErr:  assert.NoError,
		},
		{
			name:         "repeated field",
			args:         args{order: []OrderBy{id, id}},
			wantSentinel: ErrInvalidOrder,
			wantErr:      assert.Error,
		},
		{
			name:    "not sortable",
			args:    args{order: []OrderBy{{FieldPath: NewFieldPath("active")}}},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := testSchema(t).SortableFields(test.args.order)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
				return
			}
			refs := make([]string, 0, len(got))
			for _, field := range got {
				refs = append(refs, field.Ref())
			}
			assert.Equal(t, test.wantRefs, refs)
		})
	}
}

func TestField_Path(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field *FieldBuilder
		want  FieldPath
	}{
		{name: "single segment", field: NewField("id").Int(), want: NewFieldPath("id")},
		{
			name:  "multi segment",
			field: NewField("metadata", "tags").Ref("m.tags").String(),
			want:  NewFieldPath("metadata", "tags"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, test.field.field.Path())
		})
	}
}

func TestField_Ref(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field *FieldBuilder
		want  string
	}{
		{name: "explicit", field: NewField("metadata", "tags").Ref("m.tags").String(), want: "m.tags"},
		{name: "falls back to the path", field: NewField("metadata", "tags").String(), want: "metadata.tags"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schema := NewSchema(test.field).MustBuild()
			assert.Equal(t, test.want, schema.Fields()[0].Ref())
		})
	}
}

func Test_fieldType_String(t *testing.T) {
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
