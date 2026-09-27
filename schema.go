package flop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ysomad/flop/aip132"
)

// fieldType is the type a field's values carry. It decides which comparators a
// field accepts and what Go type its arguments coerce to.
type fieldType uint8

const (
	fieldTypeString fieldType = iota + 1
	fieldTypeInt
	fieldTypeFloat
	fieldTypeBool
	fieldTypeTime
	fieldTypeDuration
)

func (t fieldType) String() string {
	switch t {
	case fieldTypeString:
		return "string"
	case fieldTypeInt:
		return "int"
	case fieldTypeFloat:
		return "float"
	case fieldTypeBool:
		return "bool"
	case fieldTypeTime:
		return "time"
	case fieldTypeDuration:
		return "duration"
	}
	return fmt.Sprintf("Type(%d)", int(t))
}

// Field is one field of a collection, declared with [NewField].
//
// A field has a public path and a private backend reference. The path is what a
// client writes in a filter or order_by, what errors name the field by, and what
// a cursor is bound to. The ref is what generated queries use. The two are free
// to differ:
//
//	flop.NewField("user_id").Ref("u.id").Int().Filterable().Sortable().
//		Value(func(u user) any { return u.ID })
//
// A ref defaults to the path. Declare one when a backend addresses the field by
// another name or syntax.
//
// Renaming a ref is invisible to clients. Renaming a path is a breaking change:
// filters clients already send stop resolving, and cursors they already hold
// stop matching, because the binding is taken over the paths the order names.
type Field struct {
	path aip132.FieldPath
	ref  string
	typ  fieldType

	filterable bool
	sortable   bool
	implicit   bool
	unique     bool

	value func(any) (any, bool)
}

// Path returns the field path clients name the field by.
func (f *Field) Path() aip132.FieldPath { return f.path }

// Ref returns the backend reference generated queries use for the field.
func (f *Field) Ref() string { return f.ref }

// FieldBuilder builds a [Field].
type FieldBuilder struct {
	field    Field
	nilValue bool
}

// NewField starts a field at the given path segments. Segments are joined by
// the AIP-161 traversal operator, so NewField("metadata", "tags") declares the
// path metadata.tags. No segment may begin with an ASCII digit.
func NewField(segments ...string) *FieldBuilder {
	return &FieldBuilder{field: Field{path: aip132.NewFieldPath(segments...)}}
}

// Ref sets the backend reference generated queries use. It defaults to the
// field's path, so only a ref that differs from it has to be declared.
//
// Only assign trusted constants. An adapter may embed the ref directly into its
// query syntax; user input reaching it can become an injection vulnerability.
func (b *FieldBuilder) Ref(ref string) *FieldBuilder {
	b.field.ref = ref
	return b
}

// String types the field as text.
func (b *FieldBuilder) String() *FieldBuilder { return b.withType(fieldTypeString) }

// Int types the field as a 64-bit signed integer.
func (b *FieldBuilder) Int() *FieldBuilder { return b.withType(fieldTypeInt) }

// Float types the field as a 64-bit float.
func (b *FieldBuilder) Float() *FieldBuilder { return b.withType(fieldTypeFloat) }

// Bool types the field as a boolean.
func (b *FieldBuilder) Bool() *FieldBuilder { return b.withType(fieldTypeBool) }

// Time types the field as an RFC 3339 timestamp.
func (b *FieldBuilder) Time() *FieldBuilder { return b.withType(fieldTypeTime) }

// Duration types the field as a Go duration, such as 250ms or 2h30m.
func (b *FieldBuilder) Duration() *FieldBuilder { return b.withType(fieldTypeDuration) }

func (b *FieldBuilder) withType(t fieldType) *FieldBuilder {
	b.field.typ = t
	return b
}

// Filterable allows the field to be named in a filter.
func (b *FieldBuilder) Filterable() *FieldBuilder {
	b.field.filterable = true
	return b
}

// Sortable allows the field to be named in an order_by.
func (b *FieldBuilder) Sortable() *FieldBuilder {
	b.field.sortable = true
	return b
}

// Implicit makes a bare filter value search this field, and implies
// [FieldBuilder.Filterable]. Only string fields may be implicit.
func (b *FieldBuilder) Implicit() *FieldBuilder {
	b.field.implicit = true
	return b.Filterable()
}

// Unique declares a single-field unique key and implies [FieldBuilder.Sortable].
// For rows identified by several fields together, use [SchemaBuilder.CompositeKey].
func (b *FieldBuilder) Unique() *FieldBuilder {
	b.field.unique = true
	return b.Sortable()
}

// Value declares how to read the field off a row, which is what
// [Schema.EncodeCursor] addresses a row by. Only a field a cursor may order by
// needs one.
func (b *FieldBuilder) Value[T any](fn func(T) any) *FieldBuilder {
	b.nilValue = fn == nil
	if fn == nil {
		b.field.value = nil
		return b
	}
	b.field.value = func(row any) (any, bool) {
		typed, ok := row.(T)
		if !ok {
			return nil, false
		}
		return fn(typed), true
	}
	return b
}

// Schema is the set of fields one collection exposes.
type Schema struct {
	fields    []*Field
	byPath    map[string]*Field
	implicit  []*Field
	uniqueKey []*Field
}

// SchemaBuilder builds a [Schema].
type SchemaBuilder struct {
	fields          []*FieldBuilder
	compositePaths  []string
	compositeKeySet bool
}

// NewSchema starts a schema holding the given fields.
func NewSchema(fields ...*FieldBuilder) *SchemaBuilder {
	return &SchemaBuilder{fields: slices.Clone(fields)}
}

