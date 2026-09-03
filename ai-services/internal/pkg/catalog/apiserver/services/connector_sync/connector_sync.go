// Package connectorsync provides the ConnectorSyncJob background job that
// periodically tests connectivity for every record in the connectors table
// and writes the result back via UpdateStatus.
//
// Design mirrors the existing SyncService in the sibling sync package:
//   - single goroutine launched by Start, stopped by Stop (close of stopChan)
//   - mutex guard prevents overlapping cycles
//   - panic recovery in the loop goroutine
//   - initial sync runs immediately on Start, then on each ticker tick
package connectorsync

import (
	"context"
	"sync"
	"time"

	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	datasourceservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/datasource_service"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	// DefaultConnectorSyncInterval is the default period between ConnectorSyncJob cycles.
	// The job tests connectivity for every record in the connectors table on each tick.
	DefaultConnectorSyncInterval = 5 * time.Minute

	// connectorSyncSkew is the initial delay before the first sync run to avoid
	// overlapping with the application sync service.
	connectorSyncSkew = 10 * time.Second
)

// ConnectorSyncJob is a background job that periodically calls TestConnection for
// every connector record in the database and updates its status accordingly.
// It is connector-type-agnostic: it delegates connection testing to the registered
// ConnectionTester for each connector's provider.
type ConnectorSyncJob struct {
	connectorRepo   dbrepo.ConnectorRepository
	testers         map[string]datasourceservice.ConnectionTester
	sensitiveFields map[string]map[string]bool // provider → sensitive field set; pre-computed at construction
	encryptionKey   string
	syncInterval    time.Duration
	stopChan  chan struct{}
	syncMutex sync.Mutex // held for the duration of a sync cycle; TryLock skips overlapping cycles
}

// NewConnectorSyncJob creates a new ConnectorSyncJob.
//
// testers is a map from provider ID to its ConnectionTester implementation
// (e.g. "object_storage" → objectStorageTester, "file_system" → fileSystemTester).
// catalogProvider is used once at construction to load each provider's schema.json and
// pre-compute the set of sensitive fields per provider via catalogutils.SensitiveFieldsFromSchema.
// encryptionKey is the AES-256 key used to decrypt sensitive credential fields before
// passing them to TestConnection; it must be the same key used at create/update time.
// syncInterval controls how often the job runs; pass 0 to use DefaultConnectorSyncInterval.
func NewConnectorSyncJob(
	ctx context.Context,
	connectorRepo dbrepo.ConnectorRepository,
	catalogProvider *catalogpkg.CatalogProvider,
	testers map[string]datasourceservice.ConnectionTester,
	encryptionKey string,
	syncInterval time.Duration,
) *ConnectorSyncJob {
	if syncInterval == 0 {
		syncInterval = DefaultConnectorSyncInterval
	}

	// Pre-compute sensitive fields for each registered provider once at startup.
	// The schema is static — it never changes while the process is running.
	sensitiveFields := make(map[string]map[string]bool, len(testers))
	for providerID := range testers {
		rawSchema, err := catalogProvider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, providerID)
		if err != nil {
			logger.WarningfCtx(ctx, "ConnectorSyncJob: failed to load schema for provider %q, sensitive fields will be empty: %v", providerID, err)
			sensitiveFields[providerID] = map[string]bool{}

			continue
		}

		schema, err := pkgutils.ConvertRawJsontoMap(rawSchema)
		if err != nil {
			logger.WarningfCtx(ctx, "ConnectorSyncJob: failed to parse schema for provider %q, sensitive fields will be empty: %v", providerID, err)
			sensitiveFields[providerID] = map[string]bool{}

			continue
		}
		sensitiveFields[providerID] = catalogutils.SensitiveFieldsFromSchema(schema)
	}

	return &ConnectorSyncJob{
		connectorRepo:   connectorRepo,
		testers:         testers,
		sensitiveFields: sensitiveFields,
		encryptionKey:   encryptionKey,
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
	}
}

