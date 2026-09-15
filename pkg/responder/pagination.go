package responder

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// PaginationAll requests all records.
const PaginationAll = -1

// Default and maximum page sizes.
const (
	DefaultPageSize = 25
	MaxPageSize     = 100
)

// SortOrder is the direction of a sorted result set.
type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

// ErrInvalidPagination is returned by ParsePagination for malformed
// pagination query parameters.
var ErrInvalidPagination = errors.New("invalid pagination parameters")

// PaginationParams holds pagination query parameters.
type PaginationParams struct {
	Page      int
	Limit     int
	SortBy    string
	SortOrder SortOrder
}

// All reports whether the params request every record.
func (p PaginationParams) All() bool {
	return p.Page == PaginationAll || p.Limit == PaginationAll
}

// Offset returns the SQL offset for the current page.
func (p PaginationParams) Offset() int {
	if p.All() || p.Page < 1 || p.Limit < 1 {
		return 0
	}
	return (p.Page - 1) * p.Limit
}

// ParsePagination reads pagination parameters from the request.
func ParsePagination(r *http.Request) (PaginationParams, error) {
	return ParsePaginationSized(r, DefaultPageSize, MaxPageSize)
}

// ParsePaginationSized reads pagination parameters with custom limits.
func ParsePaginationSized(r *http.Request, defaultLimit, maxLimit int) (PaginationParams, error) {
	q := r.URL.Query()
	params := PaginationParams{Page: 1, Limit: defaultLimit, SortOrder: SortAscending}

	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		page, err := strconv.Atoi(raw)
		if err != nil || (page != PaginationAll && page < 1) {
			return params, fmt.Errorf("%w: page must be a positive integer or -1", ErrInvalidPagination)
		}
		params.Page = page
	}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || (limit != PaginationAll && limit < 1) {
			return params, fmt.Errorf("%w: limit must be a positive integer or -1", ErrInvalidPagination)
		}
		if limit != PaginationAll && limit > maxLimit {
			limit = maxLimit
		}
		params.Limit = limit
	}

	params.SortBy = strings.TrimSpace(q.Get("sort_by"))

	if raw := strings.TrimSpace(q.Get("sort_order")); raw != "" {
		switch SortOrder(raw) {
		case SortAscending, SortDescending:
			params.SortOrder = SortOrder(raw)
		default:
			return params, fmt.Errorf("%w: sort_order must be %q or %q", ErrInvalidPagination, SortAscending, SortDescending)
		}
	}

	return params, nil
}

// Pagination describes a page within a result set.
type Pagination struct {
	Page           *int
	Limit          *int
	TotalPages     *int
	TotalItems     *int
	FirstItemIndex *int
	LastItemIndex  *int
}

// NewPagination computes page metadata for totalItems records.
func NewPagination(params PaginationParams, totalItems int) Pagination {
	p := Pagination{}
	if totalItems < 0 {
		return p
	}
	p.TotalItems = &totalItems

	if params.All() || params.Limit < 1 {
		if totalItems > 0 {
			first, last := 0, totalItems-1
			p.FirstItemIndex, p.LastItemIndex = &first, &last
		}
		return p
	}

	p.Page = &params.Page
	limit := params.Limit
	p.Limit = &limit

	totalPages := (totalItems + limit - 1) / limit
	p.TotalPages = &totalPages

	first := (params.Page - 1) * limit
	if params.Page < 1 || first >= totalItems {
		return p // empty or out-of-range page: item range stays unset
	}
	last := min(first+limit-1, totalItems-1)
	p.FirstItemIndex, p.LastItemIndex = &first, &last
	return p
}

func (m *Metadata) applyPagination(p Pagination) {
	m.Page = p.Page
	m.Limit = p.Limit
	m.TotalPages = p.TotalPages
	m.TotalItems = p.TotalItems
	m.FirstItemIndex = p.FirstItemIndex
	m.LastItemIndex = p.LastItemIndex
}