// CompositeKey declares at least two fields whose combined values uniquely
// identify a row. Paths are public field names, such as "id" or "user.id",
// matching Field.Path().String(). Each field must be declared sortable.
// Cursor orders must contain every key field; TotalOrder appends missing ones
// in the order given here. This declaration cannot be combined with
// [FieldBuilder.Unique]. A later call replaces the previous key declaration.
func (b *SchemaBuilder) CompositeKey(paths ...string) *SchemaBuilder {
	b.compositePaths = slices.Clone(paths)
	b.compositeKeySet = true
	return b
}

// Build validates the declared fields and returns the schema.
func (b *SchemaBuilder) Build() (*Schema, error) {
	s := &Schema{
		fields: make([]*Field, 0, len(b.fields)),
		byPath: make(map[string]*Field, len(b.fields)),
	}
	for _, field := range b.fields {
		if field == nil {
			return nil, errorf(ErrDeclaration, "field is nil")
		}
		if field.nilValue {
			return nil, errorf(ErrDeclaration, "field %q has a nil value callback", field.field.path)
		}
		declaration := field.field
		f := &declaration
		if f.ref == "" {
			f.ref = f.path.String()
		}
		s.fields = append(s.fields, f)
		path := f.path.String()
		if path == "" {
			return nil, errorf(ErrDeclaration, "field has no path")
		}
		for _, segment := range f.path.Segments() {
			if len(segment) > 0 && segment[0] >= '0' && segment[0] <= '9' {
				return nil, errorf(ErrDeclaration, "field %q has a digit-leading segment %q", path, segment)
			}
		}
		if f.typ == 0 {
			return nil, errorf(ErrDeclaration, "field %q has no type", path)
		}
		if f.value != nil && !f.sortable {
			return nil, errorf(
				ErrDeclaration,
				"field %q is not sortable, so its value is never read", path,
			)
		}
		if f.implicit && f.typ != fieldTypeString {
			return nil, errorf(ErrDeclaration, "field %q is %s, so it cannot be implicit", path, f.typ)
		}
		if _, ok := s.byPath[path]; ok {
			return nil, errorf(ErrDeclaration, "field %q is declared twice", path)
		}
		if f.unique {
			if b.compositeKeySet {
				return nil, errorf(ErrDeclaration, "field %q declares Unique alongside a schema CompositeKey", path)
			}
			if len(s.uniqueKey) > 0 {
				return nil, errorf(
					ErrDeclaration,
					"fields %q and %q are both unique",
					s.uniqueKey[0].path.String(),
					path,
				)
			}
			s.uniqueKey = []*Field{f}
		}
		s.byPath[path] = f
		if f.implicit {
			s.implicit = append(s.implicit, f)
		}
	}
	if b.compositeKeySet {
		if len(b.compositePaths) < 2 {
			return nil, errorf(ErrDeclaration, "composite key needs at least two fields; use Unique for a single field")
		}
		for _, path := range b.compositePaths {
			field, ok := s.byPath[path]
			if !ok || !field.sortable {
				return nil, errorf(ErrDeclaration, "composite key field %q must be declared sortable", path)
			}
			if slices.Contains(s.uniqueKey, field) {
				return nil, errorf(ErrDeclaration, "composite key repeats field %q", path)
			}
			s.uniqueKey = append(s.uniqueKey, field)
		}
	}
	return s, nil
}

// MustBuild is [SchemaBuilder.Build] for a schema declared at init, where a
// mistake is a programmer error rather than something to report.
func (b *SchemaBuilder) MustBuild() *Schema {
	s, err := b.Build()
	if err != nil {
		panic(err)
	}
	return s
}

// Fields returns a copy of the field slice in declaration order.
func (s *Schema) Fields() []*Field { return slices.Clone(s.fields) }

// UniqueField returns the single-field unique key, or nil if the key is absent
// or composite. Use UniqueFields to inspect all key fields.
func (s *Schema) UniqueField() *Field {
	if len(s.uniqueKey) != 1 {
		return nil
	}
	return s.uniqueKey[0]
}

// UniqueFields returns a copy of the unique key's fields in key declaration order.
func (s *Schema) UniqueFields() []*Field { return slices.Clone(s.uniqueKey) }

// FilterableField returns the filterable field at path.
func (s *Schema) FilterableField(path aip132.FieldPath) (*Field, error) {
	if f, ok := s.byPath[path.String()]; ok && f.filterable {
		return f, nil
	}
	return nil, errorf(
		ErrInvalidFilter,
		"no filterable field %q, valid fields are %s",
		path.String(), s.fieldList(func(f *Field) bool { return f.filterable }),
	)
}

// SortableField returns the sortable field at path.
func (s *Schema) SortableField(path aip132.FieldPath) (*Field, error) {
	if f, ok := s.byPath[path.String()]; ok && f.sortable {
		return f, nil
	}
	return nil, errorf(
		ErrInvalidOrder,
		"no sortable field %q, valid fields are %s",
		path.String(), s.fieldList(func(f *Field) bool { return f.sortable }),
	)
}

// SortableFields resolves each term of an order to the field it names, keeping
// the order's own indexing so a term's direction is read from it directly.
func (s *Schema) SortableFields(order []aip132.OrderBy) ([]*Field, error) {
	if err := s.ValidateOrder(order); err != nil {
		return nil, err
	}
	fields := make([]*Field, 0, len(order))
	for _, term := range order {
		field, err := s.SortableField(term.FieldPath)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// fieldList names the fields with a capability, for an error that tells the
// caller what they could have written instead.
func (s *Schema) fieldList(has func(*Field) bool) string {
	names := make([]string, 0, len(s.fields))
	for _, f := range s.fields {
		if has(f) {
			names = append(names, f.path.String())
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
