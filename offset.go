package flop

// Offset returns the row offset of a one-based page number. A zero page number
// selects the first page.
func Offset(page, pageSize int32) (int64, error) {
	if page < 0 {
		return 0, errorf(ErrInvalidPage, "must not be negative")
	}
	if pageSize <= 0 {
		return 0, errorf(ErrInvalidPageSize, "must be positive")
	}
	if page == 0 {
		page = 1
	}
	return int64(page-1) * int64(pageSize), nil
}

// OffsetPage is a page of rows and where it sits in the collection.
type OffsetPage[T any] struct {
	Items      []T
	Page       int32
	TotalPages int64
	TotalItems int64
}

// NewOffsetPage assembles the page a page-number query returned. A zero page
// number is the first page, as it is for [Offset].
func NewOffsetPage[T any](rows []T, page, pageSize int32, totalItems int64) (OffsetPage[T], error) {
	if page < 0 {
		return OffsetPage[T]{}, errorf(ErrInvalidPage, "must not be negative")
	}
	if pageSize <= 0 {
		return OffsetPage[T]{}, errorf(ErrInvalidPageSize, "must be positive")
	}
	if page == 0 {
		page = 1
	}
	return OffsetPage[T]{
		Items:      rows,
		Page:       page,
		TotalPages: TotalPages(totalItems, pageSize),
		TotalItems: totalItems,
	}, nil
}

// TotalPages returns how many pages of pageSize rows totalItems fills.
func TotalPages(totalItems int64, pageSize int32) int64 {
	if pageSize <= 0 || totalItems <= 0 {
		return 0
	}
	pages := totalItems / int64(pageSize)
	if totalItems%int64(pageSize) != 0 {
		pages++
	}
	return pages
}
