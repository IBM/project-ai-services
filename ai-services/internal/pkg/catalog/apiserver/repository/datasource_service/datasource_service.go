package datasourceservice

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	// ErrMsgDatasourceNameExists is returned when a connector with the given name already exists.
	ErrMsgDatasourceNameExists = "Datasource with name %q already exists"
)

// ValidationError re-exported so callers use the same type as for application errors.
type ValidationError = validators.ValidationError

// DatasourceService is the single implementation of the create-datasource flow.
// It is provider-agnostic: provider-specific behaviour (connection testing) is
// delegated to a ConnectionTester looked up from the testers registry.
// Sensitive-field identification is derived at runtime from each provider's
// schema.json, keyed on format: "password".
type DatasourceService struct {
	connectorRepo   dbrepo.ConnectorRepository
	svcDepRepo      dbrepo.ServiceDependencyRepository
	validator       *validators.ConnectorValidator
	catalogProvider *catalog.CatalogProvider
	encryptionKey   string
	// testers maps providerID → ConnectionTester. Populated by NewDatasourceService.
	testers map[string]ConnectionTester
}

// NewDatasourceService creates a DatasourceService wired with all known provider testers.
// encryptionKey is the AES-256 key used to encrypt sensitive credential fields; it is
// injected by the caller (read from DB_ENCRYPTION_KEY at startup) rather than fetched
// from the environment at call time.
func NewDatasourceService(
	connectorRepo dbrepo.ConnectorRepository,
	svcDepRepo dbrepo.ServiceDependencyRepository,
	validator *validators.ConnectorValidator,
	catalogProvider *catalog.CatalogProvider,
	encryptionKey string,
) *DatasourceService {
	return &DatasourceService{
		connectorRepo:   connectorRepo,
		svcDepRepo:      svcDepRepo,
		validator:       validator,
		catalogProvider: catalogProvider,
		encryptionKey:   encryptionKey,
		testers: map[string]ConnectionTester{
			catalogconstants.DatasourceProviderObjectStorage: NewObjectStorageTester(),
			catalogconstants.DatasourceProviderFileSystem:    NewFileSystemTester(),
		},
	}
}

// CreateDatasource is the single create flow shared by all providers:
//
//  1. Validate the request body (provider existence + JSON-schema param validation).
//  2. Duplicate-name guard (case-insensitive — handled by LOWER() in the DB query).
//  3. Test the connection — the outcome sets the initial connector status.
//  4. Encrypt sensitive credential fields derived from the provider's schema.json.
//  5. Persist the connector record.
func (s *DatasourceService) CreateDatasource(ctx context.Context, req apimodels.CreateDatasourceRequest) (*apimodels.CreateDatasourceResponse, error) {
	// Phase 1: validate request (provider existence + param schema).
	if err := s.validator.ValidateCreateDatasourceRequest(ctx, req); err != nil {
		return nil, err
	}

	// Phase 2: duplicate-name guard (case-insensitive — handled by LOWER() in the DB query).
	existing, err := s.connectorRepo.GetByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing connector: %w", err)
	}
	if existing != nil {
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf(ErrMsgDatasourceNameExists, req.Name),
		}
	}

	// Phase 3: test connection — determines the initial connector status.
	tester, ok := s.testers[req.ProviderID]
	if !ok {
		// Should not happen after Phase 1 validation, but guard defensively.
		return nil, &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("No connection tester registered for provider %q", req.ProviderID),
		}
	}

	testErr := tester.TestConnection(ctx, req.Params)
	if testErr != nil {
		return nil, &ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("Connection test failed: %v", testErr),
		}
	}

	// Phase 4: derive sensitive fields from the provider's schema.json and encrypt.
	rawSchema, err := s.catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for provider %q: %w", req.ProviderID, err)
	}

	schema, err := pkgutils.ConvertRawJsontoMap(rawSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schema for provider %q: %w", req.ProviderID, err)
	}

	encryptedParams, err := encryptSensitiveFields(req.Params, sensitiveFieldsFromSchema(schema), s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt connector credentials: %w", err)
	}

	// Phase 5: persist the connector record.
	connector := &dbmodels.Connector{
		Name:      req.Name,
		Type:      catalogconstants.ConnectorTypeDatasource,
		Provider:  req.ProviderID,
		Status:    dbmodels.ConnectorStatusConnected,
		Metadata:  encryptedParams,
		CreatedBy: req.CreatedBy,
	}

	if err := s.connectorRepo.Insert(ctx, connector); err != nil {
		return nil, fmt.Errorf("failed to persist connector: %w", err)
	}

	return &apimodels.CreateDatasourceResponse{ID: connector.ID.String()}, nil
}

