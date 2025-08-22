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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalRegistryConfig holds configuration for local file-based registry
type LocalRegistryConfig struct {
	BasePath   string `yaml:"base_path" json:"base_path"`
	IndexFile  string `yaml:"index_file" json:"index_file"`
	AutoSave   bool   `yaml:"auto_save" json:"auto_save"`
	CreateDirs bool   `yaml:"create_dirs" json:"create_dirs"`
}

// LocalRegistry implements RegistryClient using local file system
type LocalRegistry struct {
	basePath    string
	indexFile   string
	autoSave    bool
	schemas     map[string]*Schema
	metadata    map[string]*SchemaMetadata
	mu          sync.RWMutex
	initialized bool
}

// RegistryIndex represents the index file structure
type RegistryIndex struct {
	Version   string                     `json:"version"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Schemas   map[string]*SchemaMetadata `json:"schemas"`
}

// NewLocalRegistry creates a new local file-based registry
func NewLocalRegistry(config LocalRegistryConfig) (*LocalRegistry, error) {
	if config.BasePath == "" {
		config.BasePath = "./schemas"
	}
	if config.IndexFile == "" {
		config.IndexFile = "index.json"
	}

	registry := &LocalRegistry{
		basePath:  config.BasePath,
		autoSave:  config.AutoSave,
		indexFile: config.IndexFile,
		schemas:   make(map[string]*Schema),
		metadata:  make(map[string]*SchemaMetadata),
	}

	// Create directories if needed
	if config.CreateDirs {
		if err := os.MkdirAll(config.BasePath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create registry directory: %w", err)
		}
	}

	// Load existing schemas
	if err := registry.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	registry.initialized = true
	return registry, nil
}

// RegisterSchema registers a new schema in the local registry
func (lr *LocalRegistry) RegisterSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	// Check if schema already exists
	if _, exists := lr.schemas[schema.ID.String()]; exists {
		return fmt.Errorf("schema already exists: %s (use UpdateSchema to modify existing schemas)", schema.ID.String())
	}

	// Use internal method to register
	return lr.registerSchemaInternal(schema, metadata)
}

// RegisterOrUpdateSchema registers a new schema or updates an existing one
func (lr *LocalRegistry) RegisterOrUpdateSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	// Check if schema exists
	if _, exists := lr.schemas[schema.ID.String()]; exists {
		// Update existing schema
		return lr.updateSchemaInternal(schema, metadata)
	}

	// Register new schema (without the existence check since we already checked)
	return lr.registerSchemaInternal(schema, metadata)
}

// registerSchemaInternal registers a schema without checking for existence (internal use)
func (lr *LocalRegistry) registerSchemaInternal(schema *Schema, metadata *SchemaMetadata) error {
	// Validate schema
	if err := lr.validateSchema(schema); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// Generate checksum
	checksum, err := lr.generateChecksum(schema.Definition)
	if err != nil {
		return fmt.Errorf("failed to generate checksum: %w", err)
	}

	// Set metadata defaults
	if metadata == nil {
		metadata = &SchemaMetadata{}
	}
	metadata.ID = schema.ID
	metadata.CreatedAt = time.Now().UTC()
	metadata.UpdatedAt = time.Now().UTC()
	metadata.Size = int64(len(schema.Definition))
	metadata.Checksum = checksum

	// Generate file path
	filePath := lr.generateFilePath(schema.ID)
	metadata.FilePath = filePath

	// Store schema
	lr.schemas[schema.ID.String()] = schema
	lr.metadata[schema.ID.String()] = metadata

	// Save to disk if auto-save is enabled
	if lr.autoSave {
		if err := lr.saveSchema(schema, metadata); err != nil {
			return fmt.Errorf("failed to save schema: %w", err)
		}
		if err := lr.updateIndex(); err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}
	}

	return nil
}

// updateSchemaInternal updates an existing schema (internal use)
func (lr *LocalRegistry) updateSchemaInternal(schema *Schema, metadata *SchemaMetadata) error {
	// Validate new schema
	if err := lr.validateSchema(schema); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// Update metadata
	if metadata == nil {
		metadata = &SchemaMetadata{}
	}
	metadata.ID = schema.ID
	metadata.UpdatedAt = time.Now().UTC()
	metadata.Size = int64(len(schema.Definition))

	// Generate file path
	filePath := lr.generateFilePath(schema.ID)
	metadata.FilePath = filePath

	// Generate new checksum
	checksum, err := lr.generateChecksum(schema.Definition)
	if err != nil {
		return fmt.Errorf("failed to generate checksum: %w", err)
	}
	metadata.Checksum = checksum

	// Update schema
	lr.schemas[schema.ID.String()] = schema
	lr.metadata[schema.ID.String()] = metadata

	// Save to disk if auto-save is enabled
	if lr.autoSave {
		if err := lr.saveSchema(schema, metadata); err != nil {
			return fmt.Errorf("failed to save schema: %w", err)
		}
		if err := lr.updateIndex(); err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}
	}

	return nil
}

// GetSchema retrieves a schema by identifier
func (lr *LocalRegistry) GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	schema, exists := lr.schemas[id.String()]
	if !exists {
		return nil, fmt.Errorf("schema not found: %s", id.String())
	}

	// Return a copy to prevent modification
	schemaCopy := *schema
	return &schemaCopy, nil
}

// ListSchemas lists available schemas matching a pattern
func (lr *LocalRegistry) ListSchemas(ctx context.Context, pattern string) ([]SchemaIdentifier, error) {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	var result []SchemaIdentifier
	for _, schema := range lr.schemas {
		if pattern == "" || schema.ID.MatchesPattern(pattern) {
			result = append(result, schema.ID)
		}
	}

	// Sort results for consistent output
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})

	return result, nil
}

// ValidateSchema validates a schema definition
func (lr *LocalRegistry) ValidateSchema(ctx context.Context, schema *Schema) error {
	return lr.validateSchema(schema)
}

// CheckCompatibility checks if two schemas are compatible
func (lr *LocalRegistry) CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error) {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	// Get both schemas
	currentSchema, exists := lr.schemas[current.String()]
	if !exists {
		return false, fmt.Errorf("current schema not found: %s", current.String())
	}

	newSchema, exists := lr.schemas[new.String()]
	if !exists {
		return false, fmt.Errorf("new schema not found: %s", new.String())
	}

	// Basic compatibility check
	compatible := lr.checkSchemaCompatibility(currentSchema, newSchema)
	return compatible, nil
}

// DeleteSchema removes a schema from the registry
func (lr *LocalRegistry) DeleteSchema(ctx context.Context, id SchemaIdentifier) error {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	// Check if schema exists
	_, exists := lr.schemas[id.String()]
	if !exists {
		return fmt.Errorf("schema not found: %s", id.String())
	}

	// Remove from memory
	delete(lr.schemas, id.String())
	delete(lr.metadata, id.String())

	// Remove from disk if auto-save is enabled
	if lr.autoSave {
		filePath := lr.generateFilePath(id)
		fullPath := filepath.Join(lr.basePath, filePath)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove schema file: %w", err)
		}

		// Try to remove empty directories
		dir := filepath.Dir(fullPath)
		for dir != lr.basePath && dir != "." {
			if err := os.Remove(dir); err != nil {
				break // Directory not empty or other error
			}
			dir = filepath.Dir(dir)
		}

		// Update index
		if err := lr.updateIndex(); err != nil {
			return fmt.Errorf("failed to update index: %w", err)
		}
	}

	return nil
}

// GetSchemaMetadata retrieves metadata for a schema
func (lr *LocalRegistry) GetSchemaMetadata(ctx context.Context, id SchemaIdentifier) (*SchemaMetadata, error) {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	schema, exists := lr.schemas[id.String()]
	if !exists {
		return nil, fmt.Errorf("schema not found: %s", id.String())
	}

	// Generate metadata from schema
	checksum, _ := lr.generateChecksum(schema.Definition)
	metadata := &SchemaMetadata{
		ID:        schema.ID,
		Version:   schema.ID.Version,
		CreatedAt: schema.PublishedAt,
		UpdatedAt: schema.PublishedAt,
		FilePath:  lr.generateFilePath(schema.ID),
		Size:      int64(len(schema.Definition)),
		Checksum:  checksum,
	}

	return metadata, nil
}

// getSchemaMetadataInternal generates metadata for a schema without acquiring locks (internal use)
func (lr *LocalRegistry) getSchemaMetadataInternal(schema *Schema) *SchemaMetadata {
	checksum, _ := lr.generateChecksum(schema.Definition)
	metadata := &SchemaMetadata{
		ID:        schema.ID,
		Version:   schema.ID.Version,
		CreatedAt: schema.PublishedAt,
		UpdatedAt: schema.PublishedAt,
		FilePath:  lr.generateFilePath(schema.ID),
		Size:      int64(len(schema.Definition)),
		Checksum:  checksum,
	}
	return metadata
}

// SaveToDisk saves all schemas to disk
func (lr *LocalRegistry) SaveToDisk() error {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	for _, schema := range lr.schemas {
		metadata := lr.metadata[schema.ID.String()]
		if err := lr.saveSchema(schema, metadata); err != nil {
			return fmt.Errorf("failed to save schema %s: %w", schema.ID.String(), err)
		}
	}

	return lr.updateIndex()
}

// GetStats returns registry statistics
func (lr *LocalRegistry) GetStats() RegistryStats {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	domains := make(map[string]int)
	entities := make(map[string]int)

	for _, schema := range lr.schemas {
		domains[schema.ID.Domain]++
		entities[schema.ID.Entity]++
	}

	return RegistryStats{
		TotalSchemas: len(lr.schemas),
		Domains:      domains,
		Entities:     entities,
	}
}

// validateSchema validates a schema definition
func (lr *LocalRegistry) validateSchema(schema *Schema) error {
	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	if schema.ID.Domain == "" || schema.ID.Entity == "" || schema.ID.Version == "" {
		return fmt.Errorf("schema ID must have domain, entity, and version")
	}

	if len(schema.Definition) == 0 {
		return fmt.Errorf("schema definition cannot be empty")
	}

	// Validate that definition is valid JSON
	var temp interface{}
	if err := json.Unmarshal(schema.Definition, &temp); err != nil {
		return fmt.Errorf("schema definition is not valid JSON: %w", err)
	}

	return nil
}

// generateChecksum generates a SHA256 checksum for schema content
func (lr *LocalRegistry) generateChecksum(content json.RawMessage) (string, error) {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:]), nil
}

// generateFilePath generates a file path for a schema
func (lr *LocalRegistry) generateFilePath(id SchemaIdentifier) string {
	return fmt.Sprintf("%s/%s/%s.json", id.Domain, id.Entity, id.Version)
}

// saveSchema saves a schema to disk
func (lr *LocalRegistry) saveSchema(schema *Schema, metadata *SchemaMetadata) error {
	filePath := filepath.Join(lr.basePath, metadata.FilePath)

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create schema file with metadata
	schemaFile := struct {
		Metadata   *SchemaMetadata `json:"metadata"`
		Definition json.RawMessage `json:"definition"`
	}{
		Metadata:   metadata,
		Definition: schema.Definition,
	}

	data, err := json.MarshalIndent(schemaFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	return nil
}

// loadFromDisk loads schemas from disk
func (lr *LocalRegistry) loadFromDisk() error {
	// Try to load index first
	indexPath := filepath.Join(lr.basePath, lr.indexFile)
	if _, err := os.Stat(indexPath); err == nil {
		if err := lr.loadFromIndex(); err != nil {
			// If index loading fails, try to scan directory
			return lr.scanDirectory()
		}
		return nil
	}

	// No index file, scan directory
	return lr.scanDirectory()
}

// loadFromIndex loads schemas using the index file
func (lr *LocalRegistry) loadFromIndex() error {
	indexPath := filepath.Join(lr.basePath, lr.indexFile)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	var index RegistryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("failed to parse index file: %w", err)
	}

	// Load each schema
	for schemaID, metadata := range index.Schemas {
		if err := lr.loadSchema(schemaID, metadata); err != nil {
			// Log error but continue loading other schemas
			fmt.Printf("Warning: failed to load schema %s: %v\n", schemaID, err)
		}
	}

	return nil
}

// scanDirectory scans the directory for schema files
func (lr *LocalRegistry) scanDirectory() error {
	return filepath.Walk(lr.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, lr.indexFile) {
			relPath, err := filepath.Rel(lr.basePath, path)
			if err != nil {
				return err
			}

			// Try to parse schema ID from path
			if schemaID := lr.parseSchemaIDFromPath(relPath); schemaID != nil {
				metadata := &SchemaMetadata{
					ID:       *schemaID,
					FilePath: relPath,
				}
				if err := lr.loadSchema(schemaID.String(), metadata); err != nil {
					fmt.Printf("Warning: failed to load schema from %s: %v\n", path, err)
				}
			}
		}

		return nil
	})
}

// parseSchemaIDFromPath attempts to parse a schema ID from a file path
func (lr *LocalRegistry) parseSchemaIDFromPath(path string) *SchemaIdentifier {
	// Expected format: domain/entity/version.json
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) != 3 {
		return nil
	}

	domain := parts[0]
	entity := parts[1]
	version := strings.TrimSuffix(parts[2], ".json")

	return &SchemaIdentifier{
		Domain:  domain,
		Entity:  entity,
		Version: version,
		Raw:     fmt.Sprintf("agntcy:%s.%s.%s", domain, entity, version),
	}
}

// loadSchema loads a single schema from disk
func (lr *LocalRegistry) loadSchema(schemaID string, metadata *SchemaMetadata) error {
	filePath := filepath.Join(lr.basePath, metadata.FilePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	var schemaFile struct {
		Metadata   *SchemaMetadata `json:"metadata"`
		Definition json.RawMessage `json:"definition"`
	}

	if err := json.Unmarshal(data, &schemaFile); err != nil {
		return fmt.Errorf("failed to parse schema file: %w", err)
	}

	schema := &Schema{
		ID:          schemaFile.Metadata.ID,
		Definition:  schemaFile.Definition,
		PublishedAt: schemaFile.Metadata.CreatedAt,
	}

	lr.schemas[schemaID] = schema
	lr.metadata[schemaID] = schemaFile.Metadata

	return nil
}

// updateIndex updates the index file
func (lr *LocalRegistry) updateIndex() error {
	index := RegistryIndex{
		Version:   "1.0",
		UpdatedAt: time.Now().UTC(),
		Schemas:   make(map[string]*SchemaMetadata),
	}

	// Build index from current schemas
	for schemaID, schema := range lr.schemas {
		metadata := lr.getSchemaMetadataInternal(schema)
		index.Schemas[schemaID] = metadata
	}

	// Save index
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	indexPath := filepath.Join(lr.basePath, lr.indexFile)
	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

// checkSchemaCompatibility performs basic schema compatibility checking
func (lr *LocalRegistry) checkSchemaCompatibility(current, new *Schema) bool {
	// Basic compatibility: same domain and entity
	if current.ID.Domain != new.ID.Domain || current.ID.Entity != new.ID.Entity {
		return false
	}

	// For now, assume all versions within the same domain/entity are compatible
	// In a real implementation, you would perform semantic compatibility checking
	return true
}

// RegistryStats represents registry statistics
type RegistryStats struct {
	TotalSchemas int            `json:"total_schemas"`
	Domains      map[string]int `json:"domains"`
	Entities     map[string]int `json:"entities"`
}
