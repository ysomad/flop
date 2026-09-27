package flop

import (
	"slices"

	"github.com/ysomad/flop/aip132"
)

// ParseOrder parses an AIP-132 order_by clause and validates it against the
// schema. Use TotalOrder after merging defaults to append the tie-breaker.
func (s *Schema) ParseOrder(text string) ([]aip132.OrderBy, error) {
	order, err := aip132.ParseOrderBy(text)
	if err != nil {
		return nil, errorf(ErrInvalidOrder, "%v", err)
	}
	if err := s.ValidateOrder(order); err != nil {
		return nil, err
	}
	return order, nil
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

// TotalOrder appends missing fields of the schema's unique key so that no two
// rows compare equal. Existing terms keep their positions and directions.
// A schema declaring no unique key returns the order unchanged.
//
// Apply it after composing an order so that the tie-breaker ends up last:
//
//	order := schema.TotalOrder(flop.MergeOrder(defaultOrder, requested))
func (s *Schema) TotalOrder(order []aip132.OrderBy) []aip132.OrderBy {
	if len(s.uniqueKey) == 0 {
		return order
	}
	result := slices.Clone(order)
	for _, field := range s.uniqueKey {
		if !slices.ContainsFunc(result, func(term aip132.OrderBy) bool {
			return term.FieldPath.Equals(field.path)
		}) {
			result = append(result, aip132.OrderBy{FieldPath: field.path})
		}
	}
	return result
}

// MergeOrder combines a requested order with a schema's default. Terms in order
// take precedence, and the terms of def it does not name follow in the order def
// gives them. Repeated fields are removed, with the first occurrence winning.
func MergeOrder(def, order []aip132.OrderBy) []aip132.OrderBy {
	merged := make([]aip132.OrderBy, 0, len(order)+len(def))
	seen := make(map[string]struct{}, len(order))
	for _, terms := range [][]aip132.OrderBy{order, def} {
		for _, term := range terms {
			path := term.FieldPath.String()
			if _, ok := seen[path]; !ok {
				merged = append(merged, term)
				seen[path] = struct{}{}
			}
		}
	}
	return merged
}
