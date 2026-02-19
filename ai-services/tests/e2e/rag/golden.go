package rag

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	minCSVRows        = 2  // header + at least one data row
	minCSVColumns     = 3  // ID, Question, GoldenAnswer
	csvLineNumberOffset   = 2  // account for 1-based indexing + header row
)

// GoldenCase represents one golden dataset row.
type GoldenCase struct {
	ID           string
	Question     string
	GoldenAnswer string
}

// LoadGoldenCSV loads golden dataset from a CSV file.
func LoadGoldenCSV(path string) ([]GoldenCase, error) {
	csvFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open golden CSV %s: %w", path, err)
	}

	defer func() {
		if err := csvFile.Close(); err != nil {
			logger.Errorf("failed to close csv file: %v", err)
		}
	}()

	reader := csv.NewReader(csvFile)
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read golden CSV %s: %w", path, err)
	}

	if len(records) < minCSVRows {
		return nil, fmt.Errorf("golden CSV %s has no data rows", path)
	}

	cases := make([]GoldenCase, 0, len(records)-1)

	for i, row := range records[1:] {
		if len(row) < minCSVColumns {
			return nil, fmt.Errorf("invalid row %d in golden CSV: expected at least %d columns", i+csvLineNumberOffset, minCSVColumns)
		}

		cases = append(cases, GoldenCase{
			ID:           strings.TrimSpace(row[0]),
			Question:     strings.TrimSpace(row[1]),
			GoldenAnswer: strings.TrimSpace(row[2]),
		})
	}

	return cases, nil
}