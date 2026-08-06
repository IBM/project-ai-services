package openapi

import (
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/project-ai-services/mcp/internal/types"
	base "github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Interface represents a processed OpenAPI specification
type Interface struct {
	Doc           *v3.Document
	Name          string
	Operations    []types.OperationInfo
	Tags          []string
	RegionServers []types.RegionServer
}

// NewInterface creates a new Interface from an OpenAPI document
func NewInterface(doc *v3.Document) *Interface {
	intf := &Interface{
		Doc:           doc,
		Name:          canonicalizeName(doc.Info.Title),
		Operations:    []types.OperationInfo{},
		Tags:          []string{},
		RegionServers: []types.RegionServer{},
	}

	// Ensure the name includes "ibm" prefix
	if !strings.Contains(intf.Name, "ibm") {
		prefix := "ibm-"
		if !strings.Contains(intf.Name, "cloud") {
			prefix += "cloud-"
		}
		intf.Name = prefix + intf.Name
	}

	intf.extractRegionServers()
	intf.collectOperations()
	intf.collectTags()

	return intf
}

// canonicalizeName converts a name to a canonical format
func canonicalizeName(name string) string {
	// Convert to lowercase, remove special characters, replace spaces with hyphens
	name = strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9\- ]`)
	name = re.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, "-")
	return name
}

// extractRegionServers extracts region information from servers
func (intf *Interface) extractRegionServers() {
	if len(intf.Doc.Servers) > 0 {
		for _, server := range intf.Doc.Servers {
			// Extract region from URL pattern like api.{region}.cloud.ibm.com
			region := "default"
			if match := regexp.MustCompile(`api\.([^.]+)\.`).FindStringSubmatch(server.URL); len(match) > 1 {
				region = match[1]
			}

			intf.RegionServers = append(intf.RegionServers, types.RegionServer{
				Region: region,
				URL:    server.URL,
			})
		}
	}
}

// collectOperations extracts all operations from the OpenAPI spec
func (intf *Interface) collectOperations() {
	if intf.Doc.Paths == nil || intf.Doc.Paths.PathItems == nil {
		return
	}

	for pair := intf.Doc.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		path := pair.Key()
		pathItem := pair.Value()

		if pathItem == nil {
			continue
		}

		// Extract path-level parameters
		var pathParams []types.ParameterInfo
		if len(pathItem.Parameters) > 0 {
			for _, param := range pathItem.Parameters {
				if param != nil {
					var schema *jsonschema.Schema
					if param.Schema != nil {
						schema = ConvertSchemaToJSONSchema(param.Schema)
					}

					required := false
					if param.Required != nil {
						required = *param.Required
					}

					pathParams = append(pathParams, types.ParameterInfo{
						Name:        param.Name,
						In:          param.In,
						Required:    required,
						Schema:      schema,
						Description: param.Description,
					})
				}
			}
		}

		// Process each HTTP method
		operations := map[string]*v3.Operation{
			"get":     pathItem.Get,
			"post":    pathItem.Post,
			"put":     pathItem.Put,
			"delete":  pathItem.Delete,
			"patch":   pathItem.Patch,
			"head":    pathItem.Head,
			"options": pathItem.Options,
			"trace":   pathItem.Trace,
		}

		for methodStr, operation := range operations {
			if operation == nil {
				continue
			}

			method := types.HTTPMethod(strings.ToUpper(methodStr))
			if !types.IsValidMethod(string(method)) {
				continue
			}

			// Collect all parameters (path + operation level)
			var allParams []types.ParameterInfo
			allParams = append(allParams, pathParams...)

			if len(operation.Parameters) > 0 {
				for _, param := range operation.Parameters {
					if param != nil {
						var schema *jsonschema.Schema
						if param.Schema != nil {
							schema = ConvertSchemaToJSONSchema(param.Schema)
						}

						required := false
						if param.Required != nil {
							required = *param.Required
						}

						allParams = append(allParams, types.ParameterInfo{
							Name:        param.Name,
							In:          param.In,
							Required:    required,
							Schema:      schema,
							Description: param.Description,
						})
					}
				}
			}

			// Extract request body info
			var requestBody *types.RequestBodyInfo
			if operation.RequestBody != nil {
				rb := operation.RequestBody

				required := false
				if rb.Required != nil {
					required = *rb.Required
				}

				// Find JSON content type
				var contentType string
				var schema *base.SchemaProxy

				if rb.Content != nil {
					// Prefer merge-patch+json, then any JSON content type
					for pair := rb.Content.First(); pair != nil; pair = pair.Next() {
						ct := pair.Key()
						mediaType := pair.Value()

						if strings.Contains(strings.ToLower(ct), "merge-patch+json") {
							contentType = ct
							schema = mediaType.Schema
							break
						} else if strings.Contains(strings.ToLower(ct), "json") && contentType == "" {
							contentType = ct
							schema = mediaType.Schema
						}
					}
				}

				if contentType != "" && schema != nil {
					jsonSchema := ConvertSchemaToJSONSchema(schema)
					requestBody = &types.RequestBodyInfo{
						Required:    required,
						ContentType: contentType,
						Schema:      jsonSchema,
					}
				}
			}

			// Extract tags
			var tags []string
			if len(operation.Tags) > 0 {
				for _, tag := range operation.Tags {
					tags = append(tags, canonicalizeName(tag))
				}
			}

			operationInfo := types.OperationInfo{
				OperationID: operation.OperationId,
				Method:      method,
				Path:        path,
				Summary:     operation.Summary,
				Description: operation.Description,
				Tags:        tags,
				Parameters:  allParams,
				RequestBody: requestBody,
			}

			intf.Operations = append(intf.Operations, operationInfo)
		}
	}
}

// collectTags extracts all unique tags from the specification
func (intf *Interface) collectTags() {
	tagSet := make(map[string]bool)

	// Add tags from the document level
	if len(intf.Doc.Tags) > 0 {
		for _, tag := range intf.Doc.Tags {
			canonicalTag := canonicalizeName(tag.Name)
			tagSet[canonicalTag] = true
		}
	}

	// Add tags from operations
	for _, op := range intf.Operations {
		for _, tag := range op.Tags {
			tagSet[tag] = true
		}
	}

	// Convert to slice
	for tag := range tagSet {
		intf.Tags = append(intf.Tags, tag)
	}
}
