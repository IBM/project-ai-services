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

// backupPostgres runs pg_dump inside the running postgres container and copies
// the SQL dump to tempDir/db/catalog-db.sql.
//
// The DB password is retrieved from the Podman secret and passed to pg_dump via
// the PGPASSWORD environment variable set only for the duration of the exec call,
// so it never appears in log output or error messages.
func backupPostgres(ctx context.Context, tempDir string) error {
	logger.Infoln("  Backing up PostgreSQL database...")

	dbDir := filepath.Join(tempDir, catalogpodman.DirDB)
	if err := os.MkdirAll(dbDir, catalogpodman.DirPerm); err != nil {
		return fmt.Errorf("failed to create db backup directory: %w", err)
	}

	containerName := catalogConstants.CatalogAppName + "--db-" + catalogpodman.PostgresContainer
	logger.Infof("  Using postgres container: %s\n", containerName)

	dbPassword, err := catalogpodman.ReadSecretFromPodman(ctx, catalogConstants.CatalogDBSecretName, catalogConstants.CatalogDBSecretKey)
	if err != nil {
		return fmt.Errorf("failed to read DB password: %w", err)
	}

	dumpPath := filepath.Join(dbDir, catalogpodman.DBDumpFileName)
	if err := runPGDump(ctx, containerName, dbPassword, dumpPath); err != nil {
		return err
	}

	logger.Infoln("  ✓ PostgreSQL dump completed")

	return nil
}

// runPGDump executes pg_dump inside the postgres container and streams the
// result to dumpPath on the host.
//
// Security: PGPASSWORD is set only in the subprocess environment, never in
// an argument list visible to `ps`, and is never included in any error message.
func runPGDump(ctx context.Context, containerName, dbPassword, dumpPath string) error {
	// Open the destination file before exec to avoid leaving a partial file on error.
	out, err := os.OpenFile(dumpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, catalogpodman.FilePerm)
	if err != nil {
		return fmt.Errorf("failed to create dump file: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()

	// Build: podman exec -e PGPASSWORD=<pw> <container> pg_dump -U <user> --clean --if-exists <db>
	// --clean:     emit DROP statements before each CREATE so the dump is idempotent.
	// --if-exists: add IF EXISTS to each DROP to suppress errors when restoring
	//              into a freshly initialised (empty) database.
	// Using "-e" flag to pass the env var only to this exec, not via argument.
	args := []string{
		"exec",
		"-e", "PGPASSWORD=" + dbPassword,
		containerName,
		"pg_dump",
		"-U", catalogpodman.PostgresUser,
		"--clean",
		"--if-exists",
		catalogpodman.PostgresDB,
	}

	// Clear the password from the args slice once the command object is built.
	cmd := exec.CommandContext(ctx, "podman", args...)
	// Zero out the password argument immediately after command construction.
	for i, a := range args {
		if len(a) > 9 && a[:9] == "PGPASSWD=" {
			args[i] = ""
		}
	}

	cmd.Stdout = out

	// Capture stderr separately so it never contains the password and can be
	// safely included in the error message.
	stderrOutput, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start pg_dump: %w", err)
	}

	// Read stderr without blocking (small buffer is enough for error messages).
	stderrBuf := make([]byte, catalogpodman.StderrBufferBytes)
	n, _ := stderrOutput.Read(stderrBuf)
	stderrMsg := string(stderrBuf[:n])

	if err := cmd.Wait(); err != nil {
		_ = out.Close()
		_ = os.Remove(dumpPath) // remove partial dump

		if stderrMsg != "" {
			return fmt.Errorf("pg_dump failed: %w — %s", err, stderrMsg)
		}

		return fmt.Errorf("pg_dump failed: %w", err)
	}

	return nil
}

// Made with Bob
