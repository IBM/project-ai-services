package repository

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

const modelListOrderBy = " ORDER BY created_at DESC, id ASC"

func TestBuildModelWhere(t *testing.T) {
	tests := []struct {
		name           string
		filters        ModelFilters
		componentTypes []string
		expectedSQL    string
		expectedArgs   []interface{}
	}{
		{
			name:        "no owner and no filters is not narrowed, as for applications",
			filters:     ModelFilters{},
			expectedSQL: "",
		},
		{
			name:         "no owner still applies the other filters",
			filters:      ModelFilters{Status: "Running"},
			expectedSQL:  " WHERE status = $1",
			expectedArgs: []interface{}{"Running"},
		},
		{
			name:         "owner only",
			filters:      ModelFilters{CreatedBy: "alice"},
			expectedSQL:  " WHERE created_by = $1",
			expectedArgs: []interface{}{"alice"},
		},
		{
			name:         "deployment type",
			filters:      ModelFilters{CreatedBy: "alice", DeploymentType: "local"},
			expectedSQL:  " WHERE created_by = $1 AND deployment_type = $2",
			expectedArgs: []interface{}{"alice", "local"},
		},
		{
			name:         "component type",
			filters:      ModelFilters{CreatedBy: "alice", ComponentType: "llm"},
			expectedSQL:  " WHERE created_by = $1 AND component_type = $2",
			expectedArgs: []interface{}{"alice", "llm"},
		},
		{
			name:         "providers",
			filters:      ModelFilters{CreatedBy: "alice", Providers: []string{"vllm-spyre", "watsonx"}},
			expectedSQL:  " WHERE created_by = $1 AND provider = ANY($2)",
			expectedArgs: []interface{}{"alice", []string{"vllm-spyre", "watsonx"}},
		},
		{
			name:         "models",
			filters:      ModelFilters{CreatedBy: "alice", Models: []string{"ibm-granite/granite"}},
			expectedSQL:  " WHERE created_by = $1 AND model = ANY($2)",
			expectedArgs: []interface{}{"alice", []string{"ibm-granite/granite"}},
		},
		{
			name:         "status",
			filters:      ModelFilters{CreatedBy: "alice", Status: "Running"},
			expectedSQL:  " WHERE created_by = $1 AND status = $2",
			expectedArgs: []interface{}{"alice", "Running"},
		},
		{
			name:         "empty multi-value filters are not applied",
			filters:      ModelFilters{CreatedBy: "alice", Providers: []string{}, Models: nil},
			expectedSQL:  " WHERE created_by = $1",
			expectedArgs: []interface{}{"alice"},
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
		{
			name:           "nil component types are unrestricted",
			filters:        ModelFilters{CreatedBy: "alice"},
			componentTypes: nil,
			expectedSQL:    " WHERE created_by = $1",
			expectedArgs:   []interface{}{"alice"},
		},
		{
			name:           "component types alone",
			filters:        ModelFilters{},
			componentTypes: []string{"llm", "embedding"},
			expectedSQL:    " WHERE component_type = ANY($1)",
			expectedArgs:   []interface{}{[]string{"llm", "embedding"}},
		},
		{
			name:           "empty component types match nothing instead of everything",
			filters:        ModelFilters{},
			componentTypes: []string{},
			expectedSQL:    " WHERE component_type = ANY($1)",
			expectedArgs:   []interface{}{[]string{}},
		},
		{
			name:           "component types follow the owner and precede the other filters",
			filters:        ModelFilters{CreatedBy: "alice", ComponentType: "llm"},
			componentTypes: []string{"llm", "embedding"},
			expectedSQL:    " WHERE created_by = $1 AND component_type = ANY($2) AND component_type = $3",
			expectedArgs:   []interface{}{"alice", []string{"llm", "embedding"}, "llm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildModelWhere(&tt.filters, tt.componentTypes)

			assert.Equal(t, tt.expectedSQL, where)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

func TestBuildModelListQuery(t *testing.T) {
	tests := []struct {
		name           string
		filters        ModelFilters
		componentTypes []string
		expectedTail   string // everything after the union subquery: alias, WHERE, ORDER BY, paging
		expectedArgs   []interface{}
	}{
		{
			name:         "owner only has no paging",
			filters:      ModelFilters{CreatedBy: "alice"},
			expectedTail: ") m WHERE created_by = $1" + modelListOrderBy,
			expectedArgs: []interface{}{"alice"},
		},
		{
			name:         "limit only",
			filters:      ModelFilters{CreatedBy: "alice", Limit: 20},
			expectedTail: ") m WHERE created_by = $1" + modelListOrderBy + " LIMIT $2",
			expectedArgs: []interface{}{"alice", 20},
		},
		{
			name:         "offset only",
			filters:      ModelFilters{CreatedBy: "alice", Offset: 40},
			expectedTail: ") m WHERE created_by = $1" + modelListOrderBy + " OFFSET $2",
			expectedArgs: []interface{}{"alice", 40},
		},
		{
			name:         "limit and offset",
			filters:      ModelFilters{CreatedBy: "alice", Limit: 20, Offset: 40},
			expectedTail: ") m WHERE created_by = $1" + modelListOrderBy + " LIMIT $2 OFFSET $3",
			expectedArgs: []interface{}{"alice", 20, 40},
		},
		{
			name:         "zero limit and offset are not applied",
			filters:      ModelFilters{CreatedBy: "alice", Limit: 0, Offset: 0},
			expectedTail: ") m WHERE created_by = $1" + modelListOrderBy,
			expectedArgs: []interface{}{"alice"},
		},
		{
			name:         "deployment type filter",
			filters:      ModelFilters{CreatedBy: "alice", DeploymentType: "remote"},
			expectedTail: ") m WHERE created_by = $1 AND deployment_type = $2" + modelListOrderBy,
			expectedArgs: []interface{}{"alice", "remote"},
		},
		{
			name:         "component type filter",
			filters:      ModelFilters{CreatedBy: "alice", ComponentType: "llm"},
			expectedTail: ") m WHERE created_by = $1 AND component_type = $2" + modelListOrderBy,
			expectedArgs: []interface{}{"alice", "llm"},
		},
		{
			name:         "provider filter",
			filters:      ModelFilters{CreatedBy: "alice", Providers: []string{"vllm-spyre"}},
			expectedTail: ") m WHERE created_by = $1 AND provider = ANY($2)" + modelListOrderBy,
			expectedArgs: []interface{}{"alice", []string{"vllm-spyre"}},
		},
		{
			name:         "model filter",
			filters:      ModelFilters{CreatedBy: "alice", Models: []string{"ibm-granite/granite"}},
			expectedTail: ") m WHERE created_by = $1 AND model = ANY($2)" + modelListOrderBy,
			expectedArgs: []interface{}{"alice", []string{"ibm-granite/granite"}},
		},
		{
			name:         "status filter",
			filters:      ModelFilters{CreatedBy: "alice", Status: "Running"},
			expectedTail: ") m WHERE created_by = $1 AND status = $2" + modelListOrderBy,
			expectedArgs: []interface{}{"alice", "Running"},
		},
		{
			name:         "filters are followed by paging placeholders",
			filters:      ModelFilters{CreatedBy: "alice", Status: "Running", Providers: []string{"watsonx"}, Limit: 10, Offset: 10},
			expectedTail: ") m WHERE created_by = $1 AND provider = ANY($2) AND status = $3" + modelListOrderBy + " LIMIT $4 OFFSET $5",
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
				" AND provider = ANY($4) AND model = ANY($5) AND status = $6" + modelListOrderBy + " LIMIT $7 OFFSET $8",
			expectedArgs: []interface{}{"alice", "local", "llm", []string{"vllm-spyre"}, []string{"ibm-granite/granite"}, "Running", 50, 100},
		},
		{
			name: "several providers and models are one clause each",
			filters: ModelFilters{
				CreatedBy: "alice", Providers: []string{"vllm-spyre", "watsonx"},
				Models: []string{"ibm/granite-4-h-small", "ibm-granite/granite-3.3-8b-instruct"},
			},
			expectedTail: ") m WHERE created_by = $1 AND provider = ANY($2) AND model = ANY($3)" + modelListOrderBy,
			expectedArgs: []interface{}{
				"alice", []string{"vllm-spyre", "watsonx"},
				[]string{"ibm/granite-4-h-small", "ibm-granite/granite-3.3-8b-instruct"},
			},
		},
		{
			name:         "empty multi-value filters are not applied",
			filters:      ModelFilters{CreatedBy: "alice", Providers: []string{}, Models: nil},
			expectedTail: ") m WHERE created_by = $1" + modelListOrderBy,
			expectedArgs: []interface{}{"alice"},
		},
		{
			name:           "component types are bound before the filters and the paging",
			filters:        ModelFilters{CreatedBy: "alice", Status: "Running", Limit: 20, Offset: 40},
			componentTypes: []string{"llm", "embedding"},
			expectedTail:   ") m WHERE created_by = $1 AND component_type = ANY($2) AND status = $3" + modelListOrderBy + " LIMIT $4 OFFSET $5",
			expectedArgs:   []interface{}{"alice", []string{"llm", "embedding"}, "Running", 20, 40},
		},
		{
			name:           "empty component types match nothing",
			filters:        ModelFilters{CreatedBy: "alice"},
			componentTypes: []string{},
			expectedTail:   ") m WHERE created_by = $1 AND component_type = ANY($2)" + modelListOrderBy,
			expectedArgs:   []interface{}{"alice", []string{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildModelListQuery(&tt.filters, tt.componentTypes)

			expectedQuery := "SELECT " + modelColumns + " FROM (" + modelUnionQuery + tt.expectedTail
			assert.Equal(t, expectedQuery, query)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

// The union must read both tables, and only the rows that can be models.
func TestBuildModelListQuery_Union(t *testing.T) {
	tests := []struct {
		name     string
		contains bool
		fragment string
	}{
		{"reads both tables", true, "UNION ALL"},
		{"local models come from components", true, "FROM components c"},
		{"local models carry their worker", true, "LEFT JOIN workers w ON w.id = c.worker_id"},
		{"remote models come from connectors", true, "FROM connectors cn"},
		{"datasource connectors stay out of the list", true, "cn.type = 'models'"},
		{"components are not picked by name", false, "c.name IS NOT NULL"},
		{"newest first with a stable tie-breaker", true, "ORDER BY created_at DESC, id ASC"},
	}

	query, _ := buildModelListQuery(&ModelFilters{CreatedBy: "alice"}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.contains {
				assert.Contains(t, query, tt.fragment)
			} else {
				assert.NotContains(t, query, tt.fragment)
			}
		})
	}
}

// User input must only ever travel as a bound argument, never inside the SQL text.
func TestBuildModelListQuery_InputIsBoundNotInlined(t *testing.T) {
	const hostile = "x' OR '1'='1"

	tests := []struct {
		name           string
		filters        ModelFilters
		componentTypes []string
		expectedArg    interface{}
	}{
		{"owner", ModelFilters{CreatedBy: hostile}, nil, hostile},
		{"deployment type", ModelFilters{DeploymentType: hostile}, nil, hostile},
		{"component type", ModelFilters{ComponentType: hostile}, nil, hostile},
		{"status", ModelFilters{Status: hostile}, nil, hostile},
		{"providers", ModelFilters{Providers: []string{hostile}}, nil, []string{hostile}},
		{"models", ModelFilters{Models: []string{hostile}}, nil, []string{hostile}},
		{"component types", ModelFilters{}, []string{hostile}, []string{hostile}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildModelListQuery(&tt.filters, tt.componentTypes)

			assert.NotContains(t, query, hostile)
			assert.Contains(t, args, tt.expectedArg)
		})
	}
}

// The count must filter exactly like the list, otherwise total_pages would not match the data.
func TestBuildModelCountQuery_SharesUnionAndFilters(t *testing.T) {
	tests := []struct {
		name               string
		filters            ModelFilters
		componentTypes     []string
		expectedWhere      string
		expectedCountArgs  []interface{}
		expectedListSuffix []interface{} // what the list appends for paging
	}{
		{
			name:               "owner and status with paging",
			filters:            ModelFilters{CreatedBy: "alice", Status: "connected", Limit: 20, Offset: 20},
			expectedWhere:      " WHERE created_by = $1 AND status = $2",
			expectedCountArgs:  []interface{}{"alice", "connected"},
			expectedListSuffix: []interface{}{20, 20},
		},
		{
			name:               "component types",
			filters:            ModelFilters{CreatedBy: "alice", Limit: 10},
			componentTypes:     []string{"llm", "embedding"},
			expectedWhere:      " WHERE created_by = $1 AND component_type = ANY($2)",
			expectedCountArgs:  []interface{}{"alice", []string{"llm", "embedding"}},
			expectedListSuffix: []interface{}{10},
		},
		{
			name:               "empty component types match nothing in both",
			filters:            ModelFilters{CreatedBy: "alice"},
			componentTypes:     []string{},
			expectedWhere:      " WHERE created_by = $1 AND component_type = ANY($2)",
			expectedCountArgs:  []interface{}{"alice", []string{}},
			expectedListSuffix: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listQuery, listArgs := buildModelListQuery(&tt.filters, tt.componentTypes)
			countQuery, countArgs := buildModelCountQuery(&tt.filters, tt.componentTypes)

			assert.True(t, strings.HasPrefix(countQuery, "SELECT COUNT(*) FROM ("))
			assert.Contains(t, countQuery, "UNION ALL")
			assert.Contains(t, countQuery, tt.expectedWhere)
			assert.Contains(t, listQuery, tt.expectedWhere)
			assert.NotContains(t, countQuery, "LIMIT")
			assert.NotContains(t, countQuery, "ORDER BY")
			assert.Equal(t, tt.expectedCountArgs, countArgs)
			assert.Equal(t, append(append([]interface{}{}, tt.expectedCountArgs...), tt.expectedListSuffix...), listArgs)
		})
	}
}

func TestBuildModelGetQuery(t *testing.T) {
	id := uuid.New()
	base := "SELECT " + modelColumns + " FROM (" + modelUnionQuery + ") m WHERE id = $1"

	tests := []struct {
		name           string
		componentTypes []string
		expectedQuery  string
		expectedArgs   []interface{}
	}{
		{
			name:          "nil component types are unrestricted",
			expectedQuery: base,
			expectedArgs:  []interface{}{id},
		},
		{
			name:           "component types limit the lookup to models",
			componentTypes: []string{"llm", "embedding"},
			expectedQuery:  base + " AND component_type = ANY($2)",
			expectedArgs:   []interface{}{id, []string{"llm", "embedding"}},
		},
		{
			name:           "empty component types match nothing",
			componentTypes: []string{},
			expectedQuery:  base + " AND component_type = ANY($2)",
			expectedArgs:   []interface{}{id, []string{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildModelGetQuery(id, tt.componentTypes)

			assert.Equal(t, tt.expectedQuery, query)
			assert.Equal(t, tt.expectedArgs, args, "the id and the types are bound")
			assert.Contains(t, query, "UNION ALL", "a model is looked up in both tables")
		})
	}
}
