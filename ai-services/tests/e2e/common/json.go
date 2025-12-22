package common

import (
	"encoding/json"
	"log"
)

// ParseJSON parses JSON data into a struct
func ParseJSON(data []byte, v interface{}) error {
	err := json.Unmarshal(data, v)
	if err != nil {
		log.Println("Failed to parse JSON:", err)
		return err
	}
	return nil
}
