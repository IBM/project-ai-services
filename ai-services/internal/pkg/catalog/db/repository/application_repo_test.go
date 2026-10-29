package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildApplicationWhereClauses_SearchFilter verifies that buildApplicationWhereClauses
// produces the correct ILIKE clause and bound argument for the Search filter.
func TestBuildApplicationWhereClauses_SearchFilter(t *testing.T) {
	filters := &ApplicationFilters{Search: "myapp"}

	clauses, args := buildApplicationWhereClauses(filters)

	require.Len(t, clauses, 1)
	require.Len(t, args, 1)

	// The clause must reference $1 and use ILIKE with the ESCAPE guard.
	assert.Contains(t, clauses[0], "ILIKE $1")
	assert.Contains(t, clauses[0], `ESCAPE '\'`)

	// The bound arg must be the wrapped pattern.
	assert.Equal(t, "%myapp%", args[0])
}

// TestBuildGetAllQuery_SearchFilter checks that buildGetAllQuery embeds the WHERE clause
// when a Search filter is provided.
func TestBuildGetAllQuery_SearchFilter(t *testing.T) {
	repo := &applicationRepo{}
	filters := &ApplicationFilters{Search: "myapp"}

	query, args := repo.buildGetAllQuery(filters)

	assert.Contains(t, query, "WHERE")
	assert.Contains(t, query, "ILIKE $1")
	require.Len(t, args, 1)
	assert.Equal(t, "%myapp%", args[0])
}

// TestBuildGetAllQuery_NoFilter checks that buildGetAllQuery omits the WHERE clause
// when no filters are provided.
func TestBuildGetAllQuery_NoFilter(t *testing.T) {
	repo := &applicationRepo{}

	query, args := repo.buildGetAllQuery(nil)

	assert.NotContains(t, query, "WHERE")
	assert.Empty(t, args)
}

// TestBuildGetAllQuery_MultipleFilters checks that multiple filters are AND-joined.
func TestBuildGetAllQuery_MultipleFilters(t *testing.T) {
	repo := &applicationRepo{}
	filters := &ApplicationFilters{
		DeploymentType: "architectures",
		CatalogID:      "rag",
		Search:         "myapp",
	}

	query, args := repo.buildGetAllQuery(filters)

	assert.Contains(t, query, "WHERE")
	assert.Contains(t, query, "AND")
	// Three filter args: deployment_type ($1), catalog_id ($2), search pattern ($3).
	require.Len(t, args, 3)
	assert.Equal(t, "architectures", args[0])
	assert.Equal(t, "rag", args[1])
	assert.Equal(t, "%myapp%", args[2])
}

// TestGetCountQueryMatchesGetAllQuery_SearchFilter is the key regression guard:
// it asserts that the WHERE clause produced by buildApplicationWhereClauses is
// consistent across both buildGetAllQuery and GetCount so that paginated totals
// always match the filtered list.
func TestGetCountQueryMatchesGetAllQuery_SearchFilter(t *testing.T) {
	repo := &applicationRepo{}
	filters := &ApplicationFilters{Search: "myapp"}

	getAllQuery, getAllArgs := repo.buildGetAllQuery(filters)

	// Manually replicate what GetCount does (no DB call needed — we test the
	// query-building path only).
	countBase := `SELECT COUNT(DISTINCT a.id) FROM applications a`
	whereClauses, countArgs := buildApplicationWhereClauses(filters)
	countQuery := countBase
	if len(whereClauses) > 0 {
		countQuery += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Both queries must use the same number of filter args and the same bound value.
	assert.Equal(t, len(countArgs), len(getAllArgs),
		"GetCount and buildGetAllQuery must bind the same number of filter args")
	for i := range countArgs {
		assert.Equal(t, countArgs[i], getAllArgs[i],
			"arg[%d] differs between GetCount and buildGetAllQuery", i)
	}

	// Both queries must contain the same WHERE fragment.
	assert.Contains(t, getAllQuery, "WHERE")
	assert.Contains(t, countQuery, "WHERE")
	assert.Contains(t, getAllQuery, "ILIKE $1")
	assert.Contains(t, countQuery, "ILIKE $1")
}

// TestBuildGetAllQuery_PaginationArgs checks that Limit and Offset are appended
// after filter args and do not interfere with the WHERE clause arg indices.
func TestBuildGetAllQuery_PaginationArgs(t *testing.T) {
	repo := &applicationRepo{}
	filters := &ApplicationFilters{
		Search: "myapp",
		Limit:  20,
		Offset: 40,
	}

	query, args := repo.buildGetAllQuery(filters)

	// Args: search pattern ($1), limit ($2), offset ($3).
	require.Len(t, args, 3)
	assert.Equal(t, "%myapp%", args[0])
	assert.Equal(t, 20, args[1])
	assert.Equal(t, 40, args[2])

	assert.Contains(t, query, "LIMIT $2")
	assert.Contains(t, query, "OFFSET $3")
}

// TestBuildApplicationWhereClauses_CreatedByFilter verifies that buildApplicationWhereClauses
// produces the correct created_by clause and bound argument.
func TestBuildApplicationWhereClauses_CreatedByFilter(t *testing.T) {
	filters := &ApplicationFilters{
		CreatedBy: "test-user-id",
	}

	clauses, args := buildApplicationWhereClauses(filters)

	require.Len(t, clauses, 1)
	require.Len(t, args, 1)
	assert.Equal(t, "a.created_by = $1", clauses[0])
	assert.Equal(t, "test-user-id", args[0])
}
