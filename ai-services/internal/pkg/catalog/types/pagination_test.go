package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPaginationMetadata(t *testing.T) {
	tests := []struct {
		name              string
		page, size, total int
		want              PaginationMetadata
	}{
		{"no items", 1, 20, 0, PaginationMetadata{Page: 1, PageSize: 20}},
		{"single page", 1, 20, 5, PaginationMetadata{Page: 1, PageSize: 20, TotalItems: 5, TotalPages: 1}},
		{"exact multiple", 1, 10, 20, PaginationMetadata{Page: 1, PageSize: 10, TotalItems: 20, TotalPages: 2, HasNext: true}},
		{"partial last page", 2, 10, 21, PaginationMetadata{Page: 2, PageSize: 10, TotalItems: 21, TotalPages: 3, HasNext: true, HasPrev: true}},
		{"last page", 3, 10, 21, PaginationMetadata{Page: 3, PageSize: 10, TotalItems: 21, TotalPages: 3, HasPrev: true}},
		{"page beyond the end", 5, 10, 21, PaginationMetadata{Page: 5, PageSize: 10, TotalItems: 21, TotalPages: 3, HasPrev: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewPaginationMetadata(tt.page, tt.size, tt.total))
		})
	}
}
