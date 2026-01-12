package common

import (
	"log"
	"os"
)

const dirPerm = 0o755 // standard permission for directories

// CreateDir creates a directory if it does not exist.
func CreateDir(path string) {
	if err := os.MkdirAll(path, dirPerm); err != nil {
		log.Println("Failed to create directory:", path, err)
	} else {
		log.Println("Directory created:", path)
	}
}
