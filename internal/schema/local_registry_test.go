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
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewLocalRegistry(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		config        LocalRegistryConfig
		expectedPath  string
		expectedIndex string
		expectError   bool
	}{
		{
			name: "default values",
			config: LocalRegistryConfig{
				CreateDirs: true,
			},
			expectedPath:  "./schemas",
			expectedIndex: "index.json",
			expectError:   false,
		},
		{
			name: "custom values",
			config: LocalRegistryConfig{
				BasePath:   tempDir,
				IndexFile:  "custom_index.json",
				AutoSave:   true,
				CreateDirs: true,
			},
			expectedPath:  tempDir,
			expectedIndex: "custom_index.json",
			expectError:   false,
		},
		{
			name: "directory creation disabled, non-existent path",
			config: LocalRegistryConfig{
				BasePath:   filepath.Join(tempDir, "nonexistent"),
				CreateDirs: false,
			},
			expectError: true, // Will fail because directory doesn't exist and we can't create it
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := NewLocalRegistry(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if registry == nil {
				t.Errorf("expected registry to be created")
				return
			}

			if registry.basePath != tt.expectedPath {
				t.Errorf("expected base path %s, got %s", tt.expectedPath, registry.basePath)
			}

			if registry.indexFile != tt.expectedIndex {
				t.Errorf("expected index file %s, got %s", tt.expectedIndex, registry.indexFile)
			}

			if !registry.initialized {
				t.Errorf("expected registry to be initialized")
			}
		})
	}
}

func TestLocalRegistry_RegisterSchema(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
		PublishedAt: time.Now(),
	}

	metadata := &SchemaMetadata{
		ID:      schemaID,
		Version: "v1",
	}

	ctx := context.Background()

	// Test successful registration
	err = registry.RegisterSchema(ctx, schema, metadata)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Verify schema was registered
	retrieved, err := registry.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting registered schema: %v", err)
	}

	if retrieved.ID.String() != schemaID.String() {
		t.Errorf("expected schema ID %s, got %s", schemaID.String(), retrieved.ID.String())
	}

	// Test duplicate registration
	err = registry.RegisterSchema(ctx, schema, metadata)
	if err == nil {
		t.Errorf("expected error when registering duplicate schema")
	}
}

func TestLocalRegistry_RegisterOrUpdateSchema(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	metadata := &SchemaMetadata{
		ID:      schemaID,
		Version: "v1",
	}

	ctx := context.Background()

	// Test registering new schema
	err = registry.RegisterOrUpdateSchema(ctx, schema, metadata)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Test updating existing schema
	updatedSchema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
		PublishedAt: time.Now(),
	}

	err = registry.RegisterOrUpdateSchema(ctx, updatedSchema, metadata)
	if err != nil {
		t.Errorf("unexpected error updating schema: %v", err)
	}

	// Verify schema was updated
	retrieved, err := registry.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting updated schema: %v", err)
	}

	if string(retrieved.Definition) != string(updatedSchema.Definition) {
		t.Errorf("expected updated definition, got %s", string(retrieved.Definition))
	}
}

func TestLocalRegistry_GetSchema(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()

	// Test getting non-existent schema
	_, err = registry.GetSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error for non-existent schema")
	}

	// Register schema and test retrieval
	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	err = registry.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	retrieved, err := registry.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema: %v", err)
	}

	if retrieved.ID.String() != schemaID.String() {
		t.Errorf("expected schema ID %s, got %s", schemaID.String(), retrieved.ID.String())
	}

	// Verify it returns a copy (modification shouldn't affect original)
	retrieved.Definition = json.RawMessage(`{"modified": true}`)

	retrieved2, err := registry.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema again: %v", err)
	}

	if string(retrieved2.Definition) == string(retrieved.Definition) {
		t.Errorf("expected original definition to be unchanged")
	}
}

func TestLocalRegistry_ListSchemas(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Register multiple schemas
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "product",
				Version: "v1",
				Raw:     "agntcy:commerce.product.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
	}

	ctx := context.Background()
	for _, schema := range schemas {
		err = registry.RegisterSchema(ctx, schema, nil)
		if err != nil {
			t.Errorf("unexpected error registering schema: %v", err)
		}
	}

	// Test listing all schemas
	result, err := registry.ListSchemas(ctx, "")
	if err != nil {
		t.Errorf("unexpected error listing schemas: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 schemas, got %d", len(result))
	}

	// Test pattern matching
	result, err = registry.ListSchemas(ctx, "commerce")
	if err != nil {
		t.Errorf("unexpected error listing commerce schemas: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 commerce schemas, got %d", len(result))
	}

	// Test domain.entity pattern
	result, err = registry.ListSchemas(ctx, "commerce.order")
	if err != nil {
		t.Errorf("unexpected error listing commerce.order schemas: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 commerce.order schema, got %d", len(result))
	}

	// Verify results are sorted
	allResults, err := registry.ListSchemas(ctx, "")
	if err != nil {
		t.Errorf("unexpected error listing all schemas: %v", err)
	}

	for i := 1; i < len(allResults); i++ {
		if allResults[i-1].String() >= allResults[i].String() {
			t.Errorf("results are not sorted: %s >= %s", allResults[i-1].String(), allResults[i].String())
		}
	}
}

