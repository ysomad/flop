// Package flopsq applies a flop schema, filter, order and pagination to a
// squirrel query builder.
//
// Fragments are built as squirrel expressions rather than as SQL text, so the
// builder's own PlaceholderFormat still rewrites the bind markers. That is why
// nothing here takes a dialect.
package flopsq

import (
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/ysomad/flop"
)

// likeEscape is the escape character every LIKE pattern flop compiles uses.
const likeEscape = ` ESCAPE '\'`

// Where builds the condition for a filter. An empty filter returns a nil
// Sqlizer, which squirrel's Where ignores.
func Where(s *flop.Schema, f *flop.Filter) (sq.Sqlizer, error) {
	expr, err := s.CompileFilter(f)
	if err != nil {
		return nil, err
	}
	return WhereExpr(expr)
}

// WhereExpr builds the condition for a compiled filter. A nil root is valid;
// malformed expressions return flop.ErrDeclaration before a Sqlizer is returned.
func WhereExpr(e flop.Expr) (sq.Sqlizer, error) {
	if err := flop.ValidateExpr(e); err != nil {
		return nil, err
	}
	return whereExpr(e)
}

func whereExpr(e flop.Expr) (sq.Sqlizer, error) {
	if e == nil {
		return nil, nil
	}
	switch node := e.(type) {
	case flop.And:
		parts, err := conditions(node.Exprs)
		if err != nil {
			return nil, err
		}
		return sq.And(parts), nil
	case flop.Or:
		parts, err := conditions(node.Exprs)
		if err != nil {
			return nil, err
		}
		return sq.Or(parts), nil
	case flop.Not:
		inner, err := whereExpr(node.Expr)
		if err != nil {
			return nil, err
		}
		return not{inner}, nil
	case flop.Cmp:
		return condition(node)
	}
	return nil, fmt.Errorf("flopsq: unknown filter node %T", e)
}

func conditions(exprs []flop.Expr) ([]sq.Sqlizer, error) {
	parts := make([]sq.Sqlizer, 0, len(exprs))
	for _, expr := range exprs {
		part, err := whereExpr(expr)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func condition(c flop.Cmp) (sq.Sqlizer, error) {
	column := c.Field.Ref()
	switch c.Op {
	case flop.OpEq:
		return sq.Eq{column: c.Value}, nil
	case flop.OpNe:
		return sq.NotEq{column: c.Value}, nil
	case flop.OpLt:
		return sq.Lt{column: c.Value}, nil
	case flop.OpLe:
		return sq.LtOrEq{column: c.Value}, nil
	case flop.OpGt:
		return sq.Gt{column: c.Value}, nil
	case flop.OpGe:
		return sq.GtOrEq{column: c.Value}, nil
	case flop.OpLike:
		// squirrel's Like writes no ESCAPE clause, so the pattern flop escaped
		// would otherwise read its backslashes as ordinary characters.
		return sq.Expr(column+" LIKE ?"+likeEscape, c.Value), nil
	}
	return nil, fmt.Errorf("flopsq: unknown operator %s on %s", c.Op, column)
}

// not negates a condition. squirrel has no NOT of its own.
type not struct{ inner sq.Sqlizer }

func (n not) ToSql() (string, []any, error) {
	clause, args, err := n.inner.ToSql()
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("NOT (%s)", clause), args, nil
}

// OrderBy renders an order as the column list squirrel's OrderBy takes.
func OrderBy(s *flop.Schema, order []flop.OrderBy) ([]string, error) {
	if len(order) == 0 {
		return nil, nil
	}
	fields, err := s.SortableFields(order)
	if err != nil {
		return nil, err
	}
	parts := make([]string, 0, len(fields))
	for i, field := range fields {
		part := field.Ref()
		if order[i].Descending {
			part += " DESC"
		}
		parts = append(parts, part)
	}
	return parts, nil
}

// Query adds the filter and order to a query, and no pagination. Either may be
// empty. A count query alongside a page uses it, so rows and totals agree on
// which records the page is drawn from.
func Query(
	b sq.SelectBuilder,
	s *flop.Schema,
	order []flop.OrderBy,
	f *flop.Filter,
) (sq.SelectBuilder, error) {
	where, err := Where(s, f)
	if err != nil {
		return b, err
	}
	if where != nil {
		b = b.Where(where)
	}
	columns, err := OrderBy(s, order)
	if err != nil {
		return b, err
	}
	if len(columns) > 0 {
		b = b.OrderBy(columns...)
	}
	return b, nil
}

// OffsetQuery adds the filter, order, limit and offset of page-number
// pagination. A zero page number selects the first page.
func OffsetQuery(
	b sq.SelectBuilder,
	s *flop.Schema,
	order []flop.OrderBy,
	f *flop.Filter,
	page, pageSize int32,
) (sq.SelectBuilder, error) {
	offset, err := flop.Offset(page, pageSize)
	if err != nil {
		return b, err
	}
	b, err = Query(b, s, order, f)
	if err != nil {
		return b, err
	}
	return b.Limit(uint64(pageSize)).Offset(uint64(offset)), nil
}

// CursorQuery adds the filter, order, seek condition and limit of cursor
// pagination. after is empty on the first page.
//
// The limit is pageSize+1, including the surplus row [flop.Schema.NewCursorPage]
// uses to detect a next page. skip offsets from the cursor position.
func CursorQuery(
	b sq.SelectBuilder,
	s *flop.Schema,
	order []flop.OrderBy,
	f *flop.Filter,
	after flop.CursorPosition,
	pageSize, skip int32,
) (sq.SelectBuilder, error) {
	if pageSize <= 0 {
		return b, fmt.Errorf("%w: must be positive", flop.ErrInvalidPageSize)
	}
	if skip < 0 {
		return b, fmt.Errorf("%w: must not be negative", flop.ErrInvalidSkip)
	}
	b, err := Query(b, s, order, f)
	if err != nil {
		return b, err
	}
	seek, err := Seek(s, order, after)
	if err != nil {
		return b, err
	}
	if seek != nil {
		b = b.Where(seek)
	}
	b = b.Limit(uint64(pageSize) + 1)
	if skip > 0 {
		b = b.Offset(uint64(skip))
	}
	return b, nil
}

// Seek builds the row comparison that continues a page after pos. A first page
// has no position and returns a nil Sqlizer.
func Seek(s *flop.Schema, order []flop.OrderBy, pos flop.CursorPosition) (sq.Sqlizer, error) {
	expr, err := s.CompileSeek(order, pos)
	if err != nil {
		return nil, err
	}
	return WhereExpr(expr)
}
