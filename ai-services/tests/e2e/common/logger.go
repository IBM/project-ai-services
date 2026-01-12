package common

import "log"

// Info logs informational messages.
func Info(msg string) {
	log.Println("[INFO]", msg)
}

// Error logs error messages.
func Error(msg string) {
	log.Println("[ERROR]", msg)
}
