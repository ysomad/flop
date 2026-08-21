package flop

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
)

// Op is the comparison a [Cmp] makes.
type Op int

const (
	OpEq Op = iota + 1
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	// OpLike matches a value against a pattern whose only metacharacters are
	// % and _, escaped by a backslash. It is what the AIP-160 has operator
	// compiles to, and what a string argument holding the * wildcard compiles
	// to whichever comparator it was written with.
	//
	// A client writes * and flop renders it as %. A literal * cannot be
	// searched for: the parser unescapes a quoted argument before the compiler
	// reads it, so \* and * arrive the same.
	OpLike
)

func (o Op) String() string {
	switch o {
	case OpEq:
		return "="
	case OpNe:
		return "!="
	case OpLt:
		return "<"
	case OpLe:
		return "<="
	case OpGt:
		return ">"
	case OpGe:
		return ">="
	case OpLike:
		return "LIKE"
	}
	return fmt.Sprintf("Op(%d)", int(o))
}

// Expr is a node of a compiled filter. The tree a schema compiles holds only
// [And], [Or], [Not] and [Cmp], so a backend can switch over it exhaustively.
type Expr interface {
	isExpr()
}

// And matches when every operand matches.
type And struct{ Exprs []Expr }

// Or matches when any operand matches.
type Or struct{ Exprs []Expr }

// Not inverts its operand.
type Not struct{ Expr Expr }

// Cmp compares a field against a value.
//
// Value is a string, int64, float64, bool or time.Time matching the field's
// type, or nil for a null comparison. It is never user text: the schema has
// already coerced it.
type Cmp struct {
	Field *Field
	Op    Op
	Value any
}

func (And) isExpr() {}
func (Or) isExpr()  {}
func (Not) isExpr() {}
func (Cmp) isExpr() {}