func TestLocalRegistry_DeleteSchema(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()

	// Test deleting non-existent schema
	err = registry.DeleteSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error when deleting non-existent schema")
	}

	// Register schema and test deletion
	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	err = registry.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Verify schema exists
	_, err = registry.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema before deletion: %v", err)
	}

	// Delete schema
	err = registry.DeleteSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error deleting schema: %v", err)
	}

	// Verify schema is gone
	_, err = registry.GetSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error after deleting schema")
	}

	// Verify file is removed (when auto-save is enabled)
	expectedPath := filepath.Join(tempDir, "commerce", "order", "v1.json")
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Errorf("expected schema file to be deleted")
	}
}

func TestLocalRegistry_ValidateSchema(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name        string
		schema      *Schema
		expectError bool
	}{
		{
			name: "valid schema",
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain:  "commerce",
					Entity:  "order",
					Version: "v1",
					Raw:     "agntcy:commerce.order.v1",
				},
				Definition: json.RawMessage(`{"type": "object"}`),
			},
			expectError: false,
		},
		{
			name:        "nil schema",
			schema:      nil,
			expectError: true,
		},
		{
			name: "empty domain",
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain:  "",
					Entity:  "order",
					Version: "v1",
				},
				Definition: json.RawMessage(`{"type": "object"}`),
			},
			expectError: true,
		},
		{
			name: "empty entity",
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain:  "commerce",
					Entity:  "",
					Version: "v1",
				},
				Definition: json.RawMessage(`{"type": "object"}`),
			},
			expectError: true,
		},
		{
			name: "empty version",
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain:  "commerce",
					Entity:  "order",
					Version: "",
				},
				Definition: json.RawMessage(`{"type": "object"}`),
			},
			expectError: true,
		},
		{
			name: "empty definition",
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain:  "commerce",
					Entity:  "order",
					Version: "v1",
				},
				Definition: json.RawMessage(``),
			},
			expectError: true,
		},
		{
			name: "invalid JSON definition",
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain:  "commerce",
					Entity:  "order",
					Version: "v1",
				},
				Definition: json.RawMessage(`{invalid json}`),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.ValidateSchema(ctx, tt.schema)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLocalRegistry_CheckCompatibility(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Register schemas
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
				Raw:     "agntcy:commerce.order.v2",
			},
			Definition: json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
	}

	ctx := context.Background()
	for _, schema := range schemas {
		err = registry.RegisterSchema(ctx, schema, nil)
		if err != nil {
			t.Errorf("unexpected error registering schema: %v", err)
		}
	}

	tests := []struct {
		name           string
		currentSchema  SchemaIdentifier
		newSchema      SchemaIdentifier
		expectedResult bool
		expectError    bool
	}{
		{
			name: "compatible schemas (same domain and entity)",
			currentSchema: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			newSchema: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
				Raw:     "agntcy:commerce.order.v2",
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "incompatible schemas (different domain)",
			currentSchema: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			newSchema: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "current schema not found",
			currentSchema: SchemaIdentifier{
				Domain:  "nonexistent",
				Entity:  "schema",
				Version: "v1",
				Raw:     "agntcy:nonexistent.schema.v1",
			},
			newSchema: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			expectError: true,
		},
		{
			name: "new schema not found",
			currentSchema: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			newSchema: SchemaIdentifier{
				Domain:  "nonexistent",
				Entity:  "schema",
				Version: "v1",
				Raw:     "agntcy:nonexistent.schema.v1",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.CheckCompatibility(ctx, tt.currentSchema, tt.newSchema)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expectedResult {
				t.Errorf("expected result %t, got %t", tt.expectedResult, result)
			}
		})
	}
}