// Start launches the sync goroutine. ctx should be a signal-aware context so the
// goroutine is also cancelled when the process is asked to shut down.
func (j *ConnectorSyncJob) Start(ctx context.Context) {
	go j.syncLoop(ctx)
	logger.InfolnCtx(ctx, "Connector sync job started")
}

// Stop gracefully shuts down the sync goroutine.
func (j *ConnectorSyncJob) Stop(ctx context.Context) {
	close(j.stopChan)
	logger.InfolnCtx(ctx, "Connector sync job stopped")
}

// syncLoop is the main loop. It runs an initial sync immediately on startup,
// then once per ticker tick until stopChan is closed.
func (j *ConnectorSyncJob) syncLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in connector sync goroutine: %v", r)
		}
	}()

	// Introduce a small skew at startup to stagger it with the application sync service.
	select {
	case <-time.After(connectorSyncSkew):
	case <-j.stopChan:
		return
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(j.syncInterval)
	defer ticker.Stop()

	// Run initial sync immediately so status is up-to-date at startup.
	j.performSync(ctx)

	for {
		select {
		case <-ticker.C:
			j.performSync(ctx)
		case <-j.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// performSync executes one full sync cycle. If a cycle is already in progress it
// is skipped — identical to the SyncService overlap guard.
func (j *ConnectorSyncJob) performSync(ctx context.Context) {
	if !j.syncMutex.TryLock() {
		logger.DebuglnCtx(ctx, "Connector sync already in progress, skipping this cycle")

		return
	}
	defer j.syncMutex.Unlock()

	logger.DebuglnCtx(ctx, "Starting connector sync cycle")

	connectors, err := j.connectorRepo.List(ctx, nil)
	if err != nil {
		logger.ErrorfCtx(ctx, "Connector sync: failed to list connectors: %v", err)

		return
	}

	for _, c := range connectors {
		j.syncConnector(ctx, c)
	}

	logger.InfolnCtx(ctx, "Completed connector sync cycle")
}

// syncConnector runs TestConnection for a single connector and writes the result
// back to the database via UpdateStatus.
//
// Sensitive credential fields are decrypted in-memory using the set pre-computed at
// construction from the provider's schema.json. Decrypted values are never logged.
func (j *ConnectorSyncJob) syncConnector(ctx context.Context, c dbmodels.Connector) {
	tester, ok := j.testers[c.Provider]
	if !ok {
		logger.WarningfCtx(ctx, "Connector sync: no tester registered for provider %q (connector %s)", c.Provider, c.ID)

		return
	}

	// Fetch the full metadata (including encrypted credentials) for this connector.
	full, err := j.connectorRepo.GetByID(ctx, c.ID, true)
	if err != nil {
		logger.ErrorfCtx(ctx, "Connector sync: failed to fetch credentials for connector %s: %v", c.ID, err)

		return
	}

	// Decrypt sensitive fields in-memory using the pre-computed set for this provider;
	// values are never forwarded to any caller.
	decrypted, err := catalogutils.DecryptSensitiveFields(full.Metadata, j.sensitiveFields[c.Provider], j.encryptionKey)
	if err != nil {
		logger.ErrorfCtx(ctx, "Connector sync: failed to decrypt credentials for connector %s: %v", c.ID, err)

		return
	}

	testErr := tester.TestConnection(ctx, decrypted)

	var (
		newStatus dbmodels.ConnectorStatus
		message   string
	)

	if testErr == nil {
		newStatus = dbmodels.ConnectorStatusConnected
	} else {
		newStatus = dbmodels.ConnectorStatusOffline
		message = testErr.Error()
	}

	if err := j.connectorRepo.UpdateStatus(ctx, c.ID, newStatus, message); err != nil {
		logger.ErrorfCtx(ctx, "Connector sync: failed to update status for connector %s: %v", c.ID, err)

		return
	}

	logger.InfofCtx(ctx, "Connector sync: connector %s (%s/%s) → %s", c.ID, c.Type, c.Provider, newStatus)
}
