package flop

import (
	"slices"

	"github.com/ysomad/flop/aip132"
)

// ParseOrder parses an AIP-132 order_by clause and validates it against the
// schema.
//
// A schema that declares a unique field has it appended when the clause does
// not already name it, so the order is total. Cursor pagination needs that to
// page without repeating or dropping rows.
func (s *Schema) ParseOrder(text string) ([]aip132.OrderBy, error) {
	order, err := aip132.ParseOrderBy(text)
	if err != nil {
		return nil, errorf(ErrInvalidOrder, "%v", err)
	}
	if err := s.ValidateOrder(order); err != nil {
		return nil, err
	}
	return s.TotalOrder(order), nil
}

// ValidateOrder reports whether every term names a distinct sortable field.
func (s *Schema) ValidateOrder(order []aip132.OrderBy) error {
	seen := make(map[string]struct{}, len(order))
	for _, term := range order {
		path := term.FieldPath.String()
		if _, ok := seen[path]; ok {
			return errorf(ErrInvalidOrder, "field %q appears twice", path)
		}
		seen[path] = struct{}{}
		if _, err := s.SortableField(term.FieldPath); err != nil {
			return err
		}
	}
	return nil
}

// TotalOrder appends the schema's unique field to an order that does not
// already name it, so that no two rows compare equal. A schema declaring no
// unique field returns the order unchanged.
//
// [Schema.ParseOrder] applies it already. Call it directly when composing an
// order first, so that the tie-breaker ends up last:
//
//	order := schema.TotalOrder(flop.MergeOrder(defaultOrder, requested))
func (s *Schema) TotalOrder(order []aip132.OrderBy) []aip132.OrderBy {
	if s.uniqueKey == nil {
		return order
	}
	unique := s.uniqueKey.path
	if slices.ContainsFunc(order, func(term aip132.OrderBy) bool {
		return term.FieldPath.Equals(unique)
	}) {
		return order
	}
	return append(order, aip132.OrderBy{FieldPath: unique})
}

// MergeOrder combines an order with the default one an endpoint sorts by.
// Terms in order take precedence, and the terms of def it does not name follow
// in the order def gives them.
func MergeOrder(def, order []aip132.OrderBy) []aip132.OrderBy {
	merged := make([]aip132.OrderBy, 0, len(order)+len(def))
	seen := make(map[string]struct{}, len(order))
	for _, term := range order {
		merged = append(merged, term)
		seen[term.FieldPath.String()] = struct{}{}
	}
	for _, term := range def {
		if _, ok := seen[term.FieldPath.String()]; !ok {
			merged = append(merged, term)
		}
	}
	return merged
}
