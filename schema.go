package flop

import (
	"fmt"
	"strings"

	"github.com/ysomad/flop/aip132"
)

// Type is the type a field's values carry. It decides which comparators a
// field accepts and what Go type its arguments coerce to.
type Type int

const (
	TypeString Type = iota + 1
	TypeInt
	TypeFloat
	TypeBool
	TypeTime
)

func (t Type) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeBool:
		return "bool"
	case TypeTime:
		return "time"
	}
	return fmt.Sprintf("Type(%d)", int(t))
}

// Field is one field of a collection, declared with [NewField].
//
// A field has two names, pointing opposite ways. The path is the public
// contract: what a client writes in a filter or order_by, what errors name it
// by, and what a cursor is bound to. The column is what generated SQL selects
// it by, and never leaves the server. The two are free to differ:
//
//	flop.NewField("user_id").Column("u.id").Int().Filterable().Sortable().
//		Value(func(u user) any { return u.ID }).Build()
//
// so a request filtering on user_id > 7 emits u.id > $1. A field that declares
// no column selects the one its path names, which is what a collection drawn
// from a single table wants. Declare one for a path that AIP-161 quotes, since
// the backticks would otherwise reach the query.
//
// Renaming a column is invisible to clients. Renaming a path is a breaking
// change to the API: filters clients already send stop resolving, and cursors
// they already hold stop matching, because the binding is taken over the paths
// the order names.
type Field struct {
	path   aip132.FieldPath
	column string
	typ    Type

	filterable bool
	sortable   bool
	implicit   bool
	unique     bool

	value func(any) (any, bool)
}

// Path returns the field path clients name the field by. It is the public half
// of the field: see [Field] for how it relates to the column.
func (f *Field) Path() aip132.FieldPath { return f.path }

// Column returns the storage name generated queries refer to the field by. It
// is the private half of the field: see [Field] for how it relates to the path.
func (f *Field) Column() string { return f.column }

// Type returns the type the field's values carry.
func (f *Field) Type() Type { return f.typ }

// Filterable reports whether the field may appear in a filter.
func (f *Field) Filterable() bool { return f.filterable }

// Sortable reports whether the field may appear in an order_by.
func (f *Field) Sortable() bool { return f.sortable }

// Implicit reports whether a bare filter value searches this field.
func (f *Field) Implicit() bool { return f.implicit }

// Unique reports whether the field orders rows totally on its own, making it
// a valid tie-breaker for cursor pagination.
func (f *Field) Unique() bool { return f.unique }

// FieldBuilder builds a [Field].
type FieldBuilder struct {
	field Field
}

// NewField starts a field at the given path segments. Segments are joined by
// the AIP-161 traversal operator, so NewField("metadata", "tags") declares the
// path metadata.tags.
func NewField(segments ...string) *FieldBuilder {
	return &FieldBuilder{field: Field{path: aip132.NewFieldPath(segments...)}}
}

// Column sets the storage name generated queries use, such as "u.created_at".
// It defaults to the field's path, so only a column that differs from it has to
// be declared. It is independent of the path clients filter and sort by, so a
// column may be renamed, qualified or moved to another table without breaking a
// request.
//
// Only assign constants. The name is written into SQL as given, and it is the
// one part of a declaration that is: every value a request carries is bound as
// an argument instead. User input reaching here is a SQL injection.
func (b *FieldBuilder) Column(name string) *FieldBuilder {
	b.field.column = name
	return b
}

// String types the field as text.
func (b *FieldBuilder) String() *FieldBuilder { return b.withType(TypeString) }

// Int types the field as a 64-bit signed integer.
func (b *FieldBuilder) Int() *FieldBuilder { return b.withType(TypeInt) }

// Float types the field as a 64-bit float.
func (b *FieldBuilder) Float() *FieldBuilder { return b.withType(TypeFloat) }

// Bool types the field as a boolean.
func (b *FieldBuilder) Bool() *FieldBuilder { return b.withType(TypeBool) }

// Time types the field as an RFC 3339 timestamp.
func (b *FieldBuilder) Time() *FieldBuilder { return b.withType(TypeTime) }

func (b *FieldBuilder) withType(t Type) *FieldBuilder {
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

// Unique declares that the field orders rows totally, and implies
// [FieldBuilder.Sortable]. Cursor pagination needs one such field to page
// without repeating or dropping rows, and a schema may declare at most one.
func (b *FieldBuilder) Unique() *FieldBuilder {
	b.field.unique = true
	return b.Sortable()
}

// Value declares how to read the field off a row, which is what
// [Schema.EncodeCursor] addresses a row by. Only a field a cursor may order by
// needs one.
func (b *FieldBuilder) Value[T any](fn func(T) any) *FieldBuilder {
	b.field.value = func(row any) (any, bool) {
		typed, ok := row.(T)
		if !ok {
			return nil, false
		}
		return fn(typed), true
	}
	return b
}

// Build returns the declared field, whose column defaults to its path.
func (b *FieldBuilder) Build() *Field {
	field := b.field
	if field.column == "" {
		field.column = field.path.String()
	}
	return &field
}

// Schema is the set of fields one collection exposes.
type Schema struct {
	fields    []*Field
	byPath    map[string]*Field
	implicit  []*Field
	uniqueKey *Field
}

// SchemaBuilder builds a [Schema].
type SchemaBuilder struct {
	fields []*Field
}

// NewSchema starts a schema holding the given fields.
func NewSchema(fields ...*Field) *SchemaBuilder {
	return &SchemaBuilder{fields: fields}
}

// Build validates the declared fields and returns the schema.
func (b *SchemaBuilder) Build() (*Schema, error) {
	s := &Schema{
		fields: b.fields,
		byPath: make(map[string]*Field, len(b.fields)),
	}
	for _, f := range b.fields {
		if f == nil {
			return nil, errorf(ErrDeclaration, "field is nil")
		}
		path := f.path.String()
		if path == "" {
			return nil, errorf(ErrDeclaration, "field has no path")
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
		if f.implicit && f.typ != TypeString {
			return nil, errorf(ErrDeclaration, "field %q is %s, so it cannot be implicit", path, f.typ)
		}
		if _, ok := s.byPath[path]; ok {
			return nil, errorf(ErrDeclaration, "field %q is declared twice", path)
		}
		if f.unique {
			if s.uniqueKey != nil {
				return nil, errorf(
					ErrDeclaration,
					"fields %q and %q are both unique",
					s.uniqueKey.path.String(),
					path,
				)
			}
			s.uniqueKey = f
		}
		s.byPath[path] = f
		if f.implicit {
			s.implicit = append(s.implicit, f)
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

// Fields returns the declared fields in declaration order.
func (s *Schema) Fields() []*Field { return s.fields }

// UniqueField returns the field declared unique, or nil if there is none.
func (s *Schema) UniqueField() *Field { return s.uniqueKey }

// FilterableField returns the filterable field at path.
func (s *Schema) FilterableField(path aip132.FieldPath) (*Field, error) {
	if f, ok := s.byPath[path.String()]; ok && f.filterable {
		return f, nil
	}
	return nil, errorf(
		ErrInvalidFilter,
		"no filterable field %q, valid fields are %s",
		path.String(), s.fieldList((*Field).Filterable),
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
		path.String(), s.fieldList((*Field).Sortable),
	)
}

// SortableFields resolves each term of an order to the field it names, keeping
// the order's own indexing so a term's direction is read from it directly.
func (s *Schema) SortableFields(order []aip132.OrderBy) ([]*Field, error) {
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
