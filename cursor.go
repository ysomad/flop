package flop

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash"
	"math"
	"slices"
	"time"

	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
)

const cursorVersion = 1

// valueKind tags a cursor position value on the wire, so a cursor decodes
// without consulting the schema that issued it.
type valueKind byte

// The kinds start at one, so that the zero byte a truncated token reads as is
// never a valid kind.
const (
	boolKind valueKind = iota + 1
	int64Kind
	uint64Kind
	float64Kind
	stringKind
	bytesKind
	timeKind
	durationKind
)

// CursorValue is one ordering field of a row and the value it held there.
type CursorValue struct {
	// FieldPath is the field the value belongs to, matching the order the
	// cursor was issued under.
	FieldPath aip132.FieldPath
	// Value is the field's value. It must be a bool, int64, uint64, float64,
	// string, []byte, time.Time or time.Duration.
	Value any
}

// CursorPosition addresses the row a page continues after. It holds one value
// per ordering field, in order.
type CursorPosition []CursorValue

// seekableFields resolves an order a cursor may page under: every term has to
// name a sortable field, and one of them has to be unique, or a page repeats
// or drops rows.
func (s *Schema) seekableFields(order []aip132.OrderBy) ([]*Field, error) {
	fields, err := s.SortableFields(order)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errorf(ErrDeclaration, "seeking needs an order")
	}
	if !slices.ContainsFunc(fields, (*Field).Unique) {
		return nil, errorf(
			ErrDeclaration,
			"seeking needs a unique field in the order, and %q has none",
			aip132.OrderByString(order),
		)
	}
	return fields, nil
}

// CompileSeek compiles the row comparison that continues a page after pos into
// the same [Expr] tree a filter compiles to, so a backend renders seeking with
// the code it already renders filtering with.
//
// A first page has no position and compiles to a nil Expr. The comparison is
// written out term by term rather than as a row value, so an order mixing
// ascending and descending fields still seeks correctly. Every ordering column
// must be NOT NULL: SQL comparisons against NULL are unknown, which would
// silently drop rows from the page.
func (s *Schema) CompileSeek(order []aip132.OrderBy, pos CursorPosition) (Expr, error) {
	fields, err := s.seekableFields(order)
	if err != nil {
		return nil, err
	}
	if len(pos) == 0 {
		return nil, nil
	}
	if len(pos) != len(fields) {
		return nil, errorf(
			ErrCursorMismatch,
			"cursor addresses %d fields but the order has %d", len(pos), len(fields),
		)
	}

	clauses := make([]Expr, 0, len(fields))
	for i, field := range fields {
		terms := make([]Expr, 0, i+1)
		for j, earlier := range fields[:i] {
			value, err := seekValue(pos[j], earlier)
			if err != nil {
				return nil, err
			}
			terms = append(terms, Cmp{Field: earlier, Op: OpEq, Value: value})
		}
		value, err := seekValue(pos[i], field)
		if err != nil {
			return nil, err
		}
		op := OpGt
		if order[i].Descending {
			op = OpLt
		}
		terms = append(terms, Cmp{Field: field, Op: op, Value: value})
		if len(terms) == 1 {
			clauses = append(clauses, terms[0])
			continue
		}
		clauses = append(clauses, And{Exprs: terms})
	}
	if len(clauses) == 1 {
		return clauses[0], nil
	}
	return Or{Exprs: clauses}, nil
}

// seekValue reads the value the cursor holds for field. The position is issued
// under the order it seeks with, so the two are paired by index and only have
// to agree on the field they name.
func seekValue(value CursorValue, field *Field) (any, error) {
	if !value.FieldPath.Equals(field.Path()) {
		return nil, errorf(
			ErrCursorMismatch,
			"cursor addresses %q where the order has %q", value.FieldPath, field.Path(),
		)
	}
	if value.Value == nil {
		return nil, errorf(ErrInvalidCursor, "ordering field %q is null", field.Path())
	}
	return value.Value, nil
}

// DecodeCursor reads a page token back into the position it addresses.
//
// order and filter are the ones the request arrived with. A token only decodes
// under the pair it was issued for, so a client cannot carry a position over to
// a different filter. An empty token is the first page: it yields a nil
// position and no error.
//
// The order only has to name sortable fields here. That it also has to be
// total is [Schema.CompileSeek]'s rule, because that is what needs it.
func (s *Schema) DecodeCursor(
	token string,
	order []aip132.OrderBy,
	filter *aip160.Filter,
) (CursorPosition, error) {
	if _, err := s.SortableFields(order); err != nil {
		return nil, err
	}
	if token == "" {
		return nil, nil
	}
	return decodeCursor(token, newCursorBinding(order, filter), order)
}

