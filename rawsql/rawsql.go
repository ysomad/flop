// Package rawsql renders a flop schema, filter and order as SQL text and named
// arguments, for callers assembling a query by hand.
//
// Every fragment it returns omits its keyword and is parenthesized, so it drops
// into a query without needing to know what surrounds it. Identifiers come only
// from the columns a schema declares and values only from bound arguments, so
// nothing a client sends reaches the SQL text.
//
// Cursor pagination selects pageSize+1 rows: the surplus row is what
// flop.Trim reads a next page from.
//
// Arguments bind by name, written "@name" and returned as a map. Pass it to a
// driver that reads names, such as pgx.NamedArgs(args), or hand each entry to
// sql.Named. Markers are named after the field they read, so a fragment reads
// the same wherever it lands in the query.
package rawsql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ysomad/flop"
	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/aip160"
)

// likeEscape is the escape character every LIKE pattern flop compiles uses.
const likeEscape = `ESCAPE '\'`

// Builder renders fragments of one query, keeping their argument names unique
// across all of them and collecting the arguments they bind.
//
// Use it whenever a query takes more than one fragment. The package-level
// functions render a single fragment and count from one, so combining their
// output would collide on names and bind the wrong values.
type Builder struct {
	args map[string]any
}

// NewBuilder returns a builder with no arguments bound.
func NewBuilder() *Builder {
	return &Builder{args: make(map[string]any)}
}

// Args returns the named arguments of every fragment rendered so far.
func (b *Builder) Args() map[string]any { return b.args }

// bind records an argument under a name derived from the field it compares and
// returns the marker that reads it back.
func (b *Builder) bind(field *flop.Field, value any) string {
	name := paramName(field, len(b.args)+1)
	b.args[name] = value
	return "@" + name
}

// paramName names the n-th argument after the field it binds, so the rendered
// SQL says what it reads. Anything a path holds that an identifier may not
// becomes an underscore, and the ordinal keeps repeats of one field apart.
func paramName(field *flop.Field, n int) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, strings.Join(field.Path().GetSegments(), "_"))
	return name + "_" + strconv.Itoa(n)
}

// Where renders a filter as a boolean expression, without the WHERE keyword.
// An empty filter renders as "".
func (b *Builder) Where(s *flop.Schema, f *aip160.Filter) (string, error) {
	expr, err := s.Compile(f)
	if err != nil {
		return "", err
	}
	return b.WhereExpr(expr)
}

// WhereExpr renders an already compiled filter. A nil expression renders as "".
func (b *Builder) WhereExpr(e flop.Expr) (string, error) {
	if e == nil {
		return "", nil
	}
	switch node := e.(type) {
	case flop.And:
		return b.join(node.Exprs, " AND ")
	case flop.Or:
		return b.join(node.Exprs, " OR ")
	case flop.Not:
		inner, err := b.WhereExpr(node.Expr)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(NOT %s)", inner), nil
	case flop.Cmp:
		return b.cmp(node)
	}
	return "", fmt.Errorf("rawsql: unknown filter node %T", e)
}

func (b *Builder) join(exprs []flop.Expr, sep string) (string, error) {
	parts := make([]string, 0, len(exprs))
	for _, expr := range exprs {
		part, err := b.WhereExpr(expr)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return fmt.Sprintf("(%s)", strings.Join(parts, sep)), nil
}

func (b *Builder) cmp(c flop.Cmp) (string, error) {
	column := c.Field.Column()
	if c.Value == nil {
		switch c.Op {
		case flop.OpEq:
			return fmt.Sprintf("(%s IS NULL)", column), nil
		case flop.OpNe:
			return fmt.Sprintf("(%s IS NOT NULL)", column), nil
		}
		return "", fmt.Errorf("rawsql: %s cannot compare %s to null", c.Op, column)
	}
	if c.Op == flop.OpLike {
		return fmt.Sprintf("(%s LIKE %s %s)", column, b.bind(c.Field, c.Value), likeEscape), nil
	}
	return fmt.Sprintf("(%s %s %s)", column, c.Op.String(), b.bind(c.Field, c.Value)), nil
}

// Seek renders the row comparison that continues a page after pos, without the
// WHERE keyword. A first page has no position and renders as "".
func (b *Builder) Seek(
	s *flop.Schema,
	order []aip132.OrderBy,
	pos flop.CursorPosition,
) (string, error) {
	expr, err := s.CompileSeek(order, pos)
	if err != nil {
		return "", err
	}
	return b.WhereExpr(expr)
}

// Where renders a filter as a boolean expression, without the WHERE keyword.
// An empty filter renders as "" with no arguments.
func Where(s *flop.Schema, f *aip160.Filter) (string, map[string]any, error) {
	b := NewBuilder()
	clause, err := b.Where(s, f)
	if err != nil {
		return "", nil, err
	}
	return clause, b.Args(), nil
}

// WhereExpr renders an already compiled filter, for a caller that compiles once
// and builds many queries.
func WhereExpr(e flop.Expr) (string, map[string]any, error) {
	b := NewBuilder()
	clause, err := b.WhereExpr(e)
	if err != nil {
		return "", nil, err
	}
	return clause, b.Args(), nil
}

// Seek renders the row comparison that continues a page after pos, without the
// WHERE keyword.
func Seek(
	s *flop.Schema,
	order []aip132.OrderBy,
	pos flop.CursorPosition,
) (string, map[string]any, error) {
	b := NewBuilder()
	clause, err := b.Seek(s, order, pos)
	if err != nil {
		return "", nil, err
	}
	return clause, b.Args(), nil
}

// OrderBy renders an order as a column list, without the ORDER BY keyword.
// An empty order renders as "".
func OrderBy(s *flop.Schema, order []aip132.OrderBy) (string, error) {
	if len(order) == 0 {
		return "", nil
	}
	fields, err := s.SortableFields(order)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(fields))
	for i, field := range fields {
		part := field.Column()
		if order[i].Descending {
			part += " DESC"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", "), nil
}
