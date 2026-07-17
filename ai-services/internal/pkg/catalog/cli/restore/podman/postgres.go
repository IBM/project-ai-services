package podman

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	catalogpodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// restorePostgres reads the SQL dump from the backup and restores it into the
// running postgres container using psql.
// Security: The DB password is passed only via the PGPASSWORD environment
// variable on the exec subprocess – it is never present in an argument visible
// to `ps` and is never logged.
func restorePostgres(ctx context.Context, tempDir string) error {
	logger.Infoln("  Restoring PostgreSQL database...")

	dumpPath := filepath.Join(tempDir, catalogpodman.DirDB, catalogpodman.DBDumpFileName)
	if _, err := os.Stat(dumpPath); os.IsNotExist(err) {
		return fmt.Errorf("database dump not found in backup at %s", dumpPath)
	}

	containerName := catalogConstants.CatalogAppName + "--db-" + catalogpodman.PostgresContainer
	logger.Infof("  Using postgres container: %s\n", containerName)

	dbPassword, err := catalogpodman.ReadSecretFromPodman(
		ctx,
		catalogConstants.CatalogDBSecretName,
		catalogConstants.CatalogDBSecretKey,
	)
	if err != nil {
		return fmt.Errorf("failed to read current DB password from live secret: %w", err)
	}

	if err := runPSQLRestore(ctx, containerName, dbPassword, dumpPath); err != nil {
		return err
	}

	logger.Infoln("  ✓ PostgreSQL restore completed")

	return nil
}

// runPSQLRestore restores the SQL dump using psql inside the postgres container.
//
// Security: PGPASSWORD is set via the -e flag of `podman exec`, keeping it out
// of the argument list that would be visible to `ps`, and is never included in
// any error or log message.
func runPSQLRestore(ctx context.Context, containerName, dbPassword, dumpPath string) error {
	// Open the SQL dump file on the host so we can pipe it into the container's stdin.
	dumpFile, err := os.Open(dumpPath)
	if err != nil {
		return fmt.Errorf("failed to open database dump file: %w", err)
	}
	defer func() {
		_ = dumpFile.Close()
	}()

	// Build: podman exec -i -e PGPASSWORD=<pw> <container> psql -U <user> <db>
	// --single-transaction: wrap the entire restore in one transaction so a
	//   partial failure leaves the database unchanged rather than half-applied.
	// ON_ERROR_STOP=1: abort immediately on the first SQL error.
	args := []string{
		"exec",
		"-i",
		"-e", "PGPASSWORD=" + dbPassword,
		containerName,
		"psql",
		"-U", catalogpodman.PostgresUser,
		"--set", "ON_ERROR_STOP=1",
		"--single-transaction",
		"-d", catalogpodman.PostgresDB,
	}

	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Stdin = dumpFile

	// Capture stderr separately so it can be safely included in an error message.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start psql restore: %w", err)
	}

	stderrBuf := make([]byte, catalogpodman.StderrBufferBytes)
	n, _ := stderrPipe.Read(stderrBuf)
	stderrMsg := string(stderrBuf[:n])

	if err := cmd.Wait(); err != nil {
		if stderrMsg != "" {
			return fmt.Errorf("psql restore failed: %w — %s", err, stderrMsg)
		}

		return fmt.Errorf("psql restore failed: %w", err)
	}

	return nil
}

// Made with Bob
