package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"gopkg.in/yaml.v3"
)

const (
	// ConfigFileName is the name of the configuration file.
	ConfigFileName = "config.yaml"
	// ConfigDirName is the directory name for ai-services config.
	ConfigDirName = ".ai-services"

	defaultDirPermissions  = 0o755
	defaultFilePermissions = 0o644
)

// Config represents the ai-services configuration.
type Config struct {
	Runtime RuntimeConfig `yaml:"runtime"`
}

// RuntimeConfig represents runtime-specific configuration.
type RuntimeConfig struct {
	// Type specifies the container runtime (podman or kubernetes)
	Type string `yaml:"type"`
	// Kubernetes specific configuration
	Kubernetes KubernetesConfig `yaml:"kubernetes,omitempty"`
}

// KubernetesConfig represents Kubernetes-specific settings.
type KubernetesConfig struct {
	// Namespace is the default Kubernetes namespace
	Namespace string `yaml:"namespace,omitempty"`
	// Kubeconfig path (optional, defaults to ~/.kube/config)
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		Runtime: RuntimeConfig{
			Type: string(types.RuntimeTypePodman),
			Kubernetes: KubernetesConfig{
				Namespace: "default",
			},
		},
	}
}

// GetConfigPath returns the path to the config file.
func GetConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	configDir := filepath.Join(home, ConfigDirName)

	return filepath.Join(configDir, ConfigFileName), nil
}

// LoadConfig loads configuration from file or returns default.
func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	// If config file doesn't exist, return default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Infof("Config file not found, using defaults\n", logger.VerbosityLevelDebug)

		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig saves configuration to file.
func SaveConfig(config *Config) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, defaultDirPermissions); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, defaultFilePermissions); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	logger.Infof("Configuration saved to %s\n", configPath)

	return nil
}

// GetRuntimeType returns the configured runtime type.
func (c *Config) GetRuntimeType() types.RuntimeType {
	return types.RuntimeType(c.Runtime.Type)
}

// SetRuntimeType sets the runtime type.
func (c *Config) SetRuntimeType(rt types.RuntimeType) {
	c.Runtime.Type = string(rt)
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	rt := types.RuntimeType(c.Runtime.Type)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'kubernetes')", c.Runtime.Type)
	}

	return nil
}

// Made with Bob
