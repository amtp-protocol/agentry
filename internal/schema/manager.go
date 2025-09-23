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

package schema

import (
	"context"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// Manager provides a unified interface for all schema-related operations
type Manager struct {
	registryClient       RegistryClient
	validator            Validator
	cache                Cache
	negotiationEngine    *NegotiationEngine
	compatibilityChecker *CompatibilityChecker
	pipeline             *ValidationPipeline
	errorReporter        *ErrorReporter
	config               ManagerConfig
}

// ManagerConfig holds configuration for the schema manager
type ManagerConfig struct {
	Registry         RegistryConfig      `yaml:"registry" json:"registry"`
	LocalRegistry    LocalRegistryConfig `yaml:"local_registry" json:"local_registry"`
	Cache            CacheConfig         `yaml:"cache" json:"cache"`
	Validation       ValidatorConfig     `yaml:"validation" json:"validation"`
	Negotiation      NegotiationConfig   `yaml:"negotiation" json:"negotiation"`
	Compatibility    CompatibilityConfig `yaml:"compatibility" json:"compatibility"`
	Pipeline         PipelineConfig      `yaml:"pipeline" json:"pipeline"`
	ErrorReporting   ErrorReportConfig   `yaml:"error_reporting" json:"error_reporting"`
	UseLocalRegistry bool                `yaml:"use_local_registry" json:"use_local_registry"`
}

// NewManager creates a new schema manager with all components
func NewManager(config ManagerConfig) (*Manager, error) {
	// Create registry client
	var registryClient RegistryClient
	if config.UseLocalRegistry {
		// Use local registry
		localRegistry, err := NewLocalRegistry(config.LocalRegistry)
		if err != nil {
			return nil, fmt.Errorf("failed to create local registry: %w", err)
		}
		registryClient = localRegistry
	} else if config.Registry.BaseURL != "" {
		registryClient = NewHTTPRegistryClient(config.Registry)
	} else {
		registryClient = NewMockRegistryClient()
	}

	// Create cache
	cacheFactory := &DefaultCacheFactory{}
	cache, err := cacheFactory.CreateCache(config.Cache)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	// Create cached registry client
	cachedRegistryClient := NewCachedRegistryClient(registryClient, cache)

	// Create validator
	validator := NewJSONSchemaValidator(cachedRegistryClient, config.Validation)

	// Create negotiation engine
	negotiationEngine := NewNegotiationEngine(cachedRegistryClient, config.Negotiation)

	// Create compatibility checker
	compatibilityChecker := NewCompatibilityChecker(cachedRegistryClient, config.Compatibility)

	// Create validation pipeline
	pipeline := NewValidationPipeline(cachedRegistryClient, validator, config.Pipeline)

	// Create error reporter
	errorReporter := NewErrorReporter(config.ErrorReporting)

	return &Manager{
		registryClient:       cachedRegistryClient,
		validator:            validator,
		cache:                cache,
		negotiationEngine:    negotiationEngine,
		compatibilityChecker: compatibilityChecker,
		pipeline:             pipeline,
		errorReporter:        errorReporter,
		config:               config,
	}, nil
}

// GetRegistry returns the underlying registry client
func (m *Manager) GetRegistry() RegistryClient {
	return m.registryClient
}

// ValidateMessage validates a message using the complete schema framework
func (m *Manager) ValidateMessage(ctx context.Context, message *types.Message) (*ValidationReport, error) {
	startTime := time.Now()

	// Handle nil message gracefully
	if message == nil {
		return &ValidationReport{
			Timestamp: startTime,
			Valid:     false,
			Errors: []ValidationError{
				{
					Field:   "message",
					Message: "message cannot be nil",
					Code:    "NIL_MESSAGE",
				},
			},
		}, nil
	}

	report := &ValidationReport{
		MessageID: message.MessageID,
		SchemaID:  message.Schema,
		Timestamp: startTime,
		Valid:     true,
	}

	// Parse schema identifier - schema is now required for schema manager validation
	// Per-agent schema requirements are handled at the validation layer
	if message.Schema == "" {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "schema",
			Message: "schema identifier is required for schema validation",
			Code:    "SCHEMA_REQUIRED",
		})
		return report, nil
	}

	schemaID, err := ParseSchemaIdentifier(message.Schema)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "schema",
			Message: fmt.Sprintf("invalid schema identifier: %s", err.Error()),
			Code:    "INVALID_SCHEMA_ID",
			Value:   message.Schema,
		})
		return report, nil
	}

	// Perform schema negotiation if needed
	negotiatedSchema, err := m.negotiationEngine.NegotiateSchema(ctx, *schemaID)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "schema",
			Message: fmt.Sprintf("schema negotiation failed: %s", err.Error()),
			Code:    "SCHEMA_NEGOTIATION_FAILED",
			Value:   message.Schema,
		})
		return report, nil
	}

	// Use negotiated schema if different
	if negotiatedSchema.String() != schemaID.String() {
		report.NegotiatedSchema = negotiatedSchema.String()
		schemaID = negotiatedSchema
	}

	// Validate using pipeline
	validationResult, err := m.pipeline.ValidateMessage(ctx, message, *schemaID)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Field:   "payload",
			Message: fmt.Sprintf("validation pipeline failed: %s", err.Error()),
			Code:    "PIPELINE_FAILED",
		})
		return report, nil
	}

	// Copy validation results
	report.Valid = validationResult.Valid
	report.Errors = validationResult.Errors
	report.Warnings = validationResult.Warnings

	// Calculate processing time
	report.ProcessingTime = time.Since(startTime)

	// Report errors if any
	if !report.Valid {
		m.errorReporter.ReportValidationErrors(ctx, report)
	}

	return report, nil
}

