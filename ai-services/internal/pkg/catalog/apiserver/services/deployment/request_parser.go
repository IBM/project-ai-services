package deployment

import (
	"context"
	"fmt"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

// RequestParser parses and validates the create application request.
type RequestParser struct {
	componentRepo repository.ComponentRepository
}

// NewRequestParser creates a new request parser.
func NewRequestParser(componentRepo repository.ComponentRepository) *RequestParser {
	return &RequestParser{
		componentRepo: componentRepo,
	}
}

// ParsedRequest represents the parsed and validated request data.
type ParsedRequest struct {
	ApplicationName string
	CatalogID       string
	IsArchitecture  bool
	Services        map[string]*ParsedService // Key: service_id
}

// ParsedService represents a parsed service from the request.
type ParsedService struct {
	CatalogID  string
	Version    string
	Components map[string]*ParsedComponent // Key: component_type
}

// ParsedComponent represents a parsed component from the request.
type ParsedComponent struct {
	ComponentType string
	ProviderID    string
	Params        map[string]interface{} // Component parameters
}

// ParseRequest parses and organizes the create application request.
// The isArchitecture flag should be determined by the caller based on catalog lookup.
func (p *RequestParser) ParseRequest(
	ctx context.Context,
	req apimodels.CreateApplicationRequest,
	isArchitecture bool,
) (*ParsedRequest, error) {
	parsed := &ParsedRequest{
		ApplicationName: req.Name,
		CatalogID:       req.CatalogID,
		IsArchitecture:  isArchitecture,
		Services:        make(map[string]*ParsedService),
	}

	// Parse each service
	for _, svcReq := range req.Services {
		parsedSvc, err := p.parseService(ctx, svcReq)
		if err != nil {
			return nil, fmt.Errorf("failed to parse service '%s': %w", svcReq.CatalogID, err)
		}
		parsed.Services[svcReq.CatalogID] = parsedSvc
	}

	return parsed, nil
}

// parseService parses a single service from the request.
func (p *RequestParser) parseService(
	ctx context.Context,
	svcReq apimodels.Service,
) (*ParsedService, error) {
	parsedSvc := &ParsedService{
		CatalogID:  svcReq.CatalogID,
		Version:    svcReq.Version,
		Components: make(map[string]*ParsedComponent),
	}

	// Parse each component
	for _, compReq := range svcReq.Components {
		parsedComp, err := p.parseComponent(ctx, compReq)
		if err != nil {
			return nil, fmt.Errorf("failed to parse component '%s': %w", compReq.ComponentType, err)
		}
		parsedSvc.Components[compReq.ComponentType] = parsedComp
	}

	return parsedSvc, nil
}

// parseComponent parses a single component from the request.
func (p *RequestParser) parseComponent(
	ctx context.Context,
	compReq apimodels.Component,
) (*ParsedComponent, error) {
	parsedComp := &ParsedComponent{
		ComponentType: compReq.ComponentType,
		ProviderID:    compReq.ProviderID,
		Params:        compReq.Params,
	}

	return parsedComp, nil
}

// Made with Bob
