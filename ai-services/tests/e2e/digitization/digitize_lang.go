package digitization

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// langIngestTimeout is the end-to-end deadline for OCR + embedding + OpenSearch indexing of a language PDF.
const langIngestTimeout = 20 * time.Minute //nolint:mnd

// GetLanguagePDFPath returns the absolute path to the PDF fixture for the given filename stem
// (e.g. "german", "french", "italian"). The file is expected at ingestion/docs/<language>.pdf.
// Returns an empty string if the file location cannot be resolved.
// To add a new language, drop <language>.pdf into ingestion/docs/ — no code change needed.
func GetLanguagePDFPath(language string) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	return filepath.Join(
		filepath.Dir(filename), "..", "ingestion", "docs",
		strings.ToLower(language)+".pdf",
	)
}

// IngestLanguageDocumentViaDigitizeAPI ingests the PDF at pdfPath via the digitize microservice
// (operation=ingestion) so it is indexed in OpenSearch before RAG evaluation.
// pdfPath must be a non-empty path to an existing PDF file.
func IngestLanguageDocumentViaDigitizeAPI(ctx context.Context, digitizeBaseURL, pdfPath, jobName string) error {
	if pdfPath == "" {
		return fmt.Errorf("pdfPath is empty — cannot submit ingestion job")
	}

	logger.Infof("[INGEST-LANG] Submitting ingestion job for %s (job name: %s)", filepath.Base(pdfPath), jobName)

	jobResp, err := CreateJob(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", jobName)
	if err != nil {
		return fmt.Errorf("failed to create ingestion job for %s: %w", filepath.Base(pdfPath), err)
	}

	logger.Infof("[INGEST-LANG] Job submitted (job_id=%s) — waiting for completion", jobResp.JobID)

	finalStatus, err := WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, langIngestTimeout)
	if err != nil {
		return fmt.Errorf("ingestion job %s did not complete: %w", jobResp.JobID, err)
	}

	logger.Infof("[INGEST-LANG] Job %s completed — status=%s docs=%d",
		jobResp.JobID, finalStatus.Status, len(finalStatus.Documents))

	return nil
}
