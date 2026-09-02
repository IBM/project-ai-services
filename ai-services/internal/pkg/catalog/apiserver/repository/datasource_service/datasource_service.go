package datasourceservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
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

	// digitizeSyncStatusSyncing is the Digitize sync_status value for an active sync.
	digitizeSyncStatusSyncing = "syncing"
	// digitizeSyncStatusUpToDate is the Digitize sync_status value when all files are current.
	digitizeSyncStatusUpToDate = "up to date"
	// digitizeSyncStatusOutOfSync is the Digitize sync_status value when an error occurred.
	digitizeSyncStatusOutOfSync = "out of sync"
)

// ValidationError re-exported so callers use the same type as for application errors.
type ValidationError = validators.ValidationError

// ServiceConnectorClientInterface is the contract for sending connector payloads to downstream
// services that accept datasource connectors.
type ServiceConnectorClientInterface interface {
	Connect(ctx context.Context, baseURL string, req apimodels.ConnectDatasourceRequest) error
}

// DatasourceService is the single implementation of the datasource connector business logic.
// It is provider-agnostic: provider-specific behaviour (connection testing and
// sensitive-field identification) is delegated to a ConnectionTester looked up
// from the testers registry.
// Sensitive-field identification is derived at runtime from each provider's
// schema.json, keyed on format: "password".
type DatasourceService struct {
	connectorRepo   dbrepo.ConnectorRepository
	appRepo         dbrepo.ApplicationRepository
	svcDepRepo      dbrepo.ServiceDependencyRepository
	validator       *validators.ConnectorValidator
	catalogProvider *catalog.CatalogProvider
	serviceClient   ServiceConnectorClientInterface
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
	appRepo dbrepo.ApplicationRepository,
	svcDepRepo dbrepo.ServiceDependencyRepository,
	validator *validators.ConnectorValidator,
	catalogProvider *catalog.CatalogProvider,
	serviceClient ServiceConnectorClientInterface,
	encryptionKey string,
) *DatasourceService {
	return &DatasourceService{
		connectorRepo:   connectorRepo,
		appRepo:         appRepo,
		svcDepRepo:      svcDepRepo,
		validator:       validator,
		catalogProvider: catalogProvider,
		serviceClient:   serviceClient,
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
	services, err := s.buildConnectedServices(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to build connected services for datasource %s: %w", id, err)
	}

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
		Metadata:  catalogutils.StripSensitiveFields(connector.Metadata, sensitiveFields),
		Services:  services,
		CreatedAt: connector.CreatedAt,
		UpdatedAt: connector.UpdatedAt,
	}, nil
}

