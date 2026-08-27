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
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// ErrMsgDatasourceNameExists is returned when a connector with the given name already exists.
	ErrMsgDatasourceNameExists = "Datasource with name %q already exists"
)

// ValidationError re-exported so callers use the same type as for application errors.
type ValidationError = validators.ValidationError

// DigitizeClientInterface is the contract for sending connector payloads to downstream Digitize services.
// Defined here so the inner package owns its dependency; the outer interface.go re-exports it
// so the router wiring can reference it without importing this package directly.
type DigitizeClientInterface interface {
	Connect(ctx context.Context, baseURL string, req apimodels.ConnectDatasourceRequest) error
}

// DatasourceService is the single implementation of the create-datasource and connect-datasource flows.
// It is provider-agnostic: provider-specific behaviour (connection testing) is
// delegated to a ConnectionTester looked up from the testers registry.
// Sensitive-field identification is derived at runtime from each provider's
// schema.json, keyed on format: "password".
type DatasourceService struct {
	connectorRepo   dbrepo.ConnectorRepository
	serviceRepo     dbrepo.ServiceRepository
	svcDepRepo      dbrepo.ServiceDependencyRepository
	validator       *validators.ConnectorValidator
	catalogProvider *catalog.CatalogProvider
	digitizeClient  DigitizeClientInterface
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
	serviceRepo dbrepo.ServiceRepository,
	svcDepRepo dbrepo.ServiceDependencyRepository,
	validator *validators.ConnectorValidator,
	catalogProvider *catalog.CatalogProvider,
	digitizeClient DigitizeClientInterface,
	encryptionKey string,
) *DatasourceService {
	return &DatasourceService{
		connectorRepo:   connectorRepo,
		serviceRepo:     serviceRepo,
		svcDepRepo:      svcDepRepo,
		validator:       validator,
		catalogProvider: catalogProvider,
		digitizeClient:  digitizeClient,
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
//  2. Duplicate-name guard (case-insensitive).
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
	schema, err := s.catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for provider %q: %w", req.ProviderID, err)
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

// ConnectDatasourceToApplication links a datasource connector to every eligible service
// (AcceptsDatasource == true) in the given application.
//
// Flow:
//  1. Load the connector (with credentials) and decrypt sensitive metadata fields.
//  2. Fetch all services for the application, filtering to those whose catalog entry
//     has AcceptsDatasource set and which have a live API endpoint registered.
//  3. For each eligible service POST the connector payload to Digitize and record
//     a service_dependency row (type: connector).
//
// At least one eligible service with an API endpoint is required; the call returns
// 422 if none are found.
func (s *DatasourceService) ConnectDatasourceToApplication(ctx context.Context, applicationID, datasourceID uuid.UUID) (*apimodels.ConnectDatasourceResponse, error) {
	// Phase 1: load connector with credentials and decrypt sensitive fields.
	connector, err := s.loadConnector(ctx, datasourceID)
	if err != nil {
		return nil, err
	}

	connectionDetails, err := s.decryptedConnectionDetails(ctx, connector)
	if err != nil {
		return nil, err
	}

	// Phase 2: resolve eligible services for this application via a single JOIN query
	// (services → applications), filtering by catalog AcceptsDatasource in-memory.
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

	// Phase 3: propagate to each eligible service.
	var connectedServiceID uuid.UUID

	for _, svc := range linkedServices {
		if err := s.sendToService(ctx, svc, connector, connectionDetails, datasourceID); err != nil {
			return nil, err
		}

		connectedServiceID = svc.ServiceID
	}

	return &apimodels.ConnectDatasourceResponse{
		ApplicationID: applicationID.String(),
		DatasourceID:  datasourceID.String(),
		ConnectorID:   connectedServiceID.String(),
	}, nil
}

// eligibleServicesForApp returns the subset of services in applicationID whose catalog
// entry has AcceptsDatasource == true and which have a registered api-type endpoint.
// It uses GetServiceEndpointsByAppID — a single JOIN query across services → applications —
// so endpoint URLs are resolved at the DB level (same pattern as GetLinkedServiceEndpoints
// in the service-dependency repo used by the AIS_1634 read path).
func (s *DatasourceService) eligibleServicesForApp(ctx context.Context, applicationID uuid.UUID) ([]dbrepo.LinkedServiceEndpoint, error) {
	all, err := s.serviceRepo.GetServiceEndpointsByAppID(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load services for application: %w", err)
	}

	var eligible []dbrepo.LinkedServiceEndpoint

	for _, ep := range all {
		catalogSvc, loadErr := s.catalogProvider.LoadService(ep.ApplicationCatalogID)
		if loadErr != nil || !catalogSvc.AcceptsDatasource {
			continue
		}

		if ep.URL == "" {
			logger.WarningfCtx(ctx, "service %s (%s) accepts datasource but has no API endpoint — skipping", ep.ServiceID, ep.ApplicationCatalogID)

			continue
		}

		eligible = append(eligible, ep)
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
	schema, err := s.catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, connector.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for provider %q: %w", connector.Provider, err)
	}

	return decryptSensitiveFields(connector.Metadata, sensitiveFieldsFromSchema(schema), s.encryptionKey)
}

// sendToService POSTs the connector payload to a single Digitize service and records the
// service_dependency row. Returns a *ValidationError on downstream failure.
func (s *DatasourceService) sendToService(
	ctx context.Context,
	svc dbrepo.LinkedServiceEndpoint,
	connector *dbmodels.Connector,
	connectionDetails map[string]any,
	datasourceID uuid.UUID,
) error {
	if svc.URL == "" {
		logger.WarningfCtx(ctx, "service %s (%s) accepts datasource but has no API endpoint — skipping", svc.ServiceID, svc.ApplicationCatalogID)

		return nil
	}

	connectReq := apimodels.ConnectDatasourceRequest{
		ConnectorID:       connector.ID.String(),
		ConnectorName:     connector.Name,
		Type:              connector.Provider,
		ConnectionDetails: connectionDetails,
	}

	if err := s.digitizeClient.Connect(ctx, svc.URL, connectReq); err != nil {
		return &ValidationError{
			Code:    http.StatusBadGateway,
			Message: fmt.Sprintf("failed to connect datasource to service %s: %v", svc.ApplicationCatalogID, err),
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

// decryptSensitiveFields returns a copy of params where every key listed in
// sensitiveKeys has its encrypted string value replaced with the decrypted plaintext.
func decryptSensitiveFields(params map[string]any, sensitiveKeys map[string]bool, encryptionKey string) (map[string]any, error) {
	if len(sensitiveKeys) == 0 || encryptionKey == "" {
		return params, nil
	}

	result := make(map[string]any, len(params))

	for k, v := range params {
		if sensitiveKeys[k] {
			ciphertext, ok := v.(string)
			if !ok {
				return nil, &ValidationError{
					Code:    http.StatusInternalServerError,
					Message: fmt.Sprintf("sensitive field %q has unexpected non-string value", k),
				}
			}

			plaintext, err := catalogutils.Decrypt(ciphertext, encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt field %q: %w", k, err)
			}

			result[k] = plaintext
		} else {
			result[k] = v
		}
	}

	return result, nil
}

// Made with Bob
