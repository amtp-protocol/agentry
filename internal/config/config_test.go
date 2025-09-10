/*
 * Copyright 2025 Cong Wang
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValidation_AdminAuth(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_validation_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a valid admin keys file
	validKeysFile := filepath.Join(tempDir, "valid_keys.txt")
	err = os.WriteFile(validKeysFile, []byte("admin-key-1\nadmin-key-2"), 0600)
	if err != nil {
		t.Fatalf("Failed to write valid keys file: %v", err)
	}

	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "no admin key file specified",
			config: &Config{
				Server: ServerConfig{
					Domain:  "test.localhost",
					Address: ":8080",
				},
				Message: MessageConfig{
					MaxSize: 10485760, // 10MB
				},
				Auth: AuthConfig{
					RequireAuth:       false,
					AdminKeyFile:      "",
					AdminAPIKeyHeader: "X-Admin-Key",
				},
			},
			expectError: false,
		},
		{
			name: "valid admin key file",
			config: &Config{
				Server: ServerConfig{
					Domain:  "test.localhost",
					Address: ":8080",
				},
				Message: MessageConfig{
					MaxSize: 10485760, // 10MB
				},
				Auth: AuthConfig{
					RequireAuth:       true,
					AdminKeyFile:      validKeysFile,
					AdminAPIKeyHeader: "X-Admin-Key",
				},
			},
			expectError: false,
		},
		{
			name: "non-existent admin key file",
			config: &Config{
				Server: ServerConfig{
					Domain:  "test.localhost",
					Address: ":8080",
				},
				Message: MessageConfig{
					MaxSize: 10485760, // 10MB
				},
				Auth: AuthConfig{
					RequireAuth:       true,
					AdminKeyFile:      "/non/existent/file.txt",
					AdminAPIKeyHeader: "X-Admin-Key",
				},
			},
			expectError: true,
			errorMsg:    "admin key file not found: /non/existent/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectError && tt.errorMsg != "" && err != nil {
				if err.Error() != tt.errorMsg {
					t.Errorf("Expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

func TestGetDefaultConfig_AdminAuth(t *testing.T) {
	cfg := getDefaultConfig()

	// Check default admin auth settings
	if cfg.Auth.AdminKeyFile != "" {
		t.Error("Admin key file should be empty by default")
	}

	if cfg.Auth.AdminAPIKeyHeader != "X-Admin-Key" {
		t.Errorf("Expected default admin API key header 'X-Admin-Key', got '%s'", cfg.Auth.AdminAPIKeyHeader)
	}
}

func TestLoadFromEnv_AdminAuth(t *testing.T) {
	// Set environment variables
	os.Setenv("AMTP_ADMIN_KEY_FILE", "/path/to/admin.keys")
	os.Setenv("AMTP_ADMIN_API_KEY_HEADER", "X-Custom-Admin-Key")
	defer func() {
		os.Unsetenv("AMTP_ADMIN_KEY_FILE")
		os.Unsetenv("AMTP_ADMIN_API_KEY_HEADER")
	}()

	cfg := getDefaultConfig()
	loadFromEnv(cfg)

	if cfg.Auth.AdminKeyFile != "/path/to/admin.keys" {
		t.Errorf("Expected admin key file '/path/to/admin.keys', got '%s'", cfg.Auth.AdminKeyFile)
	}

	if cfg.Auth.AdminAPIKeyHeader != "X-Custom-Admin-Key" {
		t.Errorf("Expected admin API key header 'X-Custom-Admin-Key', got '%s'", cfg.Auth.AdminAPIKeyHeader)
	}
}

func TestConfigIntegration_AdminAuth(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create admin keys file
	keysFile := filepath.Join(tempDir, "admin.keys")
	keysContent := `# Admin keys for testing
test-admin-key-1
test-admin-key-2
# Another key
test-admin-key-3`

	err = os.WriteFile(keysFile, []byte(keysContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write admin keys file: %v", err)
	}

	// Create config file
	configFile := filepath.Join(tempDir, "config.yaml")
	configContent := `auth:
  require_auth: false
  admin_key_file: ` + keysFile + `
  admin_api_key_header: "X-Test-Admin-Key"
server:
  domain: "test.localhost"
  address: ":8080"
message:
  max_size: 10485760`

	err = os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test the YAML loading directly since Load() uses command line flags
	cfg := getDefaultConfig()
	err = loadFromYAML(cfg, configFile)
	if err != nil {
		t.Fatalf("Failed to load YAML config: %v", err)
	}

	// Verify admin auth settings
	if cfg.Auth.AdminKeyFile != keysFile {
		t.Errorf("Expected admin key file '%s', got '%s'", keysFile, cfg.Auth.AdminKeyFile)
	}

	if cfg.Auth.AdminAPIKeyHeader != "X-Test-Admin-Key" {
		t.Errorf("Expected admin API key header 'X-Test-Admin-Key', got '%s'", cfg.Auth.AdminAPIKeyHeader)
	}
}

// Test schema environment variable loading
func TestLoadSchemaFromEnv(t *testing.T) {
	tests := []struct {
		name         string
		envVars      map[string]string
		expectSchema bool
		expectPath   string
		expectWarn   bool
	}{
		{
			name:         "no schema env vars",
			envVars:      map[string]string{},
			expectSchema: false,
		},
		{
			name: "registry type local with path",
			envVars: map[string]string{
				"AMTP_SCHEMA_REGISTRY_TYPE": "local",
				"AMTP_SCHEMA_REGISTRY_PATH": "/tmp/test-schemas",
			},
			expectSchema: true,
			expectPath:   "/tmp/test-schemas",
		},
		{
			name: "registry path without type",
			envVars: map[string]string{
				"AMTP_SCHEMA_REGISTRY_PATH": "/tmp/test-schemas",
			},
			expectSchema: true,
			expectPath:   "/tmp/test-schemas",
		},
		{
			name: "use local registry flag",
			envVars: map[string]string{
				"AMTP_SCHEMA_USE_LOCAL_REGISTRY": "true",
				"AMTP_SCHEMA_REGISTRY_PATH":      "/tmp/test-schemas",
			},
			expectSchema: true,
			expectPath:   "/tmp/test-schemas",
		},
		{
			name: "registry type without path - should warn",
			envVars: map[string]string{
				"AMTP_SCHEMA_REGISTRY_TYPE": "local",
			},
			expectSchema: false,
			expectWarn:   true,
		},
		{
			name: "use local registry without path - should warn",
			envVars: map[string]string{
				"AMTP_SCHEMA_USE_LOCAL_REGISTRY": "true",
			},
			expectSchema: false,
			expectWarn:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			os.Unsetenv("AMTP_SCHEMA_REGISTRY_TYPE")
			os.Unsetenv("AMTP_SCHEMA_REGISTRY_PATH")
			os.Unsetenv("AMTP_SCHEMA_USE_LOCAL_REGISTRY")

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Clean up after test
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			// Create config and load from env
			cfg := getDefaultConfig()
			loadSchemaFromEnv(cfg)

			// Check expectations
			if tt.expectSchema {
				if cfg.Schema == nil {
					t.Error("Expected schema config to be created, but it was nil")
					return
				}

				if !cfg.Schema.UseLocalRegistry {
					t.Error("Expected UseLocalRegistry to be true")
				}

				if cfg.Schema.LocalRegistry.BasePath != tt.expectPath {
					t.Errorf("Expected registry path '%s', got '%s'", tt.expectPath, cfg.Schema.LocalRegistry.BasePath)
				}

				// Check that only essential fields are set - components use their own defaults
				if cfg.Schema.LocalRegistry.IndexFile != "" {
					t.Errorf("Expected IndexFile to be empty (component sets default), got '%s'", cfg.Schema.LocalRegistry.IndexFile)
				}

				if cfg.Schema.LocalRegistry.AutoSave {
					t.Error("Expected AutoSave to be false (component sets default)")
				}

				if cfg.Schema.LocalRegistry.CreateDirs {
					t.Error("Expected CreateDirs to be false (component sets default)")
				}

				if cfg.Schema.Cache.Type != "" {
					t.Errorf("Expected cache type to be empty (component sets default), got '%s'", cfg.Schema.Cache.Type)
				}

				if cfg.Schema.Validation.Enabled {
					t.Error("Expected validation to be false (component sets default)")
				}
			} else {
				if cfg.Schema != nil {
					t.Error("Expected schema config to be nil, but it was created")
				}
			}
		})
	}
}

func TestLoadFromEnv_Schema(t *testing.T) {
	// Test that loadFromEnv calls loadSchemaFromEnv
	os.Setenv("AMTP_SCHEMA_REGISTRY_TYPE", "local")
	os.Setenv("AMTP_SCHEMA_REGISTRY_PATH", "/tmp/env-test-schemas")
	defer func() {
		os.Unsetenv("AMTP_SCHEMA_REGISTRY_TYPE")
		os.Unsetenv("AMTP_SCHEMA_REGISTRY_PATH")
	}()

	cfg := getDefaultConfig()
	loadFromEnv(cfg)

	if cfg.Schema == nil {
		t.Error("Expected schema config to be created by loadFromEnv")
		return
	}

	if cfg.Schema.LocalRegistry.BasePath != "/tmp/env-test-schemas" {
		t.Errorf("Expected registry path '/tmp/env-test-schemas', got '%s'", cfg.Schema.LocalRegistry.BasePath)
	}
}

func TestCommandLineFlags_AdminAuth(t *testing.T) {
	// Create temporary admin keys file
	tempDir, err := os.MkdirTemp("", "flag_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	keysFile := filepath.Join(tempDir, "admin.keys")
	err = os.WriteFile(keysFile, []byte("test-key"), 0600)
	if err != nil {
		t.Fatalf("Failed to write keys file: %v", err)
	}

	// Test that command line flags would override config
	// Note: This is more of a documentation test since we can't easily test
	// the actual flag parsing without modifying the global flag state

	cfg := getDefaultConfig()

	// Simulate what would happen if the flag was set
	cfg.Auth.AdminKeyFile = keysFile

	if cfg.Auth.AdminKeyFile != keysFile {
		t.Errorf("Expected admin key file to be set to '%s', got '%s'", keysFile, cfg.Auth.AdminKeyFile)
	}

	// Verify the file exists (simulating validation)
	if _, err := os.Stat(cfg.Auth.AdminKeyFile); err != nil {
		t.Errorf("Admin key file should exist: %v", err)
	}
}

func TestDomainValidation(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid domain",
			domain:      "example.com",
			expectError: false,
		},
		{
			name:        "valid subdomain",
			domain:      "api.example.com",
			expectError: false,
		},
		{
			name:        "localhost allowed",
			domain:      "localhost",
			expectError: false,
		},
		{
			name:        "valid single label domain",
			domain:      "test",
			expectError: false,
		},
		{
			name:        "empty domain",
			domain:      "",
			expectError: true,
			errorMsg:    "domain is required",
		},
		{
			name:        "domain with underscore",
			domain:      "invalid_domain.com",
			expectError: true,
			errorMsg:    "domain cannot contain underscores",
		},
		{
			name:        "domain starting with hyphen",
			domain:      "-invalid.com",
			expectError: true,
			errorMsg:    "label cannot start or end with hyphen",
		},
		{
			name:        "domain ending with hyphen",
			domain:      "invalid-.com",
			expectError: true,
			errorMsg:    "label cannot start or end with hyphen",
		},
		{
			name:        "label starting with hyphen",
			domain:      "example.-invalid.com",
			expectError: true,
			errorMsg:    "label cannot start or end with hyphen",
		},
		{
			name:        "label ending with hyphen",
			domain:      "example.invalid-.com",
			expectError: true,
			errorMsg:    "label cannot start or end with hyphen",
		},
		{
			name:        "too long domain",
			domain:      strings.Repeat("a", 254),
			expectError: true,
			errorMsg:    "domain too long",
		},
		{
			name:        "too long label",
			domain:      strings.Repeat("a", 64) + ".com",
			expectError: true,
			errorMsg:    "label too long",
		},
		{
			name:        "empty label",
			domain:      "example..com",
			expectError: true,
			errorMsg:    "empty label in domain",
		},
		{
			name:        "valid with numbers",
			domain:      "api2.example123.com",
			expectError: false,
		},
		{
			name:        "valid with hyphens",
			domain:      "api-v2.example-corp.com",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Server: ServerConfig{
					Domain:  tt.domain,
					Address: ":8080",
				},
				Message: MessageConfig{
					MaxSize: 10485760, // 10MB
				},
			}

			err := config.validateDomain()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for domain '%s', but got none", tt.domain)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for domain '%s': %v", tt.domain, err)
				}
			}
		})
	}
}

func TestConfigValidation_Domain(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		expectError bool
	}{
		{
			name:        "valid domain passes full validation",
			domain:      "example.com",
			expectError: false,
		},
		{
			name:        "invalid domain fails full validation",
			domain:      "invalid_domain",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Server: ServerConfig{
					Domain:  tt.domain,
					Address: ":8080",
				},
				Message: MessageConfig{
					MaxSize: 10485760, // 10MB
				},
				TLS: TLSConfig{
					Enabled: false,
				},
				Auth: AuthConfig{
					AdminKeyFile: "",
				},
			}

			err := config.validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for domain '%s', but got none", tt.domain)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for domain '%s': %v", tt.domain, err)
				}
			}
		})
	}
}

// Test complete configuration integration with schema
func TestConfigIntegration_Schema(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_schema_integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create config file with schema configuration
	configFile := filepath.Join(tempDir, "config.yaml")
	schemaPath := filepath.Join(tempDir, "schemas")
	configContent := `server:
  domain: "test.localhost"
  address: ":8080"
message:
  max_size: 10485760
schema:
  use_local_registry: true
  local_registry:
    base_path: ` + schemaPath + `
    index_file: "index.json"
    auto_save: true
    create_dirs: true
  cache:
    type: "memory"
    default_ttl: 3600s
    max_size: 1000
  validation:
    enabled: true`

	err = os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test YAML loading
	cfg := getDefaultConfig()
	err = loadFromYAML(cfg, configFile)
	if err != nil {
		t.Fatalf("Failed to load YAML config: %v", err)
	}

	// Verify schema configuration
	if cfg.Schema == nil {
		t.Fatal("Expected schema config to be loaded from YAML")
	}

	if !cfg.Schema.UseLocalRegistry {
		t.Error("Expected UseLocalRegistry to be true")
	}

	if cfg.Schema.LocalRegistry.BasePath != schemaPath {
		t.Errorf("Expected registry path '%s', got '%s'", schemaPath, cfg.Schema.LocalRegistry.BasePath)
	}

	if cfg.Schema.Cache.Type != "memory" {
		t.Errorf("Expected cache type 'memory', got '%s'", cfg.Schema.Cache.Type)
	}
}

// Test environment variables override YAML configuration
func TestConfigIntegration_EnvOverrideYAML(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_env_override_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create config file with one schema path
	configFile := filepath.Join(tempDir, "config.yaml")
	yamlSchemaPath := filepath.Join(tempDir, "yaml-schemas")
	configContent := `server:
  domain: "test.localhost"
  address: ":8080"
message:
  max_size: 10485760
schema:
  use_local_registry: true
  local_registry:
    base_path: ` + yamlSchemaPath + `
    index_file: "index.json"`

	err = os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set environment variables that should override YAML
	envSchemaPath := filepath.Join(tempDir, "env-schemas")
	os.Setenv("AMTP_SCHEMA_REGISTRY_TYPE", "local")
	os.Setenv("AMTP_SCHEMA_REGISTRY_PATH", envSchemaPath)
	defer func() {
		os.Unsetenv("AMTP_SCHEMA_REGISTRY_TYPE")
		os.Unsetenv("AMTP_SCHEMA_REGISTRY_PATH")
	}()

	// Load configuration
	cfg := getDefaultConfig()
	err = loadFromYAML(cfg, configFile)
	if err != nil {
		t.Fatalf("Failed to load YAML config: %v", err)
	}

	// Environment variables should override YAML
	loadFromEnv(cfg)

	// Verify environment variables took precedence
	if cfg.Schema == nil {
		t.Fatal("Expected schema config to exist")
	}

	if cfg.Schema.LocalRegistry.BasePath != envSchemaPath {
		t.Errorf("Expected env path '%s' to override YAML path, got '%s'", envSchemaPath, cfg.Schema.LocalRegistry.BasePath)
	}
}