func TestLocalRegistry_GetStats(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   true,
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Initially empty
	stats := registry.GetStats()
	if stats.TotalSchemas != 0 {
		t.Errorf("expected 0 total schemas, got %d", stats.TotalSchemas)
	}

	// Register schemas
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "product",
				Version: "v1",
				Raw:     "agntcy:commerce.product.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			Definition: json.RawMessage(`{"type": "object"}`),
		},
	}

	ctx := context.Background()
	for _, schema := range schemas {
		err = registry.RegisterSchema(ctx, schema, nil)
		if err != nil {
			t.Errorf("unexpected error registering schema: %v", err)
		}
	}

	stats = registry.GetStats()
	if stats.TotalSchemas != 3 {
		t.Errorf("expected 3 total schemas, got %d", stats.TotalSchemas)
	}

	if stats.Domains["commerce"] != 2 {
		t.Errorf("expected 2 commerce schemas, got %d", stats.Domains["commerce"])
	}

	if stats.Domains["messaging"] != 1 {
		t.Errorf("expected 1 messaging schema, got %d", stats.Domains["messaging"])
	}

	if stats.Entities["order"] != 1 {
		t.Errorf("expected 1 order entity, got %d", stats.Entities["order"])
	}

	if stats.Entities["product"] != 1 {
		t.Errorf("expected 1 product entity, got %d", stats.Entities["product"])
	}

	if stats.Entities["notification"] != 1 {
		t.Errorf("expected 1 notification entity, got %d", stats.Entities["notification"])
	}
}

func TestLocalRegistry_SaveToDisk(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   false, // Disable auto-save to test manual save
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Register schema without auto-save
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	ctx := context.Background()
	err = registry.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Verify file doesn't exist yet (auto-save disabled)
	expectedPath := filepath.Join(tempDir, "commerce", "order", "v1.json")
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Errorf("expected schema file to not exist before manual save")
	}

	// Save to disk
	err = registry.SaveToDisk()
	if err != nil {
		t.Errorf("unexpected error saving to disk: %v", err)
	}

	// Verify file exists now
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected schema file to exist after manual save")
	}

	// Verify index file exists
	indexPath := filepath.Join(tempDir, "index.json")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("expected index file to exist after manual save")
	}
}

func TestLocalRegistry_generateFilePath(t *testing.T) {
	config := LocalRegistryConfig{}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
	}

	expectedPath := "commerce/order/v1.json"
	actualPath := registry.generateFilePath(schemaID)

	if actualPath != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, actualPath)
	}
}

func TestLocalRegistry_generateChecksum(t *testing.T) {
	config := LocalRegistryConfig{}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	content := json.RawMessage(`{"type": "object"}`)

	checksum1, err := registry.generateChecksum(content)
	if err != nil {
		t.Errorf("unexpected error generating checksum: %v", err)
	}

	if checksum1 == "" {
		t.Errorf("expected non-empty checksum")
	}

	// Same content should produce same checksum
	checksum2, err := registry.generateChecksum(content)
	if err != nil {
		t.Errorf("unexpected error generating checksum: %v", err)
	}

	if checksum1 != checksum2 {
		t.Errorf("expected same checksum for same content")
	}

	// Different content should produce different checksum
	differentContent := json.RawMessage(`{"type": "string"}`)
	checksum3, err := registry.generateChecksum(differentContent)
	if err != nil {
		t.Errorf("unexpected error generating checksum: %v", err)
	}

	if checksum1 == checksum3 {
		t.Errorf("expected different checksum for different content")
	}
}

func TestLocalRegistry_parseSchemaIDFromPath(t *testing.T) {
	config := LocalRegistryConfig{}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected *SchemaIdentifier
	}{
		{
			name: "valid path",
			path: "commerce/order/v1.json",
			expected: &SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
		},
		{
			name:     "invalid path - too few parts",
			path:     "commerce/order.json",
			expected: nil,
		},
		{
			name:     "invalid path - too many parts",
			path:     "commerce/order/v1/extra.json",
			expected: nil,
		},
		{
			name:     "empty path",
			path:     "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.parseSchemaIDFromPath(tt.path)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil result, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected result, got nil")
				return
			}

			if result.Domain != tt.expected.Domain {
				t.Errorf("expected domain %s, got %s", tt.expected.Domain, result.Domain)
			}

			if result.Entity != tt.expected.Entity {
				t.Errorf("expected entity %s, got %s", tt.expected.Entity, result.Entity)
			}

			if result.Version != tt.expected.Version {
				t.Errorf("expected version %s, got %s", tt.expected.Version, result.Version)
			}

			if result.Raw != tt.expected.Raw {
				t.Errorf("expected raw %s, got %s", tt.expected.Raw, result.Raw)
			}
		})
	}
}

func TestLocalRegistry_AutoSaveDisabled(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "local_registry_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := LocalRegistryConfig{
		BasePath:   tempDir,
		AutoSave:   false, // Disable auto-save
		CreateDirs: true,
	}
	registry, err := NewLocalRegistry(config)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	ctx := context.Background()
	err = registry.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Verify file doesn't exist (auto-save disabled)
	expectedPath := filepath.Join(tempDir, "commerce", "order", "v1.json")
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Errorf("expected schema file to not exist when auto-save is disabled")
	}

	// Schema should still be available in memory
	retrieved, err := registry.GetSchema(ctx, schema.ID)
	if err != nil {
		t.Errorf("unexpected error getting schema from memory: %v", err)
	}

	if retrieved.ID.String() != schema.ID.String() {
		t.Errorf("expected schema to be available in memory")
	}
}