// DeleteDatasource removes a datasource connector by ID.
// Returns 404 if not found, 409 if the connector is still linked to one or more services.
//
// The existence check, in-use check, and DELETE all run inside a single serializable
// transaction (owned by the repository layer) so a concurrent link cannot slip between
// the guard and the delete.
func (s *DatasourceService) DeleteDatasource(ctx context.Context, id uuid.UUID) error {
	linkedCount, err := s.connectorRepo.DeleteIfUnlinked(ctx, id, dbmodels.DependencyTypeConnector)
	if err != nil {
		if errors.Is(err, dbrepo.ErrConnectorNotFound) {
			return &ValidationError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("datasource %q not found", id),
			}
		}

		if errors.Is(err, dbrepo.ErrConnectorInUse) {
			return &ValidationError{
				Code:    http.StatusConflict,
				Message: fmt.Sprintf("datasource is connected to %d application(s) and cannot be deleted", linkedCount),
			}
		}

		return fmt.Errorf("failed to delete datasource: %w", err)
	}

	return nil
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
// applications) returning the raw DB columns. The "api"-typed endpoint URL is extracted
// here from EndpointsJSON. A DB query failure is propagated to the caller.
// Sync-state fetch failures per service are non-fatal: ErrMsg is set on the item so the
// caller receives full context without the connector record being blocked.
func (s *DatasourceService) buildConnectedServices(ctx context.Context, connectorID uuid.UUID) ([]apimodels.ConnectedServiceItem, error) {
	linkedRows, err := s.svcDepRepo.GetLinkedServiceEndpoints(
		ctx,
		connectorID,
		dbmodels.DependencyTypeConnector,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query linked services: %w", err)
	}

	services := make([]apimodels.ConnectedServiceItem, 0, len(linkedRows))
	for _, row := range linkedRows {
		baseURL := extractAPIEndpointURL(row.EndpointsJSON)
		syncStatus, lastSyncAt, syncErr := fetchDigitzeSyncState(ctx, connectorID, baseURL)
		item := apimodels.ConnectedServiceItem{
			ApplicationID:   row.ApplicationID.String(),
			ApplicationName: row.ApplicationName,
			Service:         s.resolveServiceInfo(row.ApplicationCatalogID, row.ApplicationDeploymentType),
			SyncStatus:      syncStatus,
			LastSyncAt:      lastSyncAt,
		}
		if syncErr != "" {
			item.ErrMsg = syncErr
		}
		services = append(services, item)
	}

	return services, nil
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

// ConnectDatasourcesToApplication links one or more datasource connectors to every eligible
// service (AcceptsDatasource == true) in the given application.
//
// Each datasource is processed independently — a failure for one does not abort the others.
// Results are returned per datasource. At least one eligible service must exist; the call
// returns 422 if none are found.
//
// This method is the single reusable core for both:
//   - the PUT /applications/:id/datasources HTTP endpoint (post-creation connect)
//   - the app-creation flow where datasources are pre-attached at deploy time.
func (s *DatasourceService) ConnectDatasourcesToApplication(ctx context.Context, applicationID uuid.UUID, datasourceIDs []uuid.UUID) (*apimodels.ConnectDatasourcesResponse, error) {
	// Resolve eligible services once — shared across all datasources.
	linkedServices, err := s.eligibleServicesForApp(ctx, applicationID)
	if err != nil {
		return nil, err
	}

	if len(linkedServices) == 0 {
		return nil, &ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: "no eligible running service with an API endpoint found in application",
		}
	}

	var connErrors []apimodels.DatasourceConnectionError

	for _, datasourceID := range datasourceIDs {
		_, connectErr := s.connectOneDatasource(ctx, datasourceID, linkedServices)
		if connectErr != nil {
			connErrors = append(connErrors, apimodels.DatasourceConnectionError{
				DatasourceID: datasourceID.String(),
				Error:        connectErr.Error(),
			})
		}
	}

	if len(connErrors) == 0 {
		return nil, nil
	}

	return &apimodels.ConnectDatasourcesResponse{Errors: connErrors}, nil
}

// connectOneDatasource loads, decrypts, and propagates a single datasource connector
// to all eligible services. Returns the last connected service ID on success.
func (s *DatasourceService) connectOneDatasource(ctx context.Context, datasourceID uuid.UUID, linkedServices []dbrepo.LinkedServiceRow) (uuid.UUID, error) {
	connector, err := s.loadConnector(ctx, datasourceID)
	if err != nil {
		return uuid.Nil, err
	}

	connectionDetails, err := s.decryptedConnectionDetails(ctx, connector)
	if err != nil {
		return uuid.Nil, err
	}

	var connectedServiceID uuid.UUID

	for _, svc := range linkedServices {
		if err := s.sendToService(ctx, svc, connector, connectionDetails, datasourceID); err != nil {
			return uuid.Nil, err
		}

		connectedServiceID = svc.ServiceID
	}

	return connectedServiceID, nil
}

// eligibleServicesForApp returns the subset of services in applicationID whose catalog
// entry has AcceptsDatasource == true and which have a registered api-type endpoint.
func (s *DatasourceService) eligibleServicesForApp(ctx context.Context, applicationID uuid.UUID) ([]dbrepo.LinkedServiceRow, error) {
	app, err := s.appRepo.GetByID(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load application: %w", err)
	}

	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("application %s not found", applicationID),
		}
	}

	var eligible []dbrepo.LinkedServiceRow

	for _, svc := range app.Services {
		catalogSvc, loadErr := s.catalogProvider.LoadService(svc.CatalogID)
		if loadErr != nil || !catalogSvc.AcceptsDatasource {
			continue
		}

		endpointsJSON, _ := json.Marshal(svc.Endpoints)
		url := extractAPIEndpointURL(endpointsJSON)
		if url == "" {
			logger.WarningfCtx(ctx, "service %s (%s) accepts datasource but has no API endpoint — skipping", svc.ID, svc.CatalogID)

			continue
		}

		eligible = append(eligible, dbrepo.LinkedServiceRow{
			ServiceID:        svc.ID,
			ServiceCatalogID: svc.CatalogID,
			ApplicationID:    app.ID,
			ApplicationName:  app.Name,
			URL:              url,
		})
	}

	return eligible, nil
}

