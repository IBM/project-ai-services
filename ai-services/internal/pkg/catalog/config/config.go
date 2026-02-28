//go:build catalog_api
// +build catalog_api

// Package config manages the local CLI configuration for the catalog API client,
// including persisting and loading auth tokens from the user's config directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// configDirName is the subdirectory under os.UserConfigDir() used by ai-services.
	configDirName = "ai-services"
	// configFileName is the name of the credentials file.
	configFileName = "catalog-credentials.json"
)

// ErrNotLoggedIn is returned when no credentials are found on disk.
var ErrNotLoggedIn = errors.New("not logged in: run 'ai-services catalog login' first")

// Credentials holds the tokens returned by the API server after a successful login.
type Credentials struct {
	// ServerURL is the base URL of the catalog API server (e.g. http://localhost:8080).
	ServerURL string `json:"server_url"`
	// RefreshToken is the long-lived token used to obtain fresh access tokens.
	RefreshToken string `json:"refresh_token"`
	// AccessToken is the short-lived bearer token for API calls.
	// It is refreshed automatically when it is about to expire.
	AccessToken string `json:"access_token"`
	// AccessTokenExpiry is the UTC time at which the access token expires.
	// A zero value means the expiry is unknown and the token should be refreshed.
	AccessTokenExpiry time.Time `json:"access_token_expiry,omitempty"`
}

// configFilePath returns the absolute path to the credentials file.
func configFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine user config directory: %w", err)
	}

	return filepath.Join(base, configDirName, configFileName), nil
}

// Save persists the credentials to disk, creating the config directory if needed.
func Save(creds Credentials) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	// Write with restricted permissions so only the owner can read the tokens.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}

	return nil
}

// Load reads the credentials from disk.
// Returns ErrNotLoggedIn if the file does not exist.
func Load() (Credentials, error) {
	path, err := configFilePath()
	if err != nil {
		return Credentials{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, ErrNotLoggedIn
		}

		return Credentials{}, fmt.Errorf("read credentials file: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials file: %w", err)
	}

	return creds, nil
}

// Delete removes the credentials file from disk (used on logout).
func Delete() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove credentials file: %w", err)
	}

	return nil
}

// Made with Bob
