package common

import (
	"log"
	"os"
)

// CreateDir creates a directory if it does not exist
func CreateDir(path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Println("Failed to create directory:", path, err)
	} else {
		log.Println("Directory created:", path)
	}
}
