package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildGetAllWorkerQuery verifies the generated SQL and bound args for the
// search filter and LIMIT/OFFSET pagination combinations.
func TestBuildGetAllWorkerQuery(t *testing.T) {
	tests := []struct {
		name         string
		req          *ListWorkersRequest
		wantContains []string
		wantAbsent   []string
		wantArgs     []any
		// wantOrder lists fragments that must appear in the query in this order.
		wantOrder []string
	}{
		{
			name:         "nil request has no WHERE clause and no args",
			req:          nil,
			wantContains: []string{"FROM workers", "registered_at ASC"},
			wantAbsent:   []string{"WHERE", "LIMIT", "OFFSET"},
			wantArgs:     []any{},
		},
		{
			name:       "empty search is ignored",
			req:        &ListWorkersRequest{},
			wantAbsent: []string{"WHERE", "LIMIT", "OFFSET"},
			wantArgs:   []any{},
		},
		{
			name:         "search filter uses ILIKE with ESCAPE",
			req:          &ListWorkersRequest{Search: "worker-a"},
			wantContains: []string{"WHERE", "name ILIKE $1", `ESCAPE '\'`},
			wantAbsent:   []string{"LIMIT", "OFFSET"},
			wantArgs:     []any{"%worker-a%"},
			wantOrder:    []string{"WHERE", "ORDER BY"},
		},
		{
			name:         "limit only",
			req:          &ListWorkersRequest{Limit: 10},
			wantContains: []string{"LIMIT $1"},
			wantAbsent:   []string{"OFFSET", "WHERE"},
			wantArgs:     []any{10},
		},
		{
			name:         "offset only",
			req:          &ListWorkersRequest{Offset: 20},
			wantContains: []string{"OFFSET $1"},
			wantAbsent:   []string{"LIMIT", "WHERE"},
			wantArgs:     []any{20},
		},
		{
			name:         "search with limit and offset",
			req:          &ListWorkersRequest{Search: "worker", Limit: 10, Offset: 20},
			wantContains: []string{"name ILIKE $1", "LIMIT $2", "OFFSET $3"},
			wantArgs:     []any{"%worker%", 10, 20},
			wantOrder:    []string{"WHERE", "ORDER BY", "LIMIT", "OFFSET"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildGetAllWorkerQuery(tt.req)

			for _, s := range tt.wantContains {
				assert.Contains(t, query, s)
			}
			for _, s := range tt.wantAbsent {
				assert.NotContains(t, query, s)
			}
			if len(tt.wantArgs) == 0 {
				assert.Empty(t, args)
			} else {
				assert.Equal(t, tt.wantArgs, args)
			}

			prev := -1
			for _, s := range tt.wantOrder {
				idx := strings.Index(query, s)
				require.GreaterOrEqual(t, idx, 0, "%q not found in query", s)
				assert.Greater(t, idx, prev, "%q out of order", s)
				prev = idx
			}
		})
	}
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
