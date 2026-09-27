package flop

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
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
// [And], [Or], [Not] and [Cmp] values (not pointers).
// A nil root means match everything; nested operands must be non-nil.
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
// Value is a string, int64, float64, bool, time.Time or time.Duration matching
// the field's type, or nil for a null comparison. It is never user text: the
// schema has already coerced it. CompileSeek may also supply uint64 or []byte.
type Cmp struct {
	Field *Field
	Op    Op
	Value any
}

func (And) isExpr() {}
func (Or) isExpr()  {}
func (Not) isExpr() {}
func (Cmp) isExpr() {}

// ValidateExpr checks a compiled expression before an adapter renders it.
// It accepts nil roots and And, Or, Not and Cmp values. Malformed trees return
// ErrDeclaration, including empty groups, nil operands and unsupported nodes.
func ValidateExpr(expr Expr) error {
	if expr == nil {
		return nil
	}
	return validateExpr(expr)
}

func validateExpr(expr Expr) error {
	switch node := expr.(type) {
	case And:
		return validateOperands(node.Exprs)
	case Or:
		return validateOperands(node.Exprs)
	case Not:
		return validateExpr(node.Expr)
	case Cmp:
		if node.Field == nil || node.Field.Ref() == "" {
			return errorf(ErrDeclaration, "comparison has no field")
		}
		if node.Op < OpEq || node.Op > OpLike {
			return errorf(ErrDeclaration, "unsupported comparison operator %s", node.Op)
		}
		if node.Value == nil {
			if node.Op != OpEq && node.Op != OpNe {
				return errorf(ErrDeclaration, "%s cannot compare to null", node.Op)
			}
			return nil
		}
		if node.Op == OpLike {
			if _, ok := node.Value.(string); !ok {
				return errorf(ErrDeclaration, "LIKE needs a string pattern")
			}
		}
		switch value := node.Value.(type) {
		case bool, int64, uint64, string, []byte, time.Time, time.Duration:
			return nil
		case float64:
			if !math.IsNaN(value) && !math.IsInf(value, 0) {
				return nil
			}
		}
		return errorf(ErrDeclaration, "unsupported comparison value %T", node.Value)
	default:
		return errorf(ErrDeclaration, "unsupported expression node %T", expr)
	}
}

func validateOperands(exprs []Expr) error {
	if len(exprs) == 0 {
		return errorf(ErrDeclaration, "expression group is empty")
	}
	for _, expr := range exprs {
		if err := validateExpr(expr); err != nil {
			return err
		}
	}
	return nil
}

