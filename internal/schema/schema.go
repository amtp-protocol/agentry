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
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// SchemaIdentifier represents an AGNTCY schema identifier
type SchemaIdentifier struct {
	Domain  string `json:"domain"`
	Entity  string `json:"entity"`
	Version string `json:"version"`
	Raw     string `json:"raw"`
}

// Schema represents a schema definition
type Schema struct {
	ID          SchemaIdentifier `json:"id"`
	Definition  json.RawMessage  `json:"definition"`
	PublishedAt time.Time        `json:"published_at"`
	Signature   string           `json:"signature,omitempty"`
}

// SchemaMetadata contains metadata about a schema
type SchemaMetadata struct {
	ID        SchemaIdentifier `json:"id"`
	Version   string           `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	FilePath  string           `json:"file_path"`
	Size      int64            `json:"size"`
	Checksum  string           `json:"checksum"`
}

// ValidationError represents a schema validation error
type ValidationError struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Code    string      `json:"code"`
	Value   interface{} `json:"value,omitempty"`
}

// RegistryStats represents registry statistics
type RegistryStats struct {
	TotalSchemas int            `json:"total_schemas"`
	Domains      map[string]int `json:"domains"`
	Entities     map[string]int `json:"entities"`
}

// RegistryClient defines the interface for schema registry operations
type RegistryClient interface {
	// GetSchema retrieves a schema by identifier
	GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error)

	// ListSchemas lists available schemas matching a pattern
	ListSchemas(ctx context.Context, pattern string) ([]SchemaIdentifier, error)

	// RegisterSchema registers a new schema
	RegisterSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error

	// RegisterOrUpdateSchema registers a new schema or updates an existing one
	RegisterOrUpdateSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error

	// DeleteSchema deletes a schema by identifier
	DeleteSchema(ctx context.Context, id SchemaIdentifier) error

	// GetStats returns registry statistics
	GetStats() RegistryStats

	// ValidateSchema validates a schema definition
	ValidateSchema(ctx context.Context, schema *Schema) error

	// CheckCompatibility checks if two schemas are compatible
	CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error)
}

// Validator defines the interface for schema-based payload validation
type Validator interface {
	// ValidatePayload validates a payload against a schema
	ValidatePayload(ctx context.Context, payload json.RawMessage, schemaID SchemaIdentifier) (*ValidationResult, error)

	// ValidateWithSchema validates a payload against a provided schema definition
	ValidateWithSchema(ctx context.Context, payload json.RawMessage, schema *Schema) (*ValidationResult, error)
}

// Cache defines the interface for schema caching
type Cache interface {
	// Get retrieves a schema from cache
	Get(ctx context.Context, id SchemaIdentifier) (*Schema, error)

	// Set stores a schema in cache with TTL
	Set(ctx context.Context, schema *Schema, ttl time.Duration) error

	// Delete removes a schema from cache
	Delete(ctx context.Context, id SchemaIdentifier) error

	// Clear clears all cached schemas
	Clear(ctx context.Context) error
}

// ParseSchemaIdentifier parses an AGNTCY schema identifier string
func ParseSchemaIdentifier(schemaStr string) (*SchemaIdentifier, error) {
	if schemaStr == "" {
		return nil, fmt.Errorf("schema identifier cannot be empty")
	}

	// AGNTCY schema format: agntcy:domain.entity.version
	schemaRegex := regexp.MustCompile(`^agntcy:([a-zA-Z0-9_-]+)\.([a-zA-Z0-9_-]+)\.(v[0-9]+)$`)
	matches := schemaRegex.FindStringSubmatch(schemaStr)

	if len(matches) != 4 {
		return nil, fmt.Errorf("invalid AGNTCY schema format: %s (expected: agntcy:domain.entity.version)", schemaStr)
	}

	return &SchemaIdentifier{
		Domain:  matches[1],
		Entity:  matches[2],
		Version: matches[3],
		Raw:     schemaStr,
	}, nil
}

// String returns the string representation of the schema identifier
func (si *SchemaIdentifier) String() string {
	if si.Raw != "" {
		return si.Raw
	}
	return fmt.Sprintf("agntcy:%s.%s.%s", si.Domain, si.Entity, si.Version)
}

// MatchesPattern checks if the schema identifier matches a pattern
func (si *SchemaIdentifier) MatchesPattern(pattern string) bool {
	if pattern == "" {
		return true
	}

	// Simple pattern matching - can be enhanced later
	fullID := si.String()
	return fullID == pattern ||
		si.Domain == pattern ||
		fmt.Sprintf("%s.%s", si.Domain, si.Entity) == pattern
}

// IsCompatibleWith checks if this schema is compatible with another
func (si *SchemaIdentifier) IsCompatibleWith(other *SchemaIdentifier) bool {
	// Basic compatibility: same domain and entity, version can differ
	return si.Domain == other.Domain && si.Entity == other.Entity
}

// ValidationResult represents the result of schema validation
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors,omitempty"`
	Warnings []ValidationError `json:"warnings,omitempty"`
}

// IsValid returns true if validation passed
func (vr *ValidationResult) IsValid() bool {
	return vr.Valid && len(vr.Errors) == 0
}

// AddError adds a validation error
func (vr *ValidationResult) AddError(field, message, code string, value interface{}) {
	vr.Errors = append(vr.Errors, ValidationError{
		Field:   field,
		Message: message,
		Code:    code,
		Value:   value,
	})
	vr.Valid = false
}

// AddWarning adds a validation warning
func (vr *ValidationResult) AddWarning(field, message, code string, value interface{}) {
	vr.Warnings = append(vr.Warnings, ValidationError{
		Field:   field,
		Message: message,
		Code:    code,
		Value:   value,
	})
}