// loadConnector fetches the connector by ID (with credentials). Returns a typed ValidationError on 404.
func (s *DatasourceService) loadConnector(ctx context.Context, datasourceID uuid.UUID) (*dbmodels.Connector, error) {
	connector, err := s.connectorRepo.GetByID(ctx, datasourceID, true)
	if err != nil {
		if err == dbrepo.ErrConnectorNotFound {
			return nil, &ValidationError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("datasource %s not found", datasourceID),
			}
		}

		return nil, fmt.Errorf("failed to load connector: %w", err)
	}

	return connector, nil
}

// decryptedConnectionDetails loads the provider schema and returns the connector metadata
// with all sensitive (format:"password") fields decrypted in-memory.
func (s *DatasourceService) decryptedConnectionDetails(ctx context.Context, connector *dbmodels.Connector) (map[string]any, error) {
	rawSchema, err := s.catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, connector.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for provider %q: %w", connector.Provider, err)
	}

	schema, err := pkgutils.ConvertRawJsontoMap(rawSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schema for provider %q: %w", connector.Provider, err)
	}

	return catalogutils.DecryptSensitiveFields(connector.Metadata, sensitiveFieldsFromSchema(schema), s.encryptionKey)
}

// sendToService POSTs the connector payload to a single eligible service and records the
// service_dependency row. Returns a *ValidationError on downstream failure.
func (s *DatasourceService) sendToService(
	ctx context.Context,
	svc dbrepo.LinkedServiceRow,
	connector *dbmodels.Connector,
	connectionDetails map[string]any,
	datasourceID uuid.UUID,
) error {
	if svc.URL == "" {
		return fmt.Errorf("service %s (%s) accepts datasource but has no API endpoint — skipping", svc.ServiceID, svc.ServiceCatalogID)
	}

	// Extract allowed_extensions from connection_details — stored there during CreateDatasource.
	var allowedExtensions []string
	if raw, ok := connectionDetails["allowed_extensions"]; ok {
		if exts, ok := raw.([]any); ok {
			for _, e := range exts {
				if s, ok := e.(string); ok {
					allowedExtensions = append(allowedExtensions, s)
				}
			}
		}
	}

	connectReq := apimodels.ConnectDatasourceRequest{
		ID:                connector.ID.String(),
		Name:              connector.Name,
		Type:              connector.Provider,
		AllowedExtensions: allowedExtensions,
		ConnectionDetails: connectionDetails,
	}

	if err := s.serviceClient.Connect(ctx, svc.URL, connectReq); err != nil {
		return &ValidationError{
			Code:    http.StatusBadGateway,
			Message: fmt.Sprintf("failed to connect datasource to service %s: %v", svc.ServiceCatalogID, err),
		}
	}

	// Record the connector dependency so it survives restarts.
	dep := &dbmodels.ServiceDependency{
		ServiceID:      svc.ServiceID,
		DependencyID:   datasourceID,
		DependencyType: dbmodels.DependencyTypeConnector,
	}

	if depErr := s.svcDepRepo.AddDependency(ctx, dep); depErr != nil {
		logger.ErrorfCtx(ctx, "failed to record connector dependency for service %s: %v", svc.ServiceID, depErr)
	}

	return nil
}

