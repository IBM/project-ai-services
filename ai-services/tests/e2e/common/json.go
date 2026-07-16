package common

import (
	"encoding/json"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ParseJSON unmarshals data into v.
func ParseJSON(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		logger.Errorln("Failed to parse JSON: " + err.Error())

		return err
	}

	return nil
}
