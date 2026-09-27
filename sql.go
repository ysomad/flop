package flop

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
)

// likeEscape is the escape character every LIKE pattern flop compiles uses.
const likeEscape = `ESCAPE '\'`

// SQLBuilder renders fragments of one query, keeping their argument names unique
// across all of them and collecting the arguments they bind.
//
// Use it whenever a query takes more than one fragment. The package-level
// functions render a single fragment and count from one, so combining their
// output would collide on names and bind the wrong values.
type SQLBuilder struct {
	args    map[string]any
	nextArg int
}

// NewSQLBuilder returns a builder with no arguments bound.
func NewSQLBuilder() *SQLBuilder {
	return &SQLBuilder{args: make(map[string]any)}
}

// Args returns a copy of the named arguments rendered so far.
func (b *SQLBuilder) Args() map[string]any { return maps.Clone(b.args) }

// bind records an argument under a name derived from the field it compares and
// returns the marker that reads it back.
func (b *SQLBuilder) bind(field *Field, value any) string {
	if b.args == nil {
		b.args = make(map[string]any)
	}
	b.nextArg++
	name := paramName(field, b.nextArg)
	b.args[name] = value
	return "@" + name
}

// paramName names the n-th argument after the field it binds, so the rendered
// SQL says what it reads. Anything a path holds that an identifier may not
// becomes an underscore, and the ordinal keeps repeats of one field apart.
func paramName(field *Field, n int) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, strings.Join(field.Path().Segments(), "_"))
	return name + "_" + strconv.Itoa(n)
}

// Where renders a filter as a boolean expression, without the WHERE keyword.
// An empty filter renders as "".
func (b *SQLBuilder) Where(s *Schema, f *Filter) (string, error) {
	expr, err := s.CompileFilter(f)
	if err != nil {
		return "", err
	}
	return b.WhereExpr(expr)
}

// WhereExpr renders an already compiled filter. A nil expression renders as "".
// Malformed expressions return ErrDeclaration without binding arguments.
func (b *SQLBuilder) WhereExpr(e Expr) (string, error) {
	if err := ValidateExpr(e); err != nil {
		return "", err
	}
	if e == nil {
		return "", nil
	}
	var sql strings.Builder
	if err := b.writeExpr(&sql, e); err != nil {
		return "", err
	}
	return sql.String(), nil
}

func (b *SQLBuilder) writeExpr(sql *strings.Builder, e Expr) error {
	switch node := e.(type) {
	case And:
		return b.writeJoin(sql, node.Exprs, " AND ")
	case Or:
		return b.writeJoin(sql, node.Exprs, " OR ")
	case Not:
		sql.WriteString("(NOT ")
		if err := b.writeExpr(sql, node.Expr); err != nil {
			return err
		}
		sql.WriteByte(')')
		return nil
	case Cmp:
		return b.writeCmp(sql, node)
	}
	return fmt.Errorf("flop: unknown filter node %T", e)
}

func (b *SQLBuilder) writeJoin(sql *strings.Builder, exprs []Expr, sep string) error {
	parenthesized := len(exprs) != 1
	if parenthesized {
		sql.WriteByte('(')
	}
	for i, expr := range exprs {
		if i > 0 {
			sql.WriteString(sep)
		}
		if err := b.writeExpr(sql, expr); err != nil {
			return err
		}
	}
	if parenthesized {
		sql.WriteByte(')')
	}
	return nil
}

func (b *SQLBuilder) writeCmp(sql *strings.Builder, c Cmp) error {
	column := c.Field.Ref()
	if c.Value == nil {
		switch c.Op {
		case OpEq:
			sql.WriteByte('(')
			sql.WriteString(column)
			sql.WriteString(" IS NULL)")
			return nil
		case OpNe:
			sql.WriteByte('(')
			sql.WriteString(column)
			sql.WriteString(" IS NOT NULL)")
			return nil
		}
		return fmt.Errorf("flop: %s cannot compare %s to null", c.Op, column)
	}

	sql.WriteByte('(')
	sql.WriteString(column)
	sql.WriteByte(' ')
	if c.Op == OpLike {
		sql.WriteString("LIKE ")
		sql.WriteString(b.bind(c.Field, c.Value))
		sql.WriteByte(' ')
		sql.WriteString(likeEscape)
	} else {
		sql.WriteString(c.Op.String())
		sql.WriteByte(' ')
		sql.WriteString(b.bind(c.Field, c.Value))
	}
	sql.WriteByte(')')
	return nil
}

// Seek renders the row comparison that continues a page after pos, without the
// WHERE keyword. A first page has no position and renders as "".
func (b *SQLBuilder) Seek(
	s *Schema,
	order []OrderBy,
	pos CursorPosition,
) (string, error) {
	expr, err := s.CompileSeek(order, pos)
	if err != nil {
		return "", err
	}
	return b.WhereExpr(expr)
}

// WhereSQL renders a filter as a boolean expression, without the WHERE keyword.
// An empty filter renders as "" with no arguments.
func WhereSQL(s *Schema, f *Filter) (string, map[string]any, error) {
	b := NewSQLBuilder()
	clause, err := b.Where(s, f)
	if err != nil {
		return "", nil, err
	}
	return clause, b.Args(), nil
}

// WhereExprSQL renders an already compiled filter, for a caller that compiles once
// and builds many queries.
func WhereExprSQL(e Expr) (string, map[string]any, error) {
	b := NewSQLBuilder()
	clause, err := b.WhereExpr(e)
	if err != nil {
		return "", nil, err
	}
	return clause, b.Args(), nil
}

// SeekSQL renders the row comparison that continues a page after pos, without the
// WHERE keyword.
func SeekSQL(
	s *Schema,
	order []OrderBy,
	pos CursorPosition,
) (string, map[string]any, error) {
	b := NewSQLBuilder()
	clause, err := b.Seek(s, order, pos)
	if err != nil {
		return "", nil, err
	}
	return clause, b.Args(), nil
}

// OrderBySQL renders an order as a column list, without the ORDER BY keyword.
// An empty order renders as "".
func OrderBySQL(s *Schema, order []OrderBy) (string, error) {
	if len(order) == 0 {
		return "", nil
	}
	fields, err := s.SortableFields(order)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(fields))
	for i, field := range fields {
		part := field.Ref()
		if order[i].Descending {
			part += " DESC"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", "), nil
}
