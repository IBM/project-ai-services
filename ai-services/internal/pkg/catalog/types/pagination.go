package types

// PaginationMetadata represents pagination information in the response.
type PaginationMetadata struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalItems int  `json:"total_items"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// NewPaginationMetadata builds the pagination metadata for a page of a list of total items.
// The page and size are the validated request values.
func NewPaginationMetadata(page, size, total int) PaginationMetadata {
	totalPages := 0
	if total > 0 {
		totalPages = (total + size - 1) / size
	}

	return PaginationMetadata{
		Page:       page,
		PageSize:   size,
		TotalItems: total,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}