// comparators lists the operators each type accepts.
var comparators = map[Type]map[string]Op{
	TypeString: {"=": OpEq, "!=": OpNe, ":": OpLike},
	TypeBool:   {"=": OpEq, "!=": OpNe},
	TypeInt:    {"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe},
	TypeFloat:  {"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe},
	TypeTime:   {"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe},
}

// ParseFilter parses an AIP-160 filter and validates it against the schema.
func (s *Schema) ParseFilter(text string) (*aip160.Filter, error) {
	filter, err := aip160.ParseFilter(text)
	if err != nil {
		return nil, errorf(ErrInvalidFilter, "%v", err)
	}
	if err := s.ValidateFilter(filter); err != nil {
		return nil, err
	}
	return filter, nil
}

// ValidateFilter reports whether every restriction names a filterable field,
// uses an operator that field accepts, and carries a value of its type.
func (s *Schema) ValidateFilter(filter *aip160.Filter) error {
	_, err := s.Compile(filter)
	return err
}

// Compile resolves a filter against the schema and coerces every argument to
// the Go value its field's type calls for.
//
// A nil or empty filter compiles to a nil Expr, meaning match everything.
func (s *Schema) Compile(filter *aip160.Filter) (Expr, error) {
	if filter == nil || filter.Expression == nil {
		return nil, nil
	}
	return s.compileExpression(filter.Expression)
}

func (s *Schema) compileExpression(e *aip160.Expression) (Expr, error) {
	// A sequence carries the same meaning as AND under exact-match semantics,
	// which is all a database can offer, so both levels flatten into one And.
	var exprs []Expr
	for _, sequence := range e.Sequences {
		for _, factor := range sequence.Factors {
			expr, err := s.compileFactor(factor)
			if err != nil {
				return nil, err
			}
			exprs = append(exprs, expr)
		}
	}
	// An And with no operands has no rendering a backend could give it, so an
	// expression holding nothing is refused here rather than passed on empty.
	if len(exprs) == 0 {
		return nil, errorf(ErrInvalidFilter, "expression is empty")
	}
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return And{Exprs: exprs}, nil
}

func (s *Schema) compileFactor(f *aip160.Factor) (Expr, error) {
	exprs := make([]Expr, 0, len(f.Terms))
	for _, term := range f.Terms {
		expr, err := s.compileTerm(term)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)
	}
	if len(exprs) == 0 {
		return nil, errorf(ErrInvalidFilter, "factor is empty")
	}
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return Or{Exprs: exprs}, nil
}

func (s *Schema) compileTerm(t *aip160.Term) (Expr, error) {
	var (
		expr Expr
		err  error
	)
	switch {
	case t.Simple.Composite != nil:
		expr, err = s.compileExpression(t.Simple.Composite)
	case t.Simple.Restriction != nil:
		expr, err = s.compileRestriction(t.Simple.Restriction)
	default:
		return nil, errorf(ErrInvalidFilter, "term is empty")
	}
	if err != nil {
		return nil, err
	}
	if t.Negated {
		return Not{Expr: expr}, nil
	}
	return expr, nil
}

func (s *Schema) compileRestriction(r *aip160.Restriction) (Expr, error) {
	member := r.Comparable.Member
	if r.Comparator == "" {
		return s.compileImplicit(member)
	}
	if r.Arg.Composite != nil {
		return nil, errorf(
			ErrInvalidFilter,
			"field %q is compared against a parenthesized expression",
			member.Input(),
		)
	}

	path := memberPath(member)
	field, err := s.FilterableField(path)
	if err != nil {
		return nil, err
	}
	op, ok := comparators[field.typ][r.Comparator]
	if !ok {
		return nil, errorf(
			ErrInvalidFilter,
			"field %q is %s, so it does not accept %s",
			path.String(), field.typ, r.Comparator,
		)
	}

	arg := r.Arg.Comparable.Member
	// A bare null is the only literal that crosses every type, and only an
	// equality can ask about it.
	if !arg.Quoted() && arg.Path() == "null" {
		if op != OpEq && op != OpNe {
			return nil, errorf(
				ErrInvalidFilter,
				"field %q cannot be compared to null with %s",
				path.String(), r.Comparator,
			)
		}
		return Cmp{Field: field, Op: op, Value: nil}, nil
	}

	value, err := coerce(field, arg)
	if err != nil {
		return nil, err
	}

	// A * in a string argument makes the restriction a pattern match, whichever
	// comparator asked for it. The has operator keeps searching anywhere in the
	// value when the client wrote no wildcard of its own.
	if text, ok := value.(string); ok && strings.Contains(text, "*") {
		pattern := Cmp{Field: field, Op: OpLike, Value: quoteLike(text)}
		if op == OpNe {
			return Not{Expr: pattern}, nil
		}
		return pattern, nil
	}
	if op == OpLike {
		value = likePattern(value.(string))
	}
	return Cmp{Field: field, Op: op, Value: value}, nil
}

// memberPath reads a member as a field path.
//
// The segments are taken one at a time rather than by splitting Member.Path,
// because a quoted segment may itself contain a dot.
func memberPath(m *aip160.Member) aip132.FieldPath {
	if m.Value == nil {
		return aip132.FieldPath{}
	}
	segments := make([]string, 0, len(m.Fields)+1)
	segments = append(segments, m.Value.Value)
	for _, field := range m.Fields {
		segments = append(segments, field.Value)
	}
	return aip132.NewFieldPath(segments...)
}

// compileImplicit expands a bare value into a search of every field declared
// implicit, which is what AIP-160 calls a global restriction.
func (s *Schema) compileImplicit(member *aip160.Member) (Expr, error) {
	if len(s.implicit) == 0 {
		return nil, errorf(ErrInvalidFilter, "no field is searched by the bare value %q", member.Input())
	}
	pattern := likePattern(member.Path())
	exprs := make([]Expr, 0, len(s.implicit))
	for _, field := range s.implicit {
		exprs = append(exprs, Cmp{Field: field, Op: OpLike, Value: pattern})
	}
	if len(exprs) == 1 {
		return exprs[0], nil
	}
	return Or{Exprs: exprs}, nil
}

// coerce reads a filter argument as the Go value the field's type calls for.
//
// Numbers and booleans are only recognised unquoted, following AIP-160: the
// identifiers true and null carry meaning only against a field that is typed
// to receive them, so quoting one asks for the text. A timestamp is read either
// way, because the colons of RFC 3339 lex as comparators unless it is quoted.
func coerce(field *Field, arg *aip160.Member) (any, error) {
	text := arg.Path()
	invalid := func(want string) error {
		return errorf(
			ErrInvalidFilter,
			"field %q takes %s, and %s is not one",
			field.path.String(), want, arg.Input(),
		)
	}

	switch field.typ {
	case TypeString:
		return text, nil
	case TypeInt:
		if arg.Quoted() {
			return nil, invalid("an integer")
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, invalid("an integer")
		}
		return value, nil
	case TypeFloat:
		if arg.Quoted() {
			return nil, invalid("a number")
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, invalid("a number")
		}
		return value, nil
	case TypeBool:
		if arg.Quoted() {
			return nil, invalid("true or false")
		}
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, invalid("true or false")
	case TypeTime:
		value, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, invalid("an RFC 3339 timestamp")
		}
		return value, nil
	}
	return nil, errorf(ErrDeclaration, "field %q has no type", field.path.String())
}

// likePattern renders a value the has operator or an implicit search compares
// against: a client's own wildcards stand as they are, and a value holding none
// is searched for anywhere in the column.
func likePattern(text string) string {
	if strings.Contains(text, "*") {
		return quoteLike(text)
	}
	return "%" + quoteLike(text) + "%"
}

// quoteLike renders a client's value as a LIKE pattern: the metacharacters of
// the pattern language are escaped, so that a value such as test_name matches
// literally rather than also matching test3name, and the * that AIP-160 gives a
// client becomes the % that matches any run of characters.
func quoteLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	value = strings.ReplaceAll(value, "_", `\_`)
	return strings.ReplaceAll(value, "*", "%")
}