// GetDatasource retrieves a single datasource connector by UUID, returns its non-sensitive
// metadata, and enriches it with a list of connected services sourced from service_dependencies,
// each augmented with live sync state from the service's Digitize pod.
//
// Flow:
//  1. Fetch the connector (without credentials) — return 404 if absent.
//  2. Resolve the sensitive fields for this provider so they can be stripped from metadata.
//  3. Query service_dependencies for all services linked to this connector.
//  4. For each linked service, call GET /v1/connectors/{id} on its Digitize pod to obtain
//     sync_status and last_sync_at. Failures degrade gracefully to sync_status="unknown".
func (s *DatasourceService) GetDatasource(ctx context.Context, id uuid.UUID) (*apimodels.GetDatasourceResponse, error) {
	// Step 1: fetch connector including metadata so non-sensitive fields can be returned.
	// Sensitive fields are identified from the provider schema and stripped before the response
	// is built; they are never forwarded to the caller.
	connector, err := s.connectorRepo.GetByID(ctx, id, true)
	if err != nil {
		if err == dbrepo.ErrConnectorNotFound {
			return nil, &ValidationError{
				Code:    http.StatusNotFound,
				Message: "datasource not found",
			}
		}

		return nil, fmt.Errorf("failed to fetch datasource: %w", err)
	}

	// Step 2: resolve provider display name and load schema to derive sensitive fields for stripping.
	providerName := connector.Provider
	if catalogConn, loadErr := s.catalogProvider.LoadConnector(catalogconstants.ConnectorTypeDatasource, connector.Provider); loadErr == nil {
		providerName = catalogConn.Name
	}

	rawSchema, err := s.catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, connector.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for provider %q: %w", connector.Provider, err)
	}

	schema, err := pkgutils.ConvertRawJsontoMap(rawSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schema for provider %q: %w", connector.Provider, err)
	}

	sensitiveFields := sensitiveFieldsFromSchema(schema)

	// Steps 3–4: fetch linked services and enrich each with live Digitize sync state.
	services := s.buildConnectedServices(ctx, id)

	return &apimodels.GetDatasourceResponse{
		ID:   connector.ID.String(),
		Name: connector.Name,
		Type: connector.Type,
		Provider: apimodels.DatasourceProviderInfo{
			ID:   connector.Provider,
			Name: providerName,
		},
		Status:    string(connector.Status),
		Message:   connector.Message,
		Metadata:  stripSensitiveFields(connector.Metadata, sensitiveFields),
		Services:  services,
		CreatedAt: connector.CreatedAt,
		UpdatedAt: connector.UpdatedAt,
	}, nil
}

// ListDatasources returns a paginated list of datasource connectors, optionally filtered
// by status and provider. Pagination mirrors the application and bundle list patterns:
// uses repository.ConnectorFilters with Limit/Offset derived from Page/PageSize.
func (s *DatasourceService) ListDatasources(ctx context.Context, req apimodels.ListDatasourcesRequest) (*apimodels.DatasourceListResponse, error) {
	filters := &dbrepo.ConnectorFilters{
		Type:     catalogconstants.ConnectorTypeDatasource,
		Status:   dbmodels.ConnectorStatus(req.Status),
		Provider: req.Provider,
		Limit:    req.PageSize,
		Offset:   (req.Page - 1) * req.PageSize,
	}

	totalCount, err := s.connectorRepo.GetCount(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to count datasources: %w", err)
	}

	connectors, err := s.connectorRepo.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list datasources: %w", err)
	}

	// Collect connector IDs so connected-service counts can be fetched in one query.
	connectorIDs := make([]uuid.UUID, len(connectors))
	for i := range connectors {
		connectorIDs[i] = connectors[i].ID
	}

	serviceCounts, err := s.svcDepRepo.GetServiceCountByDependency(ctx, connectorIDs, dbmodels.DependencyTypeConnector)
	if err != nil {
		return nil, fmt.Errorf("failed to count connected services: %w", err)
	}

	data := make([]apimodels.DatasourceResponse, 0, len(connectors))
	for i := range connectors {
		data = append(data, *s.connectorToResponse(&connectors[i], serviceCounts[connectors[i].ID]))
	}

	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + req.PageSize - 1) / req.PageSize
	}

	return &apimodels.DatasourceListResponse{
		Data: data,
		Pagination: catalogtypes.PaginationMetadata{
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalItems: totalCount,
			TotalPages: totalPages,
			HasNext:    req.Page < totalPages,
			HasPrev:    req.Page > 1,
		},
	}, nil
}

// connectorToResponse converts a DB Connector model to a DatasourceResponse API model.
// The provider name is resolved from the catalog; if unavailable (e.g. provider removed from
// catalog), the name falls back to the stored provider ID so the response is never incomplete.
// connectedServices is passed in directly from the List query's COUNT projection; callers
// that do not have this value (GetByID) pass 0.
func (s *DatasourceService) connectorToResponse(c *dbmodels.Connector, connectedServices int) *apimodels.DatasourceResponse {
	providerName := c.Provider
	if catalogConnector, err := s.catalogProvider.LoadConnector(catalogconstants.ConnectorTypeDatasource, c.Provider); err == nil {
		providerName = catalogConnector.Name
	}

	return &apimodels.DatasourceResponse{
		ID:   c.ID.String(),
		Name: c.Name,
		Type: c.Type,
		Provider: apimodels.DatasourceProviderInfo{
			ID:   c.Provider,
			Name: providerName,
		},
		Status:            string(c.Status),
		Message:           c.Message,
		ConnectedServices: connectedServices,
		CreatedAt:         c.CreatedAt.Format(catalogconstants.RFC3339WithTimezone),
		UpdatedAt:         c.UpdatedAt.Format(catalogconstants.RFC3339WithTimezone),
	}
}

