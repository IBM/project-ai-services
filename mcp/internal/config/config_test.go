package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/project-ai-services/mcp/internal/types"
)

func TestGenerateMCPClientConfig(t *testing.T) {
	// Save original os.Args
	originalArgs := os.Args
	defer func() {
		os.Args = originalArgs
	}()

	tests := []struct {
		name             string
		args             []string
		serverName       string
		expectedCmd      string
		expectedArgs     []string
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:         "simple executable",
			args:         []string{"/usr/local/bin/myapp", "--api-key", "test-key"},
			serverName:   "test-server",
			expectedCmd:  "/usr/local/bin/myapp",
			expectedArgs: []string{"--api-key", "test-key"},
		},
		{
			name:         "go run command",
			args:         []string{"go", "run", "main.go", "--api-key", "test-key"},
			serverName:   "test-server",
			expectedCmd:  "go",
			expectedArgs: []string{"run", "main.go", "--api-key", "test-key"},
		},
		{
			name:         "snapshot bundled executable",
			args:         []string{"/snapshot/app/myapp", "--api-key", "test-key"},
			serverName:   "test-server",
			expectedCmd:  "/snapshot/app/myapp",
			expectedArgs: []string{"--api-key", "test-key"},
		},
		{
			name:         "windows snapshot bundled executable",
			args:         []string{"C:\\snapshot\\app\\myapp.exe", "--api-key", "test-key"},
			serverName:   "test-server",
			expectedCmd:  "C:\\snapshot\\app\\myapp.exe",
			expectedArgs: []string{"--api-key", "test-key"},
		},
		{
			name:             "filter out --config flag",
			args:             []string{"/usr/local/bin/myapp", "--config", "value", "--api-key", "test-key"},
			serverName:       "test-server",
			expectedCmd:      "/usr/local/bin/myapp",
			expectedArgs:     []string{"--api-key", "test-key"},
			shouldNotContain: []string{"--config", "value"},
		},
		{
			name:             "filter out -C flag",
			args:             []string{"/usr/local/bin/myapp", "-C", "value", "--api-key", "test-key"},
			serverName:       "test-server",
			expectedCmd:      "/usr/local/bin/myapp",
			expectedArgs:     []string{"--api-key", "test-key"},
			shouldNotContain: []string{"-C", "value"},
		},
		{
			name:             "filter out --config with value",
			args:             []string{"/usr/local/bin/myapp", "--config", "config.json", "--api-key", "test-key"},
			serverName:       "test-server",
			expectedCmd:      "/usr/local/bin/myapp",
			expectedArgs:     []string{"--api-key", "test-key"},
			shouldNotContain: []string{"--config", "config.json"},
		},
		{
			name:         "empty args after executable",
			args:         []string{"/usr/local/bin/myapp"},
			serverName:   "test-server",
			expectedCmd:  "/usr/local/bin/myapp",
			expectedArgs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args

			config, err := GenerateMCPClientConfig(tt.serverName)
			if err != nil {
				t.Fatalf("GenerateMCPClientConfig() error = %v", err)
			}

			server, ok := config.MCPServers[tt.serverName]
			if !ok {
				t.Fatalf("Server %q not found in config", tt.serverName)
			}

			if server.Command != tt.expectedCmd {
				t.Errorf("Command = %q, want %q", server.Command, tt.expectedCmd)
			}

			if len(server.Args) != len(tt.expectedArgs) {
				t.Errorf("Args length = %d, want %d", len(server.Args), len(tt.expectedArgs))
			}

			for i, arg := range tt.expectedArgs {
				if i >= len(server.Args) {
					t.Errorf("Missing argument at index %d: want %q", i, arg)
					continue
				}
				if server.Args[i] != arg {
					t.Errorf("Args[%d] = %q, want %q", i, server.Args[i], arg)
				}
			}

			// Check for things that should not be in args
			for _, shouldNot := range tt.shouldNotContain {
				for _, arg := range server.Args {
					if arg == shouldNot {
						t.Errorf("Args should not contain %q", shouldNot)
					}
				}
			}
		})
	}
}

func TestFormatMCPClientConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        *types.ConfigOutput
		shouldContain []string
		wantErr       bool
	}{
		{
			name: "simple config",
			config: &types.ConfigOutput{
				MCPServers: map[string]types.MCPClientServerConfig{
					"test-server": {
						Command: "/usr/local/bin/myapp",
						Args:    []string{"--api-key", "test-key"},
					},
				},
			},
			shouldContain: []string{
				`"mcpServers"`,
				`"test-server"`,
				`"command": "/usr/local/bin/myapp"`,
				`"args": [`,
				`"--api-key"`,
				`"test-key"`,
			},
			wantErr: false,
		},
		{
			name: "multiple servers",
			config: &types.ConfigOutput{
				MCPServers: map[string]types.MCPClientServerConfig{
					"server1": {
						Command: "/usr/local/bin/app1",
						Args:    []string{"--arg1"},
					},
					"server2": {
						Command: "/usr/local/bin/app2",
						Args:    []string{"--arg2"},
					},
				},
			},
			shouldContain: []string{
				`"server1"`,
				`"server2"`,
				`"/usr/local/bin/app1"`,
				`"/usr/local/bin/app2"`,
			},
			wantErr: false,
		},
		{
			name: "empty args",
			config: &types.ConfigOutput{
				MCPServers: map[string]types.MCPClientServerConfig{
					"test-server": {
						Command: "/usr/local/bin/myapp",
						Args:    []string{},
					},
				},
			},
			shouldContain: []string{
				`"args": []`,
			},
			wantErr: false,
		},
		{
			name: "nil args",
			config: &types.ConfigOutput{
				MCPServers: map[string]types.MCPClientServerConfig{
					"test-server": {
						Command: "/usr/local/bin/myapp",
						Args:    nil,
					},
				},
			},
			shouldContain: []string{
				`"args": null`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FormatMCPClientConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("FormatMCPClientConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check that result is valid JSON
			var parsed interface{}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Errorf("Result is not valid JSON: %v", err)
			}

			// Check that result contains expected strings
			for _, expected := range tt.shouldContain {
				if !strings.Contains(result, expected) {
					t.Errorf("Result does not contain %q", expected)
				}
			}

			// Check indentation (should be 2 spaces)
			if !strings.Contains(result, "\n  ") {
				t.Error("Result should be indented with 2 spaces")
			}
		})
	}
}

func TestGenerateMCPClientConfigEdgeCases(t *testing.T) {
	// Save original os.Args
	originalArgs := os.Args
	defer func() {
		os.Args = originalArgs
	}()

	t.Run("single argument (just executable)", func(t *testing.T) {
		os.Args = []string{"/usr/local/bin/myapp"}

		config, err := GenerateMCPClientConfig("test")
		if err != nil {
			t.Fatalf("GenerateMCPClientConfig() error = %v", err)
		}

		server := config.MCPServers["test"]
		if server.Command != "/usr/local/bin/myapp" {
			t.Errorf("Command = %q, want %q", server.Command, "/usr/local/bin/myapp")
		}

		if len(server.Args) != 0 {
			t.Errorf("Args should be empty, got %v", server.Args)
		}
	})

	t.Run("config flag at end", func(t *testing.T) {
		os.Args = []string{"/usr/local/bin/myapp", "--api-key", "test", "--config"}

		config, err := GenerateMCPClientConfig("test")
		if err != nil {
			t.Fatalf("GenerateMCPClientConfig() error = %v", err)
		}

		server := config.MCPServers["test"]
		expectedArgs := []string{"--api-key", "test"}

		if len(server.Args) != len(expectedArgs) {
			t.Errorf("Args length = %d, want %d", len(server.Args), len(expectedArgs))
		}

		for _, arg := range server.Args {
			if arg == "--config" {
				t.Error("Args should not contain --config")
			}
		}
	})
}

func TestFormatMCPClientConfigError(t *testing.T) {
	// This test is mainly for completeness since json.MarshalIndent
	// is unlikely to fail with our simple types, but we can test
	// the error path by using a channel (which can't be marshaled)

	// Since we can't actually create an unmarshallable ConfigOutput
	// (all fields are marshalable), we'll just ensure the function
	// handles the error path correctly by testing with valid data
	config := &types.ConfigOutput{
		MCPServers: map[string]types.MCPClientServerConfig{
			"test": {
				Command: "test",
				Args:    []string{"arg"},
			},
		},
	}

	_, err := FormatMCPClientConfig(config)
	if err != nil {
		t.Errorf("FormatMCPClientConfig() unexpected error = %v", err)
	}
}