// UpdateDatasource updates only the updatable credential fields for a datasource.
// Updatable fields are those whose ui:section is "Authentication" in the provider's schema.json.
// Sensitive fields (format: "password") are derived from the same schema, consistent with Create.
//
// The update flow:
//  1. Fetch the existing connector (with encrypted credentials).
//  2. Load the provider schema to derive updatable and sensitive fields.
//  3. Filter the request to only the updatable fields for this provider.
//  4. Decrypt the existing metadata to obtain the full current field set.
//  5. Merge: start from the existing decrypted metadata, then overlay the filtered updates.
//  6. Run the connectivity test against the merged (full) metadata.
//  7. If the test fails, return 422 — the record is left unchanged.
//  8. Encrypt, persist, propagate, and return via persistAndPropagate.
func (s *DatasourceService) UpdateDatasource(ctx context.Context, id uuid.UUID, req apimodels.UpdateDatasourceRequest) (*apimodels.UpdateDatasourceResponse, error) {
	// Phase 1: fetch existing connector (metadata required for merging and decryption).
	existing, err := s.connectorRepo.GetByID(ctx, id, true)
	if err != nil {
		if errors.Is(err, dbrepo.ErrConnectorNotFound) {
			return nil, &ValidationError{Code: http.StatusNotFound, Message: "datasource not found"}
		}

		return nil, fmt.Errorf("failed to fetch existing connector: %w", err)
	}

	// Phase 2: load provider schema to derive updatable and sensitive fields.
	rawSchema, err := s.catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, existing.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for provider %q: %w", existing.Provider, err)
	}

	schema, err := pkgutils.ConvertRawJsontoMap(rawSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schema for provider %q: %w", existing.Provider, err)
	}

	updatable := updatableFieldsFromSchema(schema)
	sensitive := sensitiveFieldsFromSchema(schema)

	// Phase 3: look up the ConnectionTester for this provider.
	tester, ok := s.testers[existing.Provider]
	if !ok {
		// Should not happen in normal operation — means the stored provider ID has no registered
		// tester (server-side misconfiguration). Log it so it is diagnosable.
		logger.ErrorfCtx(ctx, "no connection tester registered for provider %q on connector %s", existing.Provider, id)

		return nil, fmt.Errorf("no connection tester registered for provider %q", existing.Provider)
	}

	// Phase 4: filter the request to only the updatable fields for this provider.
	// Return 400 if the caller supplied only structural (immutable) fields — there is nothing
	// to update and running a connectivity test would be misleading.
	filteredUpdates := filterUpdatableFields(req.Params, updatable)
	if len(filteredUpdates) == 0 {
		return nil, &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("request contains no updatable fields for provider %q; updatable fields are the Authentication parameters defined in the provider schema", existing.Provider),
		}
	}

	// Phase 5: decrypt existing metadata to get the full current field set.
	decryptedExisting, err := catalogutils.DecryptSensitiveFields(existing.Metadata, sensitive, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt existing connector credentials: %w", err)
	}

	// Phase 6: merge — start from the existing full metadata, then overlay the filtered updates.
	merged := pkgutils.MergeMaps(decryptedExisting, filteredUpdates)

	// Phase 7: connectivity test with the merged metadata.
	if testErr := tester.TestConnection(ctx, merged); testErr != nil {
		return nil, &ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("Connection test failed: %v", testErr),
		}
	}

	// Phase 8: encrypt, persist, propagate to Digitize, and build the response.
	return s.persistAndPropagate(ctx, id, merged, sensitive, updatable)
}

// GetApplicationDatasource returns the catalog identity and live sync state for
// a datasource linked to the given application.
//
// Flow:
//  1. Verify the application exists — returns 404 if not found.
//  2. Verify the datasource connector exists — returns 404 if not found.
//  3. Scan service_dependencies rows for this datasource; on the first row whose
//     applicationID matches, capture the endpoint URL and break. Returns 404 when
//     no matching row is found — meaning the datasource is not connected to this application.
//  4. Resolve provider display name via CatalogProvider.LoadConnector; falls back to the
//     stored provider ID so the response is never blocked by a missing catalog entry.
//  5. Fetch live sync state using the captured endpoint URL; degrades gracefully to
//     sync_status="unknown" when the service is unreachable.
func (s *DatasourceService) GetApplicationDatasource(ctx context.Context, applicationID, datasourceID uuid.UUID) (*apimodels.GetApplicationDatasourceResponse, error) {
	// Step 1: verify the application exists.
	app, err := s.appRepo.GetByID(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load application: %w", err)
	}

	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("application %s not found", applicationID),
		}
	}

	// Step 2: verify the datasource connector exists (credentials not needed for this response).
	connector, err := s.connectorRepo.GetByID(ctx, datasourceID, false)
	if err != nil {
		if err == dbrepo.ErrConnectorNotFound {
			return nil, &ValidationError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("datasource %s not found", datasourceID),
			}
		}

		return nil, fmt.Errorf("failed to fetch datasource: %w", err)
	}

	// Step 3: confirm the datasource is linked to this application and capture its endpoint URL.
	baseURL, err := s.resolveLinkedEndpoint(ctx, applicationID, datasourceID)
	if err != nil {
		return nil, err
	}

	// Step 4: resolve provider display name; fall back to stored provider ID on catalog miss.
	providerName := connector.Provider
	if catalogConn, loadErr := s.catalogProvider.LoadConnector(catalogconstants.ConnectorTypeDatasource, connector.Provider); loadErr == nil {
		providerName = catalogConn.Name
	}

	// Step 5: fetch live sync state using the endpoint URL captured in step 3.
	// Degrades gracefully to sync_status="unknown" when the service is unreachable.
	serviceDetails := fetchServiceSyncDetails(ctx, datasourceID, baseURL)

	return &apimodels.GetApplicationDatasourceResponse{
		ID:             connector.ID.String(),
		Name:           connector.Name,
		Status:         string(connector.Status),
		Message:        connector.Message,
		Provider:       apimodels.DatasourceProviderInfo{ID: connector.Provider, Name: providerName},
		ServiceDetails: serviceDetails,
	}, nil
}

