package datasourceservice

import (
	"context"
	"errors"
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

// DatasourceService is the single implementation of the create/update-datasource flow.
// It is provider-agnostic: provider-specific behaviour (connection testing and
// sensitive-field identification) is delegated to a ConnectionTester looked up
// from the testers registry.
// Sensitive-field identification is derived at runtime from each provider's
// schema.json, keyed on format: "password".
type DatasourceService struct {
	connectorRepo   dbrepo.ConnectorRepository
	serviceDepsRepo dbrepo.ServiceDependencyRepository
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
	serviceDepsRepo dbrepo.ServiceDependencyRepository,
	validator *validators.ConnectorValidator,
	catalogProvider *catalog.CatalogProvider,
	encryptionKey string,
) *DatasourceService {
	return &DatasourceService{
		connectorRepo:   connectorRepo,
		serviceDepsRepo: serviceDepsRepo,
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

	serviceCounts, err := s.serviceDepsRepo.GetServiceCountByDependency(ctx, connectorIDs, dbmodels.DependencyTypeConnector)
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
	decryptedExisting, err := decryptSensitiveFields(existing.Metadata, sensitive, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt existing connector credentials: %w", err)
	}

	// Phase 6: merge — start from the existing full metadata, then overlay the filtered updates.
	merged := mergeMaps(decryptedExisting, filteredUpdates)

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

	updated, err := s.connectorRepo.Update(ctx, id, dbrepo.ConnectorUpdateFields{Metadata: encryptedMerged})
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
	serviceEndpoints, err := s.serviceDepsRepo.GetLinkedServiceEndpoints(
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
		baseURL := svc.URL
		if baseURL == "" {
			propErrors = append(propErrors, apimodels.PropagationError{
				ApplicationID:   svc.ApplicationID.String(),
				ApplicationName: svc.ApplicationName,
				Error:           "service has no reachable endpoint",
			})

			continue
		}

		if err := updateDigitizeConnector(ctx, baseURL, datasourceID.String(), credPayload); err != nil {
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

// mergeMaps returns a new map that starts with all keys from base, then overlays overrides.
// Neither input map is modified.
func mergeMaps(base, overrides map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(overrides))
	for k, v := range base {
		merged[k] = v
	}

	for k, v := range overrides {
		merged[k] = v
	}

	return merged
}

// decryptSensitiveFields returns a copy of params where every key listed in sensitiveKeys
// has its ciphertext value replaced with the corresponding plaintext.
// Fields that are not in sensitiveKeys are copied as-is.
func decryptSensitiveFields(params map[string]any, sensitiveKeys map[string]bool, encryptionKey string) (map[string]any, error) {
	if len(sensitiveKeys) == 0 || len(params) == 0 {
		return params, nil
	}

	if encryptionKey == "" {
		return nil, fmt.Errorf("encryption key is not configured (DB_ENCRYPTION_KEY must be set)")
	}

	result := make(map[string]any, len(params))
	for k, v := range params {
		if sensitiveKeys[k] {
			ciphertext, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("sensitive field %q must be a string", k)
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