// buildConnectedServices queries service_dependencies for all services linked to the given
// connector and enriches each with live sync state from its Digitize pod.
// GetLinkedServiceEndpoints issues a single JOIN query (service_dependencies → services →
// applications) returning the application identity and the first "api"-typed endpoint URL.
// A query failure is non-fatal: a warning is logged and an empty slice is returned so that
// the caller can still return the connector record successfully.
func (s *DatasourceService) buildConnectedServices(ctx context.Context, connectorID uuid.UUID) []apimodels.ConnectedServiceItem {
	linkedServices, err := s.svcDepRepo.GetLinkedServiceEndpoints(
		ctx,
		connectorID,
		dbmodels.DependencyTypeConnector,
	)
	if err != nil {
		logger.WarningfCtx(ctx, "failed to query linked services for datasource %s: %v", connectorID, err)

		return nil
	}

	services := make([]apimodels.ConnectedServiceItem, 0, len(linkedServices))
	for _, svc := range linkedServices {
		syncStatus, lastSyncAt := fetchDigitzeSyncState(ctx, connectorID, svc.URL)
		services = append(services, apimodels.ConnectedServiceItem{
			ServiceID:       svc.ServiceID.String(),
			ApplicationID:   svc.ApplicationID.String(),
			ApplicationName: svc.ApplicationName,
			Service:         s.resolveServiceInfo(svc.ApplicationCatalogID, svc.ApplicationDeploymentType),
			SyncStatus:      syncStatus,
			LastSyncAt:      lastSyncAt,
		})
	}

	return services
}

// resolveServiceInfo builds a ConnectedServiceInfo by loading the display name from catalog
// metadata for the given catalogID + deploymentType. Falls back gracefully: when the catalog
// entry cannot be loaded the id is used as the name so the response is never blocked.
func (s *DatasourceService) resolveServiceInfo(catalogID, deploymentType string) apimodels.ConnectedServiceInfo {
	info := apimodels.ConnectedServiceInfo{ID: catalogID, Name: catalogID}

	if deploymentType == string(dbmodels.DeploymentTypeArchitectures) {
		if arch, err := s.catalogProvider.LoadArchitecture(catalogID); err == nil {
			info.Name = arch.Name
		}
	} else {
		if svc, err := s.catalogProvider.LoadService(catalogID); err == nil {
			info.Name = svc.Name
		}
	}

	return info
}

// encryptSensitiveFields returns a copy of params where every key listed in
// sensitiveKeys has its string value replaced with an AES-256-GCM ciphertext.
// encryptionKey is the AES-256 secret injected at service construction time (DB_ENCRYPTION_KEY).
func encryptSensitiveFields(params map[string]any, sensitiveKeys map[string]bool, encryptionKey string) (map[string]any, error) {
	if len(sensitiveKeys) == 0 {
		return params, nil
	}

	if encryptionKey == "" {
		return nil, fmt.Errorf("encryption key is not configured (DB_ENCRYPTION_KEY must be set)")
	}

	result := make(map[string]any, len(params))
	for k, v := range params {
		if sensitiveKeys[k] {
			plaintext, ok := v.(string)
			if !ok {
				return nil, &ValidationError{
					Code:    http.StatusBadRequest,
					Message: fmt.Sprintf("sensitive field %q must be a string value", k),
				}
			}

			ciphertext, err := catalogutils.Encrypt(plaintext, encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt field %q: %w", k, err)
			}

			result[k] = ciphertext
		} else {
			result[k] = v
		}
	}

	return result, nil
}

// stripSensitiveFields returns a copy of metadata with all keys listed in sensitiveFields removed.
// The original map is never mutated. A nil or empty metadata returns an empty map.
func stripSensitiveFields(metadata map[string]any, sensitiveFields map[string]bool) map[string]any {
	result := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if !sensitiveFields[k] {
			result[k] = v
		}
	}

	return result
}

// fetchDigitzeSyncState calls GET /v1/connectors/{connectorID} on the Digitize pod at baseURL
// and returns the sync_status and last_sync_at values.
// If the Digitize pod is unreachable or baseURL is empty, it degrades gracefully:
// sync_status is set to "unknown" and last_sync_at is nil. No error is returned to the caller
// because sync state degradation must never prevent the catalog record from being returned.
func fetchDigitzeSyncState(ctx context.Context, connectorID uuid.UUID, baseURL string) (syncStatus string, lastSyncAt *string) {
	if baseURL == "" {
		return "unknown", nil
	}

	syncStatus, lastSyncAt, err := fetchConnectorSyncFromDigitize(ctx, baseURL, connectorID.String())
	if err != nil {
		return "unknown", nil
	}

	return syncStatus, lastSyncAt
}

// Made with Bob