// resolveLinkedEndpoint confirms that datasourceID is linked to a service belonging to
// applicationID and returns the API endpoint URL for that service. A datasource can
// appear at most once per application, so the loop breaks on the first match.
func (s *DatasourceService) resolveLinkedEndpoint(ctx context.Context, applicationID, datasourceID uuid.UUID) (string, error) {
	allRows, err := s.svcDepRepo.GetLinkedServiceEndpoints(ctx, datasourceID, dbmodels.DependencyTypeConnector)
	if err != nil {
		return "", fmt.Errorf("failed to query linked services for datasource %s: %w", datasourceID, err)
	}

	for _, row := range allRows {
		if row.ApplicationID == applicationID {
			return extractAPIEndpointURL(row.EndpointsJSON), nil
		}
	}

	return "", &ValidationError{
		Code:    http.StatusNotFound,
		Message: "datasource not connected to this application",
	}
}

// persistAndPropagate encrypts merged metadata, writes it to the DB, propagates the
// new (plain-text) credentials to every linked Digitize service, and returns the
// response DTO. It is called only after a successful connectivity test.
func (s *DatasourceService) persistAndPropagate(
	ctx context.Context,
	id uuid.UUID,
	merged map[string]any,
	sensitiveFields map[string]bool,
	updatable map[string]bool,
) (*apimodels.UpdateDatasourceResponse, error) {
	encryptedMerged, err := encryptSensitiveFields(merged, sensitiveFields, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt connector credentials: %w", err)
	}

	updated, err := s.connectorRepo.Update(ctx, id, dbrepo.ConnectorUpdateFields{
		Metadata: encryptedMerged,
		Status:   dbmodels.ConnectorStatusConnected,
		Message:  "",
	})
	if err != nil {
		if errors.Is(err, dbrepo.ErrConnectorNotFound) {
			return nil, &ValidationError{Code: http.StatusNotFound, Message: "datasource not found"}
		}

		return nil, fmt.Errorf("failed to update connector: %w", err)
	}

	// Propagate plain-text credentials to linked Digitize services — never the encrypted form.
	propagationErrors := s.propagateCredentials(ctx, id, updatable, merged)

	resp := &apimodels.UpdateDatasourceResponse{
		DatasourceItem: datasourceItemFromConnector(updated),
	}

	if len(propagationErrors) > 0 {
		resp.PropagationErrors = propagationErrors
	}

	return resp, nil
}

