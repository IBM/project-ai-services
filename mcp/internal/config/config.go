package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/mcp/internal/types"
)

// GenerateMCPClientConfig generates an MCP client-compatible configuration
func GenerateMCPClientConfig(serverName string) (*types.ConfigOutput, error) {
	// Get the current executable path and arguments
	args := os.Args[:]

	// Remove --config flag if present
	filteredArgs := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--config" || arg == "-C" {
			continue
		}
		// Skip the argument after --config if it was specified as separate argument
		if i > 0 && (args[i-1] == "--config" || args[i-1] == "-C") {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}

	// Determine if we're running a bundled executable
	execPath := filteredArgs[0]
	var command string
	var cmdArgs []string

	// Check if we're running from a snapshot (bundled executable)
	if strings.HasPrefix(execPath, "/snapshot/") || strings.HasPrefix(execPath, "C:\\snapshot\\") {
		// For bundled executables, use the current executable
		command = execPath
		cmdArgs = filteredArgs[1:]
	} else {
		// For regular Go binaries, use the executable path
		if filepath.Base(execPath) == "go" && len(filteredArgs) > 1 && filteredArgs[1] == "run" {
			// Running with 'go run', need to handle differently
			command = "go"
			cmdArgs = filteredArgs[1:]
		} else {
			command = execPath
			cmdArgs = filteredArgs[1:]
		}
	}

	config := &types.ConfigOutput{
		MCPServers: map[string]types.MCPClientServerConfig{
			serverName: {
				Command: command,
				Args:    cmdArgs,
			},
		},
	}

	return config, nil
}

// FormatMCPClientConfig formats the configuration as JSON
func FormatMCPClientConfig(config *types.ConfigOutput) (string, error) {
	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}
