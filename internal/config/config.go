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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/schema"
	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Server  ServerConfig          `yaml:"server"`
	TLS     TLSConfig             `yaml:"tls"`
	DNS     DNSConfig             `yaml:"dns"`
	Message MessageConfig         `yaml:"message"`
	Auth    AuthConfig            `yaml:"auth"`
	Logging LoggingConfig         `yaml:"logging"`
	Metrics *MetricsConfig        `yaml:"metrics,omitempty"`
	Schema  *schema.ManagerConfig `yaml:"schema,omitempty"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Address      string        `yaml:"address"`
	Domain       string        `yaml:"domain"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CertFile   string `yaml:"cert_file"`
	KeyFile    string `yaml:"key_file"`
	MinVersion string `yaml:"min_version"`
}

// DNSConfig holds DNS discovery configuration
type DNSConfig struct {
	CacheTTL    time.Duration     `yaml:"cache_ttl"`
	Timeout     time.Duration     `yaml:"timeout"`
	Resolvers   []string          `yaml:"resolvers"`
	MockMode    bool              `yaml:"mock_mode"`
	MockRecords map[string]string `yaml:"mock_records"`
	AllowHTTP   bool              `yaml:"allow_http"`
}

// MessageConfig holds message processing configuration
type MessageConfig struct {
	MaxSize           int64         `yaml:"max_size"`
	IdempotencyTTL    time.Duration `yaml:"idempotency_ttl"`
	ValidationEnabled bool          `yaml:"validation_enabled"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	RequireAuth       bool     `yaml:"require_auth"`
	Methods           []string `yaml:"methods"`
	APIKeyHeader      string   `yaml:"api_key_header"`
	AdminKeyFile      string   `yaml:"admin_key_file"`       // Path to admin API key file
	AdminAPIKeyHeader string   `yaml:"admin_api_key_header"` // Header for admin API key
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
}

// Load loads configuration from YAML file and environment variables
// Command line flags take precedence over environment variables
// Environment variables take precedence over YAML file values
func Load() (*Config, error) {
	// Parse command line flags
	configFile := flag.String("config", "", "Path to configuration file (YAML)")
	adminKeyFile := flag.String("admin-key-file", "", "Path to admin API key file")
	flag.Parse()

	// Start with default configuration
	cfg := getDefaultConfig()

	// Load from YAML file if specified or if default files exist
	if err := loadFromYAML(cfg, *configFile); err != nil {
		return nil, fmt.Errorf("failed to load YAML config: %w", err)
	}

	// Override with environment variables
	loadFromEnv(cfg)

	// Override with command line flags
	if *adminKeyFile != "" {
		cfg.Auth.AdminKeyFile = *adminKeyFile
	}

	// Validate configuration
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// getDefaultConfig returns a configuration with default values
func getDefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:      ":8443",
			Domain:       "localhost",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		TLS: TLSConfig{
			Enabled:    true,
			CertFile:   "",
			KeyFile:    "",
			MinVersion: "1.3",
		},
		DNS: DNSConfig{
			CacheTTL:    5 * time.Minute,
			Timeout:     5 * time.Second,
			Resolvers:   []string{"8.8.8.8:53", "1.1.1.1:53"},
			MockMode:    false,
			MockRecords: getDefaultMockRecords(),
			AllowHTTP:   false,
		},
		Message: MessageConfig{
			MaxSize:           10 * 1024 * 1024,   // 10MB
			IdempotencyTTL:    7 * 24 * time.Hour, // 7 days
			ValidationEnabled: true,
		},
		Auth: AuthConfig{
			RequireAuth:       false,
			Methods:           []string{"domain", "apikey"},
			APIKeyHeader:      "X-API-Key",
			AdminKeyFile:      "",            // No admin key file by default
			AdminAPIKeyHeader: "X-Admin-Key", // Header for admin authentication
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// loadFromYAML loads configuration from a YAML file
func loadFromYAML(cfg *Config, configFile string) error {
	// Only load config file if explicitly provided via command line
	if configFile == "" {
		return nil
	}

	filePath := configFile

	// Read and parse YAML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse YAML config file %s: %w", filePath, err)
	}

	return nil
}

// loadFromEnv overrides configuration with environment variables
func loadFromEnv(cfg *Config) {
	// Server configuration
	if val := getEnv("AMTP_SERVER_ADDRESS", ""); val != "" {
		cfg.Server.Address = val
	}
	if val := getEnv("AMTP_DOMAIN", ""); val != "" {
		cfg.Server.Domain = val
	}
	if val := getDurationEnv("AMTP_READ_TIMEOUT", 0); val != 0 {
		cfg.Server.ReadTimeout = val
	}
	if val := getDurationEnv("AMTP_WRITE_TIMEOUT", 0); val != 0 {
		cfg.Server.WriteTimeout = val
	}
	if val := getDurationEnv("AMTP_IDLE_TIMEOUT", 0); val != 0 {
		cfg.Server.IdleTimeout = val
	}

	// TLS configuration
	if val := getBoolEnvWithDefault("AMTP_TLS_ENABLED", cfg.TLS.Enabled); val != cfg.TLS.Enabled {
		cfg.TLS.Enabled = val
	}
	if val := getEnv("AMTP_TLS_CERT_FILE", ""); val != "" {
		cfg.TLS.CertFile = val
	}
	if val := getEnv("AMTP_TLS_KEY_FILE", ""); val != "" {
		cfg.TLS.KeyFile = val
	}
	if val := getEnv("AMTP_TLS_MIN_VERSION", ""); val != "" {
		cfg.TLS.MinVersion = val
	}

	// DNS configuration
	if val := getDurationEnv("AMTP_DNS_CACHE_TTL", 0); val != 0 {
		cfg.DNS.CacheTTL = val
	}
	if val := getDurationEnv("AMTP_DNS_TIMEOUT", 0); val != 0 {
		cfg.DNS.Timeout = val
	}
	if val := getEnv("AMTP_DNS_RESOLVERS", ""); val != "" {
		cfg.DNS.Resolvers = strings.Split(val, ",")
	}
	if val := getBoolEnvWithDefault("AMTP_DNS_MOCK_MODE", cfg.DNS.MockMode); val != cfg.DNS.MockMode {
		cfg.DNS.MockMode = val
	}
	if val := getBoolEnvWithDefault("AMTP_DNS_ALLOW_HTTP", cfg.DNS.AllowHTTP); val != cfg.DNS.AllowHTTP {
		cfg.DNS.AllowHTTP = val
	}

	// Load mock records from environment if provided
	if mockRecords := loadMockRecords(); len(mockRecords) > 0 {
		cfg.DNS.MockRecords = mockRecords
	}

	// Message configuration
	if val := getInt64Env("AMTP_MESSAGE_MAX_SIZE", 0); val != 0 {
		cfg.Message.MaxSize = val
	}
	if val := getDurationEnv("AMTP_IDEMPOTENCY_TTL", 0); val != 0 {
		cfg.Message.IdempotencyTTL = val
	}
	if val := getBoolEnvWithDefault("AMTP_MESSAGE_VALIDATION_ENABLED", cfg.Message.ValidationEnabled); val != cfg.Message.ValidationEnabled {
		cfg.Message.ValidationEnabled = val
	}

	// Auth configuration
	if val := getBoolEnvWithDefault("AMTP_AUTH_REQUIRED", cfg.Auth.RequireAuth); val != cfg.Auth.RequireAuth {
		cfg.Auth.RequireAuth = val
	}
	if val := getEnv("AMTP_AUTH_API_KEY_HEADER", ""); val != "" {
		cfg.Auth.APIKeyHeader = val
	}
	if val := getEnv("AMTP_ADMIN_KEY_FILE", ""); val != "" {
		cfg.Auth.AdminKeyFile = val
	}
	if val := getEnv("AMTP_ADMIN_API_KEY_HEADER", ""); val != "" {
		cfg.Auth.AdminAPIKeyHeader = val
	}

	// Logging configuration
	if val := getEnv("AMTP_LOG_LEVEL", ""); val != "" {
		cfg.Logging.Level = val
	}
	if val := getEnv("AMTP_LOG_FORMAT", ""); val != "" {
		cfg.Logging.Format = val
	}

	// Metrics configuration
	loadMetricsFromEnv(cfg)

	// Schema configuration
	loadSchemaFromEnv(cfg)
}

// validate validates the configuration
func (c *Config) validate() error {
	// Validate server domain
	if err := c.validateDomain(); err != nil {
		return fmt.Errorf("invalid server domain: %w", err)
	}

	if c.TLS.Enabled && (c.TLS.CertFile == "" || c.TLS.KeyFile == "") {
		return fmt.Errorf("TLS cert and key files are required when TLS is enabled")
	}

	if c.Message.MaxSize <= 0 {
		return fmt.Errorf("message max size must be positive")
	}

	// Validate admin key file if specified
	if c.Auth.AdminKeyFile != "" {
		if _, err := os.Stat(c.Auth.AdminKeyFile); err != nil {
			return fmt.Errorf("admin key file not found: %s", c.Auth.AdminKeyFile)
		}
	}

	return nil
}

// validateDomain validates the server domain configuration
func (c *Config) validateDomain() error {
	domain := strings.TrimSpace(c.Server.Domain)
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	// Allow localhost for development
	if domain == "localhost" {
		return nil
	}

	// Check for invalid characters first
	if strings.Contains(domain, "_") {
		return fmt.Errorf("domain cannot contain underscores: %s", domain)
	}

	// Check length limits
	if len(domain) > 253 {
		return fmt.Errorf("domain too long (max 253 characters): %s", domain)
	}

	// Validate each label
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 {
			return fmt.Errorf("empty label in domain: %s", domain)
		}
		if len(label) > 63 {
			return fmt.Errorf("label too long (max 63 characters) in domain: %s", domain)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("label cannot start or end with hyphen in domain: %s", domain)
		}
	}

	// Validate domain format using regex (after specific checks)
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s", domain)
	}

	// Warn about IP addresses (not recommended but not invalid)
	if net.ParseIP(domain) != nil {
		fmt.Printf("WARNING: Using IP address (%s) as domain is not recommended. Consider using a proper domain name.\n", domain)
	}

	return nil
}

// Helper functions for environment variable parsing
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getInt64Env(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getBoolEnvWithDefault gets a boolean environment variable with a specific default
func getBoolEnvWithDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getDefaultMockRecords returns default mock DNS records
func getDefaultMockRecords() map[string]string {
	return map[string]string{
		"localhost":   "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*,agntcy:dev.*",
		"test.local":  "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*",
		"dev.local":   "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:dev.*",
		"example.com": "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:example.*",
	}
}

// loadMockRecords loads mock DNS records from environment or returns empty map
func loadMockRecords() map[string]string {
	// Check if mock records are provided via environment variable
	if mockRecordsJSON := os.Getenv("AMTP_DNS_MOCK_RECORDS"); mockRecordsJSON != "" {
		var records map[string]string
		if err := json.Unmarshal([]byte(mockRecordsJSON), &records); err == nil {
			return records
		}
	}

	// Return empty map - let the caller decide on defaults
	return map[string]string{}
}

// loadSchemaFromEnv loads schema configuration from environment variables
func loadSchemaFromEnv(cfg *Config) {
	registryType := getEnv("AMTP_SCHEMA_REGISTRY_TYPE", "")
	registryPath := getEnv("AMTP_SCHEMA_REGISTRY_PATH", "")
	useLocalRegistry := getBoolEnv("AMTP_SCHEMA_USE_LOCAL_REGISTRY", false)

	if registryType == "local" || registryPath != "" || useLocalRegistry {
		if registryPath == "" {
			log.Printf("WARNING: Schema management enabled but AMTP_SCHEMA_REGISTRY_PATH not set. Schema registration will fail.")
			return
		}

		log.Printf("INFO: Schema management enabled with local registry at: %s", registryPath)

		if cfg.Schema == nil {
			cfg.Schema = &schema.ManagerConfig{}
		}

		cfg.Schema.UseLocalRegistry = true
		cfg.Schema.LocalRegistry.BasePath = registryPath
	} else {
		log.Printf("INFO: Schema management not configured. Set AMTP_SCHEMA_REGISTRY_TYPE=local and AMTP_SCHEMA_REGISTRY_PATH to enable.")
	}
}

// loadMetricsFromEnv loads metrics configuration from environment variables
func loadMetricsFromEnv(cfg *Config) {
	// Check if metrics should be enabled
	if getBoolEnv("AMTP_METRICS_ENABLED", false) {
		log.Printf("INFO: Metrics enabled via environment variable")

		if cfg.Metrics == nil {
			cfg.Metrics = &MetricsConfig{}
		}
		cfg.Metrics.Enabled = true
	} else {
		log.Printf("INFO: Metrics not enabled. Set AMTP_METRICS_ENABLED=true to enable metrics.")
	}
}