// propagateCredentials calls PUT /v1/connectors/<datasourceID> on every Digitize service
// linked to this datasource. Each call is retried once on failure. Errors are collected and
// returned; a failure does not roll back the DB update.
// credFields is the set of fields to include in the propagation payload (the updatable fields).
func (s *DatasourceService) propagateCredentials(
	ctx context.Context,
	datasourceID uuid.UUID,
	credFields map[string]bool,
	fullMerged map[string]any,
) []apimodels.PropagationError {
	// GetLinkedServiceEndpoints issues a single JOIN query:
	//   service_dependencies → services → applications
	// returning the application identity and the service's runtime endpoint URL for
	// each row where dependency_id = datasourceID AND dependency_type = 'connector'.
	serviceEndpoints, err := s.svcDepRepo.GetLinkedServiceEndpoints(
		ctx,
		datasourceID,
		dbmodels.DependencyTypeConnector,
	)
	if err != nil {
		// Non-fatal: log the error and surface it as a propagation failure rather than
		// returning a 500 — the DB record was already updated successfully.
		logger.WarningfCtx(ctx, "failed to query linked service endpoints for datasource %s: %v", datasourceID, err)

		return []apimodels.PropagationError{{
			ApplicationID:   "",
			ApplicationName: "unknown",
			Error:           fmt.Sprintf("failed to query linked service endpoints: %v", err),
		}}
	}

	if len(serviceEndpoints) == 0 {
		return nil
	}

	// Build the credential payload — only the updatable (Authentication) fields for this provider.
	credPayload := filterUpdatableFields(fullMerged, credFields)

	var propErrors []apimodels.PropagationError

	for _, svc := range serviceEndpoints {
		baseURL := extractAPIEndpointURL(svc.EndpointsJSON)
		if baseURL == "" {
			propErrors = append(propErrors, apimodels.PropagationError{
				ApplicationID:   svc.ApplicationID.String(),
				ApplicationName: svc.ApplicationName,
				Error:           "service has no reachable endpoint",
			})

			continue
		}

		if err := catalogclient.NewServiceClient(baseURL).UpdateConnector(ctx, datasourceID.String(), credPayload); err != nil {
			propErrors = append(propErrors, apimodels.PropagationError{
				ApplicationID:   svc.ApplicationID.String(),
				ApplicationName: svc.ApplicationName,
				Error:           err.Error(),
			})
		}
	}

	return propErrors
}

// filterUpdatableFields returns a new map containing only the keys present in allowed.
// Keys not in allowed are silently dropped.
func filterUpdatableFields(input map[string]any, allowed map[string]bool) map[string]any {
	result := make(map[string]any, len(allowed))
	for k, v := range input {
		if allowed[k] {
			result[k] = v
		}
	}

	return result
}