// EncodeCursor renders the last row of a page as the token the next page
// continues from, bound to the order and filter the page was drawn under.
//
// Every field the order names has to declare a [FieldBuilder.Value], which is
// what reads the row. A row of another type than the one those accessors were
// declared for is a declaration mistake, not something a request can cause.
func (s *Schema) EncodeCursor(
	row any,
	order []aip132.OrderBy,
	filter *aip160.Filter,
) (string, error) {
	fields, err := s.SortableFields(order)
	if err != nil {
		return "", err
	}
	if len(fields) == 0 {
		return "", errorf(ErrDeclaration, "a cursor position needs an order")
	}

	pos := make(CursorPosition, 0, len(fields))

	for _, field := range fields {
		if field.value == nil {
			return "", errorf(
				ErrDeclaration,
				"no cursor value for the ordering field %q", field.path.String(),
			)
		}
		value, ok := field.value(row)
		if !ok {
			return "", errorf(
				ErrDeclaration,
				"cursor value for the ordering field %q does not read a %T",
				field.path.String(), row,
			)
		}
		pos = append(pos, CursorValue{FieldPath: field.path, Value: value})
	}

	return encodeCursor(pos, order, newCursorBinding(order, filter))
}

// CursorPage is a page of rows and the token the page after it continues from.
// NextCursor is empty on the last page.
type CursorPage[T any] struct {
	Items      []T
	NextCursor string
}

// CursorPage assembles the page a cursor query returned: it splits the surplus
// row off and mints the token that continues after the last row of the page.
//
// A query asks for pageSize+1 rows, so that the surplus row is there to report
// whether another page follows.
func (s *Schema) CursorPage[T any](
	rows []T,
	pageSize int32,
	order []aip132.OrderBy,
	filter *aip160.Filter,
) (CursorPage[T], error) {
	items, hasNext := trim(rows, pageSize)
	page := CursorPage[T]{Items: items}
	if !hasNext {
		return page, nil
	}

	next, err := s.EncodeCursor(items[len(items)-1], order, filter)
	if err != nil {
		return CursorPage[T]{}, err
	}

	page.NextCursor = next

	return page, nil
}

// trim splits a page from the surplus row that reports whether another page
// follows.
func trim[T any](rows []T, pageSize int32) ([]T, bool) {
	if pageSize <= 0 || int64(len(rows)) <= int64(pageSize) {
		return rows, false
	}
	return rows[:pageSize], true
}

// cursorBinding fingerprints the request a cursor was issued for. The filter
// renders from its tree rather than from the text the client sent, so respacing
// a filter between pages does not invalidate the cursor.
type cursorBinding [sha256.Size]byte

func newCursorBinding(o []aip132.OrderBy, f *aip160.Filter) cursorBinding {
	hash := sha256.New()
	writeHashString(hash, aip132.OrderByString(o))
	writeHashString(hash, f.String())
	var binding cursorBinding
	copy(binding[:], hash.Sum(nil))
	return binding
}

// writeHashString writes s length-prefixed, so that two fields cannot hash the
// same way by shifting a character across the boundary between them.
func writeHashString(h hash.Hash, s string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(s)))
	h.Write(length[:])
	h.Write([]byte(s))
}

// encodeCursor renders a position and its binding as an opaque page token.
//
// The plaintext is a version byte, the binding, and then one kind-tagged value
// per ordering field. The field paths stay off the wire: the binding pins the
// order the cursor was issued under, so decoding reads them back from it.
func encodeCursor(
	position CursorPosition,
	order []aip132.OrderBy,
	binding cursorBinding,
) (string, error) {
	if len(order) == 0 {
		return "", errorf(ErrDeclaration, "a cursor needs an order")
	}
	if len(position) != len(order) {
		return "", errorf(ErrDeclaration,
			"cursor position addresses %d fields but the order has %d", len(position), len(order))
	}

	plaintext := make([]byte, 0, 64)
	plaintext = append(plaintext, cursorVersion)
	plaintext = append(plaintext, binding[:]...)

	for i, value := range position {
		if !value.FieldPath.Equals(order[i].FieldPath) {
			return "", errorf(ErrDeclaration, "cursor position names %q where the order has %q",
				value.FieldPath, order[i].FieldPath)
		}

		var err error
		plaintext, err = appendCursorValue(plaintext, value)
		if err != nil {
			return "", err
		}
	}
	return base64.RawURLEncoding.EncodeToString(plaintext), nil
}

