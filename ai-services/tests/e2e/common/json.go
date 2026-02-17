package common

import (
	"encoding/json"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ParseJSON parses JSON data into a struct.
func ParseJSON(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		// Use shared klog-based logger instead of std log
		logger.Errorln("Failed to parse JSON: " + err.Error())

		return err
	}

	return nil
}