// datasourceItemFromConnector converts a Connector DB model to the public DatasourceItem DTO.
// This is the single place that maps model fields to response fields, avoiding drift when
// new columns are added to the Connector model.
func datasourceItemFromConnector(c *dbmodels.Connector) apimodels.DatasourceItem {
	return apimodels.DatasourceItem{
		ID:        c.ID,
		Name:      c.Name,
		Type:      c.Type,
		Provider:  c.Provider,
		Status:    string(c.Status),
		Message:   c.Message,
		CreatedBy: c.CreatedBy,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// fetchDigitzeSyncState calls GET /v1/connectors/{connectorID} on the Digitize pod at baseURL
// using catalogclient.ServiceClient (resty-based) and returns the sync_status, last_sync_at,
// and a non-empty errMsg when the state could not be fetched (empty baseURL or HTTP failure).
// The caller embeds errMsg in the response item so users know why sync state is unavailable;
// the connector record is always returned regardless of sync-state fetch outcome.
func fetchDigitzeSyncState(ctx context.Context, connectorID uuid.UUID, baseURL string) (syncStatus string, lastSyncAt *string, errMsg string) {
	if baseURL == "" {
		return "unknown", nil, "no api endpoint registered for this service"
	}

	state, err := catalogclient.NewServiceClient(baseURL).GetConnectorSync(ctx, connectorID.String())
	if err != nil {
		logger.WarningfCtx(ctx, "failed to fetch sync state for datasource %s from %s: %v", connectorID, baseURL, err)

		return "unknown", nil, fmt.Sprintf("failed to fetch sync state: %v", err)
	}

	return state.SyncStatus, state.LastSyncAt, ""
}

// fetchServiceSyncDetails calls GET /v1/connectors/{connectorID} on the connected service
// at baseURL and returns a fully-populated ServiceSyncDetails. On failure (empty baseURL
// or HTTP error) it degrades gracefully: SyncStatus = "unknown", all numeric/timestamp
// fields = nil, ErrMsg populated — identical degradation pattern to
// fetchDigitzeSyncState / ConnectedServiceItem.
func fetchServiceSyncDetails(ctx context.Context, connectorID uuid.UUID, baseURL string) apimodels.ServiceSyncDetails {
	if baseURL == "" {
		return apimodels.ServiceSyncDetails{
			SyncStatus: "unknown",
			ErrMsg:     "no api endpoint registered for this service",
		}
	}

	state, err := catalogclient.NewServiceClient(baseURL).GetConnectorSync(ctx, connectorID.String())
	if err != nil {
		logger.WarningfCtx(ctx, "failed to fetch sync state for connector %s from %s: %v", connectorID, baseURL, err)

		return apimodels.ServiceSyncDetails{
			SyncStatus: "unknown",
			ErrMsg:     fmt.Sprintf("failed to fetch sync state: %v", err),
		}
	}

	return apimodels.ServiceSyncDetails{
		SyncStatus:    state.SyncStatus,
		TotalFiles:    state.TotalFiles,
		LastSyncAt:    state.LastSyncAt,
		LastSyncError: state.LastSyncError,
	}
}

// extractAPIEndpointURL parses a JSONB endpoints array (shape: [{"type":"...","url":"..."},...])
// and returns the URL of the first entry whose "type" is "api".
// Both Podman and OpenShift deployers register the service backend URL with type "api".
// Returns an empty string when the array is empty, malformed, or contains no "api" entry.
func extractAPIEndpointURL(endpointsJSON json.RawMessage) string {
	if len(endpointsJSON) == 0 {
		return ""
	}

	var endpoints []map[string]any
	if err := json.Unmarshal(endpointsJSON, &endpoints); err != nil {
		return ""
	}

	for _, ep := range endpoints {
		if t, ok := ep["type"].(string); ok && t == "api" {
			if u, ok := ep["url"].(string); ok {
				return u
			}
		}
	}

	return ""
}

// ListApplicationDatasources returns a paginated list of datasource connectors linked to
// the given application.
//
// Digitize calls are minimised: GET /v1/connectors is called once per application to bulk-fetch
// status, last_sync, and total_files for all connectors, then GET /v1/connectors/{id}/syncs?latest=true
// is called per connector for new_files (needed to build the message).
// All connectors in an application share the same Digitize pod so one base URL is resolved
// from the first connector's linked service rows via the existing extractAPIEndpointURL helper.
//
// Returns a *ValidationError with code 404 when the application does not exist.
func (s *DatasourceService) ListApplicationDatasources(ctx context.Context, req apimodels.ListApplicationDatasourcesRequest) (*apimodels.ApplicationDatasourceListResponse, error) {
	appID, err := uuid.Parse(req.ApplicationID)
	if err != nil {
		return nil, &ValidationError{Code: http.StatusBadRequest, Message: "invalid application ID"}
	}

	app, err := s.appRepo.GetByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up application: %w", err)
	}

	if app == nil {
		return nil, &ValidationError{Code: http.StatusNotFound, Message: "application not found"}
	}

	connectorIDs, total, err := s.svcDepRepo.GetConnectorsByAppID(ctx, appID, req.PageSize, (req.Page-1)*req.PageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to list application connectors: %w", err)
	}

	// Resolve base URL and bulk-fetch all Digitize connectors once for the whole page.
	// Uses the first connector to locate the shared Digitize pod endpoint.
	baseURL, digitizeConnectors := s.fetchDigitizeConnectors(ctx, connectorIDs)

	data := make([]apimodels.ApplicationDatasourceItem, 0, len(connectorIDs))
	for _, cid := range connectorIDs {
		item, buildErr := s.buildApplicationDatasourceItem(ctx, cid, baseURL, digitizeConnectors)
		if buildErr != nil {
			return nil, buildErr
		}

		data = append(data, *item)
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + req.PageSize - 1) / req.PageSize
	}

	return &apimodels.ApplicationDatasourceListResponse{
		Data: data,
		Pagination: catalogtypes.PaginationMetadata{
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalItems: total,
			TotalPages: totalPages,
			HasNext:    req.Page < totalPages,
			HasPrev:    req.Page > 1,
		},
	}, nil
}

