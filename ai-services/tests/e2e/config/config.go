package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServiceURL    string
	HealthPath    string
	Timeout       time.Duration
	Retries       int
	AIServiceBin  string
	LogProbeWords []string
}

// Default values
const (
	defaultServiceURL  = "http://localhost:8080"
	defaultHealthPath  = "/health"
	defaultTimeoutSecs = 5
	defaultRetries     = 5
)

// LoadFromEnv reads configuration from environment variables and returns a Config populated with defaults.
func LoadFromEnv() *Config {
	cfg := &Config{
		ServiceURL:   defaultServiceURL,
		HealthPath:   defaultHealthPath,
		Timeout:      time.Duration(defaultTimeoutSecs) * time.Second,
		Retries:      defaultRetries,
		AIServiceBin: os.Getenv("AI_SERVICES_BIN"),
		// case-insensitive keywords to look for in logs to indicate readiness
		LogProbeWords: []string{"ready", "healthy", "started", "serving"},
	}

	if v := strings.TrimSpace(os.Getenv("AI_SERVICE_URL")); v != "" {
		cfg.ServiceURL = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_HEALTH_PATH")); v != "" {
		cfg.HealthPath = v
	}
	if v := strings.TrimSpace(os.Getenv("AI_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}
	if v := strings.TrimSpace(os.Getenv("AI_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Retries = n
		}
	}
	// Ensure there is a bin set (empty means rely on PATH)
	if cfg.AIServiceBin == "" {
		cfg.AIServiceBin = os.Getenv("AI_SERVICES_BIN") // keep empty if not set
	}
	return cfg
}

// HealthURL returns the full URL for the health endpoint composed from ServiceURL and HealthPath.
func (c *Config) HealthURL() string {
	base := strings.TrimRight(c.ServiceURL, "/")
	path := strings.TrimLeft(c.HealthPath, "/")
	if path == "" {
		return base
	}
	return base + "/" + path
}
