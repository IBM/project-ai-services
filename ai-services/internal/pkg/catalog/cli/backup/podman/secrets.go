package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	catalogpodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// backupSecrets writes the admin-password hash (catalog-secret) to the temp directory.
// It is stored as a JSON file so the restore operation can re-create the secret
// without ambiguity.
//
// catalog-db-secret is intentionally NOT backed up: the postgres user password
// lives in the database data-volume and is never modified by the SQL dump, so
// restoring a different DB password would break the backend→postgres connection.
//
// Security: secret values are written to a 0600 file inside a 0700 temp directory
// and are never logged or included in any error message.
func backupSecrets(ctx context.Context, tempDir string) error {
	secretsDir := filepath.Join(tempDir, catalogpodman.DirSecrets)
	if err := os.MkdirAll(secretsDir, catalogpodman.DirPerm); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	return backupSingleSecret(ctx, secretsDir,
		catalogConstants.CatalogSecretName, catalogConstants.CatalogSecretKey)
}

// backupSingleSecret fetches one Podman secret value and writes it as a JSON file.
func backupSingleSecret(ctx context.Context, secretsDir, secretName, secretKey string) error {
	logger.Infof("  Backing up secret: %s\n", secretName)

	value, err := catalogpodman.ReadSecretFromPodman(ctx, secretName, secretKey)
	if err != nil {
		return err
	}

	data := map[string]string{secretKey: value}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode secret %s: %w", secretName, err)
	}

	destPath := filepath.Join(secretsDir, secretName+".json")
	if err := os.WriteFile(destPath, encoded, catalogpodman.FilePerm); err != nil {
		return fmt.Errorf("failed to write secret backup for %s: %w", secretName, err)
	}

	return nil
}

// Made with Bob