func appendCursorValue(dst []byte, value CursorValue) ([]byte, error) {
	switch v := value.Value.(type) {
	case bool:
		bit := byte(0)
		if v {
			bit = 1
		}
		return append(dst, byte(boolKind), bit), nil
	case int64:
		return appendFixed64(dst, int64Kind, uint64(v)), nil
	case uint64:
		return appendFixed64(dst, uint64Kind, v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errorf(ErrInvalidCursor,
				"cursor position %q does not order rows: %v", value.FieldPath, v)
		}
		return appendFixed64(dst, float64Kind, math.Float64bits(v)), nil
	case string:
		return appendVarbytes(dst, stringKind, []byte(v)), nil
	case []byte:
		return appendVarbytes(dst, bytesKind, v), nil
	case time.Time:
		if v.IsZero() {
			return nil, errorf(ErrInvalidCursor, "cursor position %q is the zero time", value.FieldPath)
		}
		encoded, err := v.MarshalBinary()
		if err != nil {
			return nil, errorf(ErrInvalidCursor, "cursor position %q: %v", value.FieldPath, err)
		}
		return appendVarbytes(dst, timeKind, encoded), nil
	case time.Duration:
		return appendFixed64(dst, durationKind, uint64(v)), nil
	case nil:
		return nil, errorf(ErrInvalidCursor,
			"cursor position %q is null, so it cannot address a row", value.FieldPath)
	default:
		return nil, errorf(ErrInvalidCursor,
			"cursor position %q has unsupported type %T", value.FieldPath, value.Value)
	}
}

func appendFixed64(dst []byte, kind valueKind, bits uint64) []byte {
	return binary.BigEndian.AppendUint64(append(dst, byte(kind)), bits)
}

func appendVarbytes(dst []byte, kind valueKind, raw []byte) []byte {
	dst = binary.AppendUvarint(append(dst, byte(kind)), uint64(len(raw)))
	return append(dst, raw...)
}

// decodeCursor reads a page token back into the position it was issued with,
// naming each value after the ordering field it belongs to.
func decodeCursor(
	cursor string,
	binding cursorBinding,
	order []aip132.OrderBy,
) (CursorPosition, error) {
	plaintext, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, errorf(ErrInvalidCursor, "not base64: %v", err)
	}

	r := cursorReader{buf: plaintext}
	if version := r.next(1); r.err == nil && version[0] != cursorVersion {
		return nil, errorf(ErrInvalidCursor, "unsupported version %d", version[0])
	}
	if got := r.next(sha256.Size); r.err == nil && cursorBinding(got) != binding {
		return nil, errorf(ErrCursorMismatch, "issued for a different order or filter")
	}

	position := make(CursorPosition, 0, len(order))
	for _, term := range order {
		path := term.FieldPath.String()
		position = append(position, CursorValue{FieldPath: term.FieldPath, Value: r.value(path)})
	}
	if r.err != nil {
		return nil, r.err
	}
	if len(position) == 0 {
		return nil, errorf(ErrInvalidCursor, "addresses no position")
	}
	if len(r.buf) != 0 {
		return nil, errorf(ErrInvalidCursor, "has %d trailing bytes", len(r.buf))
	}
	return position, nil
}

// cursorReader reads a decoded cursor, keeping the first error it hits so that
// no single read has to carry a length check of its own. A value it returns is
// only meaningful while err is nil.
type cursorReader struct {
	buf []byte
	err error
}

func (r *cursorReader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = errorf(ErrInvalidCursor, format, args...)
	}
}

// next takes the leading n bytes.
func (r *cursorReader) next(n int) []byte {
	if r.err != nil {
		return nil
	}
	if len(r.buf) < n {
		r.fail("is truncated")
		return nil
	}
	head := r.buf[:n]
	r.buf = r.buf[n:]
	return head
}

func (r *cursorReader) uint64() uint64 {
	raw := r.next(8)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(raw)
}

// varbytes takes a uvarint length and the payload that follows it.
func (r *cursorReader) varbytes() []byte {
	if r.err != nil {
		return nil
	}
	length, read := binary.Uvarint(r.buf)
	if read <= 0 || length > uint64(len(r.buf)-read) {
		r.fail("has an unreadable length")
		return nil
	}
	r.buf = r.buf[read:]
	return r.next(int(length))
}

// value reads one kind-tagged value. path only names the field in errors.
func (r *cursorReader) value(path string) any {
	tag := r.next(1)
	if r.err != nil {
		return nil
	}
	switch kind := valueKind(tag[0]); kind {
	case boolKind:
		raw := r.next(1)
		if r.err != nil {
			return nil
		}
		if raw[0] > 1 {
			r.fail("position %q is not a bool", path)
			return nil
		}
		return raw[0] == 1
	case int64Kind:
		return int64(r.uint64())
	case uint64Kind:
		return r.uint64()
	case float64Kind:
		value := math.Float64frombits(r.uint64())
		if r.err == nil && (math.IsNaN(value) || math.IsInf(value, 0)) {
			r.fail("position %q does not order rows: %v", path, value)
			return nil
		}
		return value
	case stringKind:
		return string(r.varbytes())
	case bytesKind:
		return r.varbytes()
	case timeKind:
		raw := r.varbytes()
		if r.err != nil {
			return nil
		}
		var value time.Time
		if err := value.UnmarshalBinary(raw); err != nil {
			r.fail("position %q is not a time: %v", path, err)
			return nil
		}
		return value
	case durationKind:
		return time.Duration(r.uint64())
	default:
		r.fail("position %q has unknown value kind %d", path, kind)
		return nil
	}
}