// comparators lists the operators each type accepts.
var comparators = map[fieldType]map[string]Op{
	fieldTypeString: {"=": OpEq, "!=": OpNe, ":": OpLike},
	fieldTypeBool:   {"=": OpEq, "!=": OpNe},
	fieldTypeInt:    {"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe},
	fieldTypeFloat:  {"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe},
	fieldTypeTime:   {"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe},
	fieldTypeDuration: {
		"=": OpEq, "!=": OpNe, "<": OpLt, "<=": OpLe, ">": OpGt, ">=": OpGe,
	},
}

var likeReplacer = strings.NewReplacer(
	`\`, `\\`,
	"%", `\%`,
	"_", `\_`,
	"*", "%",
)

// ParseFilter parses an AIP-160 filter and validates it against the schema.
func (s *Schema) ParseFilter(text string) (*Filter, error) {
	filter, err := ParseFilter(text)
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
func (s *Schema) ValidateFilter(filter *Filter) error {
	_, err := s.CompileFilter(filter)
	return err
}

// CompileFilter resolves a filter against the schema and coerces every argument to
// the Go value its field's type calls for.
//
// A nil or empty filter compiles to a nil Expr, meaning match everything.
func (s *Schema) CompileFilter(filter *Filter) (Expr, error) {
	if filter == nil || filter.expression == nil {
		return nil, nil
	}
	return s.compileExpression(filter.expression)
}

func (s *Schema) compileExpression(e *expression) (Expr, error) {
	// A sequence carries the same meaning as AND under exact-match semantics,
	// which is all a database can offer, so both levels flatten into one And.
	var exprs []Expr
	for _, seq := range e.Sequences {
		if len(seq.Factors) == 0 {
			return nil, errorf(ErrInvalidFilter, "sequence is empty")
		}
		for _, f := range seq.Factors {
			expr, err := s.compileFactor(f)
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

func (s *Schema) compileFactor(f factor) (Expr, error) {
	exprs := make([]Expr, 0, len(f.Terms))
	for _, t := range f.Terms {
		expr, err := s.compileTerm(t)
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

func (s *Schema) compileTerm(t term) (Expr, error) {
	if t.Simple.Composite != nil && t.Simple.Restriction != nil {
		return nil, errorf(ErrInvalidFilter, "term has both a restriction and a composite")
	}
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

func (s *Schema) compileRestriction(r *restriction) (Expr, error) {
	m := r.Member
	if err := validateMember(m); err != nil {
		return nil, err
	}
	if r.Comparator == "" {
		if r.Arg != nil {
			return nil, errorf(ErrInvalidFilter, "argument has no comparator")
		}
		return s.compileImplicit(m)
	}
	if r.Arg == nil {
		return nil, errorf(ErrInvalidFilter, "comparison has no argument")
	}
	if r.Arg.Composite != nil {
		return nil, errorf(
			ErrInvalidFilter,
			"field %q is compared against a parenthesized expression",
			m.Input(),
		)
	}

	path := memberPath(m)
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

	a := r.Arg.Member
	if err := validateMember(a); err != nil {
		return nil, err
	}
	// A bare null is the only literal that crosses every type, and only an
	// equality can ask about it.
	if !a.Quoted() && a.Path() == "null" {
		if op != OpEq && op != OpNe {
			return nil, errorf(
				ErrInvalidFilter,
				"field %q cannot be compared to null with %s",
				path.String(), r.Comparator,
			)
		}
		return Cmp{Field: field, Op: op, Value: nil}, nil
	}

	v, err := coerce(field, a)
	if err != nil {
		return nil, err
	}

	// A * in a string argument makes the restriction a pattern match, whichever
	// comparator asked for it. The has operator keeps searching anywhere in the
	// value when the client wrote no wildcard of its own.
	if text, ok := v.(string); ok && strings.Contains(text, "*") {
		pattern := Cmp{Field: field, Op: OpLike, Value: likeReplacer.Replace(text)}
		if op == OpNe {
			return Not{Expr: pattern}, nil
		}
		return pattern, nil
	}
	if op == OpLike {
		v = likePattern(v.(string))
	}
	return Cmp{Field: field, Op: op, Value: v}, nil
}

func validateMember(m member) error {
	values := append([]value{m.Value}, m.Fields...)
	for _, v := range values {
		if v.Value == "" && !v.Quoted {
			return errorf(ErrInvalidFilter, "member has an empty unquoted segment")
		}
	}
	return nil
}

// memberPath reads a member as a field path.
//
// The segments are taken one at a time rather than by splitting Member.Path,
// because a quoted segment may itself contain a dot.
func memberPath(m member) FieldPath {
	segments := make([]string, 0, len(m.Fields)+1)
	segments = append(segments, m.Value.Value)
	for _, field := range m.Fields {
		segments = append(segments, field.Value)
	}
	return NewFieldPath(segments...)
}

// compileImplicit expands a bare value into a search of every field declared
// implicit, which is what AIP-160 calls a global restriction.
func (s *Schema) compileImplicit(m member) (Expr, error) {
	if len(s.implicit) == 0 {
		return nil, errorf(ErrInvalidFilter, "no field is searched by the bare value %q", m.Input())
	}
	pattern := likePattern(m.Path())
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
func coerce(field *Field, a member) (any, error) {
	text := a.Path()
	invalid := func(want string) error {
		return errorf(
			ErrInvalidFilter,
			"field %q takes %s, and %s is not one",
			field.path.String(), want, a.Input(),
		)
	}

	switch field.typ {
	case fieldTypeString:
		return text, nil
	case fieldTypeInt:
		if a.Quoted() {
			return nil, invalid("an integer")
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, invalid("an integer")
		}
		return value, nil
	case fieldTypeFloat:
		if a.Quoted() {
			return nil, invalid("a number")
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, invalid("a number")
		}
		return value, nil
	case fieldTypeBool:
		if a.Quoted() {
			return nil, invalid("true or false")
		}
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, invalid("true or false")
	case fieldTypeTime:
		value, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, invalid("an RFC 3339 timestamp")
		}
		return value, nil
	case fieldTypeDuration:
		value, err := time.ParseDuration(text)
		if err != nil {
			return nil, invalid("a Go duration")
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
		return likeReplacer.Replace(text)
	}
	return "%" + likeReplacer.Replace(text) + "%"
}
