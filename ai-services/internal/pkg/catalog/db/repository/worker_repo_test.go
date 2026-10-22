package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildGetAllWorkerQuery_NilRequest checks that a nil request produces no WHERE clause
// and no bound args.
func TestBuildGetAllWorkerQuery_NilRequest(t *testing.T) {
	query, args := buildGetAllWorkerQuery(nil)

	assert.NotContains(t, query, "WHERE")
	assert.Contains(t, query, "FROM workers")
	assert.Contains(t, query, "ORDER BY "+workerStatusPriority+", registered_at ASC")
	assert.Empty(t, args)
}

// TestBuildGetAllWorkerQuery_EmptySearch checks that an empty Search is ignored.
func TestBuildGetAllWorkerQuery_EmptySearch(t *testing.T) {
	query, args := buildGetAllWorkerQuery(&ListWorkersRequest{})

	assert.NotContains(t, query, "WHERE")
	assert.Empty(t, args)
}

// TestBuildGetAllWorkerQuery_SearchFilter verifies the ILIKE clause, ESCAPE guard,
// bound pattern, and that ORDER BY follows the WHERE clause.
func TestBuildGetAllWorkerQuery_SearchFilter(t *testing.T) {
	query, args := buildGetAllWorkerQuery(&ListWorkersRequest{Search: "worker-a"})

	assert.Contains(t, query, "WHERE")
	assert.Contains(t, query, "name ILIKE $1")
	assert.Contains(t, query, `ESCAPE '\'`)
	require.Len(t, args, 1)
	assert.Equal(t, "%worker-a%", args[0])

	assert.Less(t, strings.Index(query, "WHERE"), strings.Index(query, "ORDER BY"),
		"WHERE must precede ORDER BY")
}

// TestBuildGetAllWorkerQuery_SearchEscapesWildcards verifies that LIKE wildcards
// and backslashes in user input are escaped so they match literally.
func TestBuildGetAllWorkerQuery_SearchEscapesWildcards(t *testing.T) {
	tests := []struct {
		name     string
		search   string
		expected string
	}{
		{"percent", "100%", `%100\%%`},
		{"underscore", "a_b", `%a\_b%`},
		{"backslash", `a\b`, `%a\\b%`},
		{"all wildcards", `%_\`, `%\%\_\\%`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, args := buildGetAllWorkerQuery(&ListWorkersRequest{Search: tt.search})

			require.Len(t, args, 1)
			assert.Equal(t, tt.expected, args[0])
		})
	}
}

// TestWorkerSearchColumns guards the default searchable columns.
func TestWorkerSearchColumns(t *testing.T) {
	assert.Equal(t, []string{"name"}, workerSearchColumns)
}

// TestNewWorkerRepository checks that the constructor returns a usable repository.
func TestNewWorkerRepository(t *testing.T) {
	repo := NewWorkerRepository(nil)

	require.NotNil(t, repo)
	_, ok := repo.(*workerRepo)
	assert.True(t, ok)
}

// TestWorkerUpdate_ZeroValueLeavesFieldsNil documents that an empty WorkerUpdate
// carries no changes (all nil → COALESCE keeps existing values).
func TestWorkerUpdate_ZeroValueLeavesFieldsNil(t *testing.T) {
	var u WorkerUpdate

	assert.Nil(t, u.Status)
	assert.Nil(t, u.LastHeartbeat)
	assert.Nil(t, u.Message)
}

// TestBuildGetAllWorkerQuery_Pagination tests LIMIT and OFFSET parameter construction.
func TestBuildGetAllWorkerQuery_Pagination(t *testing.T) {
	t.Run("limit only", func(t *testing.T) {
		query, args := buildGetAllWorkerQuery(&ListWorkersRequest{Limit: 10})
		assert.Contains(t, query, "LIMIT $1")
		assert.NotContains(t, query, "OFFSET")
		require.Len(t, args, 1)
		assert.Equal(t, 10, args[0])
	})

	t.Run("offset only", func(t *testing.T) {
		query, args := buildGetAllWorkerQuery(&ListWorkersRequest{Offset: 20})
		assert.NotContains(t, query, "LIMIT")
		assert.Contains(t, query, "OFFSET $1")
		require.Len(t, args, 1)
		assert.Equal(t, 20, args[0])
	})

	t.Run("search with limit and offset", func(t *testing.T) {
		query, args := buildGetAllWorkerQuery(&ListWorkersRequest{
			Search: "worker",
			Limit:  10,
			Offset: 20,
		})
		assert.Contains(t, query, "name ILIKE $1")
		assert.Contains(t, query, "LIMIT $2")
		assert.Contains(t, query, "OFFSET $3")
		require.Len(t, args, 3)
		assert.Equal(t, "%worker%", args[0])
		assert.Equal(t, 10, args[1])
		assert.Equal(t, 20, args[2])

		assert.Less(t, strings.Index(query, "WHERE"), strings.Index(query, "ORDER BY"))
		assert.Less(t, strings.Index(query, "ORDER BY"), strings.Index(query, "LIMIT"))
		assert.Less(t, strings.Index(query, "LIMIT"), strings.Index(query, "OFFSET"))
	})
}

// TestBuildWorkerWhereClauses_NilRequest checks that nil request yields empty WHERE clauses.
func TestBuildWorkerWhereClauses_NilRequest(t *testing.T) {
	clauses, args := buildWorkerWhereClauses(nil)
	assert.Empty(t, clauses)
	assert.Empty(t, args)
}

// TestStatusPriorityCase verifies the generated CASE expressions rank statuses as required.
func TestStatusPriorityCase(t *testing.T) {
	assert.Equal(t,
		"CASE status WHEN 'disconnected' THEN 0 WHEN 'pending' THEN 1 WHEN 'ready' THEN 2 ELSE 3 END",
		workerStatusPriority)
	assert.Equal(t,
		"CASE status WHEN 'offline' THEN 0 WHEN 'connected' THEN 1 ELSE 2 END",
		connectorStatusPriority)
	assert.Equal(t,
		"CASE a.status WHEN 'Error' THEN 0 WHEN 'Downloading' THEN 1 WHEN 'Deploying' THEN 1 "+
			"WHEN 'Deleting' THEN 1 WHEN 'Running' THEN 2 ELSE 3 END",
		applicationStatusPriority)
}
