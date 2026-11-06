package repository

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestBuildModelWhere_OwnerIsAlwaysFirst(t *testing.T) {
	where, args := buildModelWhere(&ModelFilters{CreatedBy: "alice"})

	assert.Equal(t, " WHERE created_by = $1", where)
	assert.Equal(t, []interface{}{"alice"}, args)
}

func TestBuildModelWhere_Filters(t *testing.T) {
	tests := []struct {
		name         string
		filters      ModelFilters
		expectedSQL  string
		expectedArgs []interface{}
	}{
		{
			name:         "deployment type",
			filters:      ModelFilters{CreatedBy: "alice", DeploymentType: "local"},
			expectedSQL:  " WHERE created_by = $1 AND deployment_type = $2",
			expectedArgs: []interface{}{"alice", "local"},
		},
		{
			name:         "status",
			filters:      ModelFilters{CreatedBy: "alice", Status: "Running"},
			expectedSQL:  " WHERE created_by = $1 AND status = $2",
			expectedArgs: []interface{}{"alice", "Running"},
		},
		{
			name: "all filters keep a stable order and numbering",
			filters: ModelFilters{
				CreatedBy: "alice", DeploymentType: "local", ComponentType: "llm",
				Providers: []string{"vllm-spyre"}, Models: []string{"ibm-granite/granite"}, Status: "Running",
			},
			expectedSQL: " WHERE created_by = $1 AND deployment_type = $2 AND component_type = $3" +
				" AND provider = ANY($4) AND model = ANY($5) AND status = $6",
			expectedArgs: []interface{}{"alice", "local", "llm", []string{"vllm-spyre"}, []string{"ibm-granite/granite"}, "Running"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildModelWhere(&tt.filters)

			assert.Equal(t, tt.expectedSQL, where)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

// User input must only ever travel as a bound argument, never inside the SQL text.
func TestBuildModelListQuery_FiltersAreBoundNotInlined(t *testing.T) {
	const hostile = "x' OR '1'='1"

	query, args := buildModelListQuery(&ModelFilters{CreatedBy: hostile, Models: []string{hostile}, Limit: 20})

	assert.NotContains(t, query, hostile)
	assert.Contains(t, args, hostile)
	assert.Contains(t, args, []string{hostile})
}

func TestBuildModelListQuery_UnionAllAndScoping(t *testing.T) {
	query, args := buildModelListQuery(&ModelFilters{CreatedBy: "alice", Limit: 20, Offset: 40})

	assert.Contains(t, query, "UNION ALL")
	assert.Contains(t, query, "FROM components c")
	assert.Contains(t, query, "FROM connectors cn")
	assert.Contains(t, query, "c.name IS NOT NULL", "pipeline-created components must stay out of the list")
	assert.Contains(t, query, "cn.type = 'models'", "datasource connectors must stay out of the list")
	assert.Contains(t, query, "ORDER BY created_at DESC, id ASC")
	assert.Contains(t, query, "LIMIT $2 OFFSET $3")
	assert.Equal(t, []interface{}{"alice", 20, 40}, args)
}

func TestBuildModelListQuery(t *testing.T) {
	const orderBy = " ORDER BY created_at DESC, id ASC"

	tests := []struct {
		name         string
		filters      ModelFilters
		expectedTail string // everything after the union subquery: alias, WHERE, ORDER BY, paging
		expectedArgs []interface{}
	}{
		{
			name:         "owner only has no paging",
			filters:      ModelFilters{CreatedBy: "alice"},
			expectedTail: ") m WHERE created_by = $1" + orderBy,
			expectedArgs: []interface{}{"alice"},
		},
		{
			name:         "limit only",
			filters:      ModelFilters{CreatedBy: "alice", Limit: 20},
			expectedTail: ") m WHERE created_by = $1" + orderBy + " LIMIT $2",
			expectedArgs: []interface{}{"alice", 20},
		},
		{
			name:         "offset only",
			filters:      ModelFilters{CreatedBy: "alice", Offset: 40},
			expectedTail: ") m WHERE created_by = $1" + orderBy + " OFFSET $2",
			expectedArgs: []interface{}{"alice", 40},
		},
		{
			name:         "limit and offset",
			filters:      ModelFilters{CreatedBy: "alice", Limit: 20, Offset: 40},
			expectedTail: ") m WHERE created_by = $1" + orderBy + " LIMIT $2 OFFSET $3",
			expectedArgs: []interface{}{"alice", 20, 40},
		},
		{
			name:         "zero limit and offset are not applied",
			filters:      ModelFilters{CreatedBy: "alice", Limit: 0, Offset: 0},
			expectedTail: ") m WHERE created_by = $1" + orderBy,
			expectedArgs: []interface{}{"alice"},
		},
		{
			name:         "deployment type filter",
			filters:      ModelFilters{CreatedBy: "alice", DeploymentType: "remote"},
			expectedTail: ") m WHERE created_by = $1 AND deployment_type = $2" + orderBy,
			expectedArgs: []interface{}{"alice", "remote"},
		},
		{
			name:         "component type filter",
			filters:      ModelFilters{CreatedBy: "alice", ComponentType: "llm"},
			expectedTail: ") m WHERE created_by = $1 AND component_type = $2" + orderBy,
			expectedArgs: []interface{}{"alice", "llm"},
		},
		{
			name:         "provider filter",
			filters:      ModelFilters{CreatedBy: "alice", Providers: []string{"vllm-spyre"}},
			expectedTail: ") m WHERE created_by = $1 AND provider = ANY($2)" + orderBy,
			expectedArgs: []interface{}{"alice", []string{"vllm-spyre"}},
		},
		{
			name:         "model filter",
			filters:      ModelFilters{CreatedBy: "alice", Models: []string{"ibm-granite/granite"}},
			expectedTail: ") m WHERE created_by = $1 AND model = ANY($2)" + orderBy,
			expectedArgs: []interface{}{"alice", []string{"ibm-granite/granite"}},
		},
		{
			name:         "status filter",
			filters:      ModelFilters{CreatedBy: "alice", Status: "Running"},
			expectedTail: ") m WHERE created_by = $1 AND status = $2" + orderBy,
			expectedArgs: []interface{}{"alice", "Running"},
		},
		{
			name:         "filters are followed by paging placeholders",
			filters:      ModelFilters{CreatedBy: "alice", Status: "Running", Providers: []string{"watsonx"}, Limit: 10, Offset: 10},
			expectedTail: ") m WHERE created_by = $1 AND provider = ANY($2) AND status = $3" + orderBy + " LIMIT $4 OFFSET $5",
			expectedArgs: []interface{}{"alice", []string{"watsonx"}, "Running", 10, 10},
		},
		{
			name: "all filters and paging",
			filters: ModelFilters{
				CreatedBy: "alice", DeploymentType: "local", ComponentType: "llm",
				Providers: []string{"vllm-spyre"}, Models: []string{"ibm-granite/granite"}, Status: "Running",
				Limit: 50, Offset: 100,
			},
			expectedTail: ") m WHERE created_by = $1 AND deployment_type = $2 AND component_type = $3" +
				" AND provider = ANY($4) AND model = ANY($5) AND status = $6" + orderBy + " LIMIT $7 OFFSET $8",
			expectedArgs: []interface{}{"alice", "local", "llm", []string{"vllm-spyre"}, []string{"ibm-granite/granite"}, "Running", 50, 100},
		},
		{
			name: "several providers and models are one clause each",
			filters: ModelFilters{
				CreatedBy: "alice", Providers: []string{"vllm-spyre", "watsonx"},
				Models: []string{"ibm/granite-4-h-small", "ibm-granite/granite-3.3-8b-instruct"},
			},
			expectedTail: ") m WHERE created_by = $1 AND provider = ANY($2) AND model = ANY($3)" + orderBy,
			expectedArgs: []interface{}{
				"alice", []string{"vllm-spyre", "watsonx"},
				[]string{"ibm/granite-4-h-small", "ibm-granite/granite-3.3-8b-instruct"},
			},
		},
		{
			name:         "empty multi-value filters are not applied",
			filters:      ModelFilters{CreatedBy: "alice", Providers: []string{}, Models: nil},
			expectedTail: ") m WHERE created_by = $1" + orderBy,
			expectedArgs: []interface{}{"alice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildModelListQuery(&tt.filters)

			expectedQuery := "SELECT " + modelColumns + " FROM (" + modelUnionQuery + tt.expectedTail
			assert.Equal(t, expectedQuery, query)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

// The count must filter exactly like the list, otherwise total_pages would not match the data.
func TestBuildModelCountQuery_SharesUnionAndFilters(t *testing.T) {
	f := &ModelFilters{CreatedBy: "alice", Status: "connected", Limit: 20, Offset: 20}

	listQuery, listArgs := buildModelListQuery(f)
	countQuery, countArgs := buildModelCountQuery(f)

	assert.True(t, strings.HasPrefix(countQuery, "SELECT COUNT(*) FROM ("))
	assert.Contains(t, countQuery, "UNION ALL")
	assert.Contains(t, countQuery, " WHERE created_by = $1 AND status = $2")
	assert.NotContains(t, countQuery, "LIMIT")
	assert.NotContains(t, countQuery, "ORDER BY")
	assert.Equal(t, []interface{}{"alice", "connected"}, countArgs)
	assert.Equal(t, []interface{}{"alice", "connected", 20, 20}, listArgs)
	assert.Contains(t, listQuery, " WHERE created_by = $1 AND status = $2")
}

func TestBuildModelGetQuery(t *testing.T) {
	id := uuid.New()

	query, args := buildModelGetQuery(id)

	assert.Equal(t, "SELECT "+modelColumns+" FROM ("+modelUnionQuery+") m WHERE id = $1", query)
	assert.Equal(t, []interface{}{id}, args, "the id is bound")
	assert.Contains(t, query, "UNION ALL", "a model is looked up in both tables")
}

// As for applications, the owner filter is optional: without one the list is not narrowed by it.
func TestBuildModelWhere_OwnerIsOptional(t *testing.T) {
	where, args := buildModelWhere(&ModelFilters{})
	assert.Empty(t, where)
	assert.Empty(t, args)

	where, args = buildModelWhere(&ModelFilters{Status: "Running"})
	assert.Equal(t, " WHERE status = $1", where)
	assert.Equal(t, []interface{}{"Running"}, args)
}
