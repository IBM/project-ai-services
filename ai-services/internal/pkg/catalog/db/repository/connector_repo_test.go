package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// TestBuildWhereClause_NilFilters checks that nil filters produce no WHERE clause.
func TestBuildWhereClause_NilFilters(t *testing.T) {
	where, args := buildWhereClause(nil)

	assert.Empty(t, where)
	assert.Empty(t, args)
}

// TestBuildWhereClause_EmptyFilters checks that an all-zero filter struct is ignored.
func TestBuildWhereClause_EmptyFilters(t *testing.T) {
	where, args := buildWhereClause(&ConnectorFilters{})

	assert.Empty(t, where)
	assert.Empty(t, args)
}

// TestBuildWhereClause_SingleFilters verifies each filter in isolation.
func TestBuildWhereClause_SingleFilters(t *testing.T) {
	tests := []struct {
		name         string
		filters      *ConnectorFilters
		expectedSQL  string
		expectedArgs []interface{}
	}{
		{
			name:         "type",
			filters:      &ConnectorFilters{Type: "datasource"},
			expectedSQL:  " WHERE type = $1",
			expectedArgs: []interface{}{"datasource"},
		},
		{
			name:         "status",
			filters:      &ConnectorFilters{Status: models.ConnectorStatusConnected},
			expectedSQL:  " WHERE status = $1",
			expectedArgs: []interface{}{models.ConnectorStatusConnected},
		},
		{
			name:         "provider",
			filters:      &ConnectorFilters{Provider: "object_storage"},
			expectedSQL:  " WHERE provider = $1",
			expectedArgs: []interface{}{"object_storage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildWhereClause(tt.filters)

			assert.Equal(t, tt.expectedSQL, where)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

// TestBuildWhereClause_SearchFilter verifies the ILIKE clause, ESCAPE guard and bound pattern.
func TestBuildWhereClause_SearchFilter(t *testing.T) {
	where, args := buildWhereClause(&ConnectorFilters{Search: "my-db"})

	assert.Contains(t, where, "WHERE")
	assert.Contains(t, where, "name ILIKE $1")
	assert.Contains(t, where, `ESCAPE '\'`)
	require.Len(t, args, 1)
	assert.Equal(t, "%my-db%", args[0])
}

// TestBuildWhereClause_SearchEscapesWildcards verifies LIKE wildcards are escaped.
func TestBuildWhereClause_SearchEscapesWildcards(t *testing.T) {
	_, args := buildWhereClause(&ConnectorFilters{Search: `100%_\`})

	require.Len(t, args, 1)
	assert.Equal(t, `%100\%\_\\%`, args[0])
}

// TestBuildWhereClause_AllFilters verifies placeholder numbering, arg order and AND-joining
// when every filter is set.
func TestBuildWhereClause_AllFilters(t *testing.T) {
	where, args := buildWhereClause(&ConnectorFilters{
		Type:     "datasource",
		Status:   models.ConnectorStatusOffline,
		Provider: "file_system",
		Search:   "abc",
	})

	assert.True(t, strings.HasPrefix(where, " WHERE "))
	assert.Contains(t, where, "type = $1")
	assert.Contains(t, where, "status = $2")
	assert.Contains(t, where, "provider = $3")
	assert.Contains(t, where, "name ILIKE $4")
	assert.Equal(t, 3, strings.Count(where, " AND "))

	require.Len(t, args, 4)
	assert.Equal(t, "datasource", args[0])
	assert.Equal(t, models.ConnectorStatusOffline, args[1])
	assert.Equal(t, "file_system", args[2])
	assert.Equal(t, "%abc%", args[3])
}

// TestBuildWhereClause_PaginationIgnored checks that Limit/Offset do not affect the WHERE clause.
func TestBuildWhereClause_PaginationIgnored(t *testing.T) {
	where, args := buildWhereClause(&ConnectorFilters{Limit: 10, Offset: 5})

	assert.Empty(t, where)
	assert.Empty(t, args)
}

// TestConnectorBuildListQuery_NoFilters checks the base query and ordering.
func TestConnectorBuildListQuery_NoFilters(t *testing.T) {
	repo := &connectorRepo{}

	query, args := repo.buildListQuery(nil)

	assert.Equal(t, "SELECT "+nonSensitiveColumns+" FROM connectors ORDER BY created_at DESC", query)
	assert.Empty(t, args)
}

// TestConnectorBuildListQuery_NeverSelectsMetadata guards against leaking credentials.
func TestConnectorBuildListQuery_NeverSelectsMetadata(t *testing.T) {
	repo := &connectorRepo{}

	query, _ := repo.buildListQuery(&ConnectorFilters{Search: "x", Limit: 1, Offset: 1})

	assert.NotContains(t, query, "metadata")
}

// TestConnectorBuildListQuery_LimitOnly checks LIMIT without OFFSET.
func TestConnectorBuildListQuery_LimitOnly(t *testing.T) {
	repo := &connectorRepo{}

	query, args := repo.buildListQuery(&ConnectorFilters{Limit: 20})

	assert.Contains(t, query, "LIMIT $1")
	assert.NotContains(t, query, "OFFSET")
	require.Len(t, args, 1)
	assert.Equal(t, 20, args[0])
}

// TestConnectorBuildListQuery_OffsetOnly checks OFFSET without LIMIT.
func TestConnectorBuildListQuery_OffsetOnly(t *testing.T) {
	repo := &connectorRepo{}

	query, args := repo.buildListQuery(&ConnectorFilters{Offset: 40})

	assert.NotContains(t, query, "LIMIT")
	assert.Contains(t, query, "OFFSET $1")
	require.Len(t, args, 1)
	assert.Equal(t, 40, args[0])
}

// TestConnectorBuildListQuery_PaginationAfterFilters checks that LIMIT/OFFSET placeholders
// continue numbering after the filter args.
func TestConnectorBuildListQuery_PaginationAfterFilters(t *testing.T) {
	repo := &connectorRepo{}

	query, args := repo.buildListQuery(&ConnectorFilters{
		Type:   "datasource",
		Search: "abc",
		Limit:  20,
		Offset: 40,
	})

	// Args: type ($1), search ($2), limit ($3), offset ($4).
	require.Len(t, args, 4)
	assert.Equal(t, "datasource", args[0])
	assert.Equal(t, "%abc%", args[1])
	assert.Equal(t, 20, args[2])
	assert.Equal(t, 40, args[3])

	assert.Contains(t, query, "LIMIT $3")
	assert.Contains(t, query, "OFFSET $4")

	// Clause ordering: WHERE -> ORDER BY -> LIMIT -> OFFSET.
	assert.Less(t, strings.Index(query, "WHERE"), strings.Index(query, "ORDER BY"))
	assert.Less(t, strings.Index(query, "ORDER BY"), strings.Index(query, "LIMIT"))
	assert.Less(t, strings.Index(query, "LIMIT"), strings.Index(query, "OFFSET"))
}

// TestConnectorListAndCountShareWhereClause is a regression guard: GetCount and List both
// derive their filter from buildWhereClause, so paginated totals match the filtered list.
func TestConnectorListAndCountShareWhereClause(t *testing.T) {
	repo := &connectorRepo{}
	filters := &ConnectorFilters{Type: "datasource", Search: "abc"}

	where, countArgs := buildWhereClause(filters)
	listQuery, listArgs := repo.buildListQuery(filters)

	assert.Contains(t, listQuery, where)
	assert.Equal(t, countArgs, listArgs)
}

// TestConnectorColumns guards the projection constants used on API vs. internal paths.
func TestConnectorColumns(t *testing.T) {
	assert.NotContains(t, nonSensitiveColumns, "metadata")
	assert.Equal(t, nonSensitiveColumns+", metadata", allColumns)
}

// TestConnectorSearchColumns guards the default searchable columns.
func TestConnectorSearchColumns(t *testing.T) {
	assert.Equal(t, []string{"name"}, connectorSearchColumns)
}

// TestNewConnectorRepository checks that the constructor returns a usable repository.
func TestNewConnectorRepository(t *testing.T) {
	repo := NewConnectorRepository(nil)

	require.NotNil(t, repo)
	_, ok := repo.(*connectorRepo)
	assert.True(t, ok)
}

// TestConnectorSentinelErrors checks the sentinel errors are distinct and have stable messages.
func TestConnectorSentinelErrors(t *testing.T) {
	assert.NotErrorIs(t, ErrConnectorInUse, ErrConnectorNotFound)
	assert.EqualError(t, ErrConnectorInUse, "connector is in use")
	assert.EqualError(t, ErrConnectorNotFound, "connector not found")
}