// fetchDigitizeConnectors resolves the Digitize base URL from the first connector in the
// list (using the existing GetLinkedServiceEndpoints + extractAPIEndpointURL path), then
// calls GET /v1/connectors once to return a map keyed by connector ID.
// Returns an empty string and nil map when no endpoint is found or the call fails.
func (s *DatasourceService) fetchDigitizeConnectors(ctx context.Context, connectorIDs []uuid.UUID) (string, map[string]apimodels.DigitizeConnectorItem) {
	if len(connectorIDs) == 0 {
		return "", nil
	}

	linkedRows, err := s.svcDepRepo.GetLinkedServiceEndpoints(ctx, connectorIDs[0], dbmodels.DependencyTypeConnector)
	if err != nil {
		logger.WarningfCtx(ctx, "failed to resolve Digitize endpoint for connector %s: %v", connectorIDs[0], err)

		return "", nil
	}

	baseURL := ""
	for _, row := range linkedRows {
		if u := extractAPIEndpointURL(row.EndpointsJSON); u != "" {
			baseURL = u

			break
		}
	}

	if baseURL == "" {
		return "", nil
	}

	connectors, err := catalogclient.NewDigitizeClient(baseURL).ListConnectors(ctx)
	if err != nil {
		logger.WarningfCtx(ctx, "failed to list connectors from Digitize at %s: %v", baseURL, err)

		return baseURL, nil
	}

	return baseURL, connectors
}

// buildApplicationDatasourceItem fetches connector metadata from the DB and enriches it
// with sync state from the pre-fetched digitizeConnectors map (status, last_sync, total_files)
// plus a per-connector sync log call for new_files (needed to build the message).
func (s *DatasourceService) buildApplicationDatasourceItem(ctx context.Context, connectorID uuid.UUID, baseURL string, digitizeConnectors map[string]apimodels.DigitizeConnectorItem) (*apimodels.ApplicationDatasourceItem, error) {
	connector, err := s.connectorRepo.GetByID(ctx, connectorID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch connector: %w", err)
	}

	providerName := connector.Provider
	if catalogConn, loadErr := s.catalogProvider.LoadConnector(catalogconstants.ConnectorTypeDatasource, connector.Provider); loadErr == nil {
		providerName = catalogConn.Name
	}

	item := &apimodels.ApplicationDatasourceItem{
		ID:   connector.ID.String(),
		Name: connector.Name,
		Provider: apimodels.DatasourceProviderInfo{
			ID:   connector.Provider,
			Name: providerName,
		},
		Status: "unknown",
	}

	if baseURL == "" {
		item.ErrMsg = "no api endpoint registered for this service"

		return item, nil
	}

	digitizeConn, found := digitizeConnectors[connectorID.String()]
	if !found {
		item.ErrMsg = "connector not found on Digitize pod"

		return item, nil
	}

	item.Status = digitizeConn.SyncStatus
	item.LastSync = digitizeConn.LastSyncAt
	item.Files = digitizeConn.TotalFiles

	// Fetch the latest sync log for new_files — needed to build the message.
	syncLog, logErr := catalogclient.NewDigitizeClient(baseURL).GetLatestConnectorSyncLog(ctx, connectorID.String())
	if logErr != nil {
		logger.WarningfCtx(ctx, "failed to fetch latest sync log for connector %s: %v", connectorID, logErr)
		item.ErrMsg = fmt.Sprintf("failed to fetch latest sync log: %v", logErr)

		return item, nil
	}

	if syncLog != nil {
		item.Message = buildSyncMessage(digitizeConn.SyncStatus, syncLog)
	}

	return item, nil
}

// buildSyncMessage derives the human-readable message for a datasource list item.
//
//	syncing     → "Processing <total_files>/<new_files> files"
//	up to date  → "<new_files> new files found"
//	out of sync → <error from latest sync log>
func buildSyncMessage(status string, log *apimodels.ConnectorSyncLog) string {
	switch status {
	case digitizeSyncStatusSyncing:
		return fmt.Sprintf("Processing %d/%d files", log.TotalFiles, log.NewFiles)
	case digitizeSyncStatusUpToDate:
		return fmt.Sprintf("%d new files found", log.NewFiles)
	case digitizeSyncStatusOutOfSync:
		return log.Error
	default:
		return ""
	}
}

// Made with Bob