// GetSchema retrieves a schema by identifier
func (m *Manager) GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	return m.registryClient.GetSchema(ctx, id)
}

// ListSchemas lists available schemas
func (m *Manager) ListSchemas(ctx context.Context, pattern string) ([]SchemaIdentifier, error) {
	return m.registryClient.ListSchemas(ctx, pattern)
}

// RegisterSchema registers a new schema
func (m *Manager) RegisterSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	return m.registryClient.RegisterSchema(ctx, schema, metadata)
}

// UpdateSchema updates an existing schema
func (m *Manager) UpdateSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	return m.registryClient.RegisterOrUpdateSchema(ctx, schema, metadata)
}

// DeleteSchema deletes a schema
func (m *Manager) DeleteSchema(ctx context.Context, id SchemaIdentifier) error {
	// Clear from cache first
	if cachedClient, ok := m.registryClient.(*CachedRegistryClient); ok {
		cachedClient.InvalidateCache(ctx, id) // #nosec G104 -- ignore error
	}

	return m.registryClient.DeleteSchema(ctx, id)
}

// CheckCompatibility checks if two schemas are compatible
func (m *Manager) CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error) {
	return m.compatibilityChecker.CheckCompatibility(ctx, current, new)
}

// ValidatePayload validates a payload against a schema
func (m *Manager) ValidatePayload(ctx context.Context, payload []byte, schemaID SchemaIdentifier) (*ValidationResult, error) {
	return m.validator.ValidatePayload(ctx, payload, schemaID)
}

// ClearCache clears the schema cache
func (m *Manager) ClearCache(ctx context.Context) error {
	return m.cache.Clear(ctx)
}

// GetStats returns comprehensive statistics
func (m *Manager) GetStats(ctx context.Context) (*ManagerStats, error) {
	registryStats := m.registryClient.GetStats()

	var cacheStats CacheStats
	if memCache, ok := m.cache.(*MemoryCache); ok {
		cacheStats = memCache.GetStats()
	}

	return &ManagerStats{
		Registry: registryStats,
		Cache:    cacheStats,
		Config:   m.config,
	}, nil
}

// Shutdown gracefully shuts down the manager
func (m *Manager) Shutdown(ctx context.Context) error {
	// Stop cache cleanup if it's a memory cache
	if memCache, ok := m.cache.(*MemoryCache); ok {
		memCache.Stop()
	}

	return nil
}

// ManagerStats represents comprehensive manager statistics
type ManagerStats struct {
	Registry RegistryStats `json:"registry"`
	Cache    CacheStats    `json:"cache"`
	Config   ManagerConfig `json:"config"`
}
