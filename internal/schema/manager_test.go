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
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestNewManager(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		config      ManagerConfig
		expectError bool
	}{
		{
			name: "with local registry",
			config: ManagerConfig{
				UseLocalRegistry: true,
				LocalRegistry: LocalRegistryConfig{
					BasePath:   tempDir,
					CreateDirs: true,
				},
				Cache: CacheConfig{
					Type: "memory",
				},
				Validation: ValidatorConfig{
					Enabled: true,
				},
				Negotiation: NegotiationConfig{
					Enabled: true,
				},
				Compatibility: CompatibilityConfig{
					Enabled: true,
				},
				Pipeline: PipelineConfig{
					Enabled: true,
				},
				ErrorReporting: ErrorReportConfig{
					Enabled: true,
				},
			},
			expectError: false,
		},
		{
			name: "with HTTP registry",
			config: ManagerConfig{
				UseLocalRegistry: false,
				Registry: RegistryConfig{
					BaseURL: "https://registry.example.com",
				},
				Cache: CacheConfig{
					Type: "memory",
				},
				Validation: ValidatorConfig{
					Enabled: true,
				},
			},
			expectError: false,
		},
		{
			name: "with mock registry (no config)",
			config: ManagerConfig{
				UseLocalRegistry: false,
				Cache: CacheConfig{
					Type: "memory",
				},
			},
			expectError: false,
		},
		{
			name: "invalid cache type",
			config: ManagerConfig{
				UseLocalRegistry: false,
				Cache: CacheConfig{
					Type: "invalid",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(tt.config)

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

			if manager == nil {
				t.Errorf("expected manager to be created")
				return
			}

			// Verify components are initialized
			if manager.registryClient == nil {
				t.Errorf("expected registry client to be initialized")
			}

			if manager.validator == nil {
				t.Errorf("expected validator to be initialized")
			}

			if manager.cache == nil {
				t.Errorf("expected cache to be initialized")
			}

			if manager.negotiationEngine == nil {
				t.Errorf("expected negotiation engine to be initialized")
			}

			if manager.compatibilityChecker == nil {
				t.Errorf("expected compatibility checker to be initialized")
			}

			if manager.pipeline == nil {
				t.Errorf("expected pipeline to be initialized")
			}

			if manager.errorReporter == nil {
				t.Errorf("expected error reporter to be initialized")
			}

			// Clean up
			manager.Shutdown(context.Background())
		})
	}
}

func TestManager_GetRegistry(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	registry := manager.GetRegistry()
	if registry == nil {
		t.Errorf("expected registry to be returned")
	}

	if registry != manager.registryClient {
		t.Errorf("expected same registry instance")
	}
}

func TestManager_ValidateMessage_NoSchema(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "user@example.com",
		Schema:    "", // No schema specified
		Payload:   json.RawMessage(`{"order_id": "12345"}`),
	}

	ctx := context.Background()
	report, err := manager.ValidateMessage(ctx, message)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if report == nil {
		t.Errorf("expected validation report")
		return
	}

	if report.Valid {
		t.Errorf("expected validation to fail when no schema specified")
	}

	if len(report.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(report.Errors))
	}

	if report.Errors[0].Code != "SCHEMA_REQUIRED" {
		t.Errorf("expected error code 'SCHEMA_REQUIRED', got '%s'", report.Errors[0].Code)
	}
}

func TestManager_ValidateMessage_InvalidSchemaID(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "user@example.com",
		Schema:    "invalid-schema-format", // Invalid schema ID
		Payload:   json.RawMessage(`{"order_id": "12345"}`),
	}

	ctx := context.Background()
	report, err := manager.ValidateMessage(ctx, message)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if report == nil {
		t.Errorf("expected validation report")
		return
	}

	if report.Valid {
		t.Errorf("expected validation to fail for invalid schema ID")
	}

	if len(report.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(report.Errors))
	}

	if report.Errors[0].Code != "INVALID_SCHEMA_ID" {
		t.Errorf("expected error code 'INVALID_SCHEMA_ID', got '%s'", report.Errors[0].Code)
	}
}

func TestManager_ValidateMessage_Success(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
		Validation: ValidatorConfig{
			Enabled: true,
		},
		Negotiation: NegotiationConfig{
			Enabled: false, // Disable negotiation for simpler test
		},
		Pipeline: PipelineConfig{
			Enabled: true,
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	// Register a schema
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID: schemaID,
		Definition: json.RawMessage(`{
			"type": "object",
			"properties": {
				"order_id": {"type": "string"}
			},
			"required": ["order_id"]
		}`),
		PublishedAt: time.Now(),
	}

	ctx := context.Background()
	err = manager.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "user@example.com",
		Schema:    schemaID.String(),
		Payload:   json.RawMessage(`{"order_id": "12345"}`),
	}

	report, err := manager.ValidateMessage(ctx, message)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if report == nil {
		t.Errorf("expected validation report")
		return
	}

	if !report.Valid {
		t.Errorf("expected validation to pass")
	}

	if len(report.Errors) != 0 {
		t.Errorf("expected no errors, got %d", len(report.Errors))
	}

	if report.ProcessingTime == 0 {
		t.Errorf("expected processing time to be set")
	}
}

func TestManager_ValidateMessage_WithNegotiation(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
		Negotiation: NegotiationConfig{
			Enabled:          true,
			FallbackStrategy: "latest",
		},
		Pipeline: PipelineConfig{
			Enabled: true,
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	// Register v1 and v2 schemas
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: json.RawMessage(`{
				"type": "object",
				"properties": {
					"order_id": {"type": "string"}
				}
			}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
				Raw:     "agntcy:commerce.order.v2",
			},
			Definition: json.RawMessage(`{
				"type": "object",
				"properties": {
					"order_id": {"type": "string"},
					"amount": {"type": "number"}
				}
			}`),
		},
	}

	ctx := context.Background()
	for _, schema := range schemas {
		err = manager.RegisterSchema(ctx, schema, nil)
		if err != nil {
			t.Errorf("unexpected error registering schema: %v", err)
		}
	}

	// Request v3 (doesn't exist), should negotiate to v2 (latest)
	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "user@example.com",
		Schema:    "agntcy:commerce.order.v3", // Doesn't exist
		Payload:   json.RawMessage(`{"order_id": "12345", "amount": 100.50}`),
	}

	report, err := manager.ValidateMessage(ctx, message)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if report == nil {
		t.Errorf("expected validation report")
		return
	}

	if !report.Valid {
		t.Errorf("expected validation to pass with negotiation")
	}

	if report.NegotiatedSchema != "agntcy:commerce.order.v2" {
		t.Errorf("expected negotiated schema 'agntcy:commerce.order.v2', got '%s'", report.NegotiatedSchema)
	}
}

func TestManager_GetSchema(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:         schemaID,
		Definition: json.RawMessage(`{"type": "object"}`),
	}

	ctx := context.Background()
	err = manager.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	retrieved, err := manager.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema: %v", err)
	}

	if retrieved.ID.String() != schemaID.String() {
		t.Errorf("expected schema ID %s, got %s", schemaID.String(), retrieved.ID.String())
	}
}

func TestManager_ListSchemas(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

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
	}

	ctx := context.Background()
	for _, schema := range schemas {
		err = manager.RegisterSchema(ctx, schema, nil)
		if err != nil {
			t.Errorf("unexpected error registering schema: %v", err)
		}
	}

	result, err := manager.ListSchemas(ctx, "commerce")
	if err != nil {
		t.Errorf("unexpected error listing schemas: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(result))
	}
}

func TestManager_UpdateSchema(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	originalSchema := &Schema{
		ID:         schemaID,
		Definition: json.RawMessage(`{"type": "object"}`),
	}

	ctx := context.Background()
	err = manager.RegisterSchema(ctx, originalSchema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	updatedSchema := &Schema{
		ID:         schemaID,
		Definition: json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
	}

	err = manager.UpdateSchema(ctx, updatedSchema, nil)
	if err != nil {
		t.Errorf("unexpected error updating schema: %v", err)
	}

	retrieved, err := manager.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting updated schema: %v", err)
	}

	if string(retrieved.Definition) != string(updatedSchema.Definition) {
		t.Errorf("expected updated definition")
	}
}

func TestManager_DeleteSchema(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:         schemaID,
		Definition: json.RawMessage(`{"type": "object"}`),
	}

	ctx := context.Background()
	err = manager.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Verify schema exists
	_, err = manager.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema before deletion: %v", err)
	}

	err = manager.DeleteSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error deleting schema: %v", err)
	}

	// Verify schema is gone
	_, err = manager.GetSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error after deleting schema")
	}
}

func TestManager_CheckCompatibility(t *testing.T) {
	// Create temporary directory for local registry
	tempDir, err := os.MkdirTemp("", "manager_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := ManagerConfig{
		UseLocalRegistry: true,
		LocalRegistry: LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
		Cache: CacheConfig{
			Type: "memory",
		},
		Compatibility: CompatibilityConfig{
			Enabled: true,
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

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
			Definition: json.RawMessage(`{"type": "object"}`),
		},
	}

	ctx := context.Background()
	for _, schema := range schemas {
		err = manager.RegisterSchema(ctx, schema, nil)
		if err != nil {
			t.Errorf("unexpected error registering schema: %v", err)
		}
	}

	compatible, err := manager.CheckCompatibility(ctx, schemas[0].ID, schemas[1].ID)
	if err != nil {
		t.Errorf("unexpected error checking compatibility: %v", err)
	}

	if !compatible {
		t.Errorf("expected schemas to be compatible")
	}
}

func TestManager_ValidatePayload(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	payload := []byte(`{"order_id": "12345"}`)
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	_, err = manager.ValidatePayload(ctx, payload, schemaID)

	// With mock registry, this should fail because schema doesn't exist
	if err == nil {
		t.Errorf("expected error when schema doesn't exist")
	}
}

func TestManager_ClearCache(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	ctx := context.Background()
	err = manager.ClearCache(ctx)

	if err != nil {
		t.Errorf("unexpected error clearing cache: %v", err)
	}
}

func TestManager_GetStats(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	ctx := context.Background()
	stats, err := manager.GetStats(ctx)

	if err != nil {
		t.Errorf("unexpected error getting stats: %v", err)
	}

	if stats == nil {
		t.Errorf("expected stats to be returned")
		return
	}

	// Verify stats structure
	if stats.Registry.TotalSchemas < 0 {
		t.Errorf("expected non-negative total schemas")
	}

	if stats.Cache.Size < 0 {
		t.Errorf("expected non-negative cache size")
	}
}

func TestManager_Shutdown(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()
	err = manager.Shutdown(ctx)

	if err != nil {
		t.Errorf("unexpected error during shutdown: %v", err)
	}
}

func TestManager_ValidationWithErrorReporting(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
		ErrorReporting: ErrorReportConfig{
			Enabled: true,
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "user@example.com",
		Schema:    "", // No schema - should trigger error
		Payload:   json.RawMessage(`{"order_id": "12345"}`),
	}

	ctx := context.Background()
	report, err := manager.ValidateMessage(ctx, message)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if report == nil {
		t.Errorf("expected validation report")
		return
	}

	if report.Valid {
		t.Errorf("expected validation to fail")
	}

	// Error reporting should have been triggered (though we can't easily test the output)
	if len(report.Errors) == 0 {
		t.Errorf("expected validation errors to be present")
	}
}

func TestManagerConfig_DefaultValues(t *testing.T) {
	config := ManagerConfig{}

	// Test that manager can be created with empty config
	manager, err := NewManager(config)
	if err != nil {
		t.Errorf("unexpected error with default config: %v", err)
		return
	}

	if manager == nil {
		t.Errorf("expected manager to be created with default config")
		return
	}

	manager.Shutdown(context.Background())
}

func TestManager_EdgeCases(t *testing.T) {
	config := ManagerConfig{
		UseLocalRegistry: false,
		Cache: CacheConfig{
			Type: "memory",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Shutdown(context.Background())

	ctx := context.Background()

	t.Run("validate nil message", func(t *testing.T) {
		// Should handle nil message gracefully and return validation error
		report, err := manager.ValidateMessage(ctx, nil)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if report == nil {
			t.Errorf("expected report to be returned")
			return
		}
		if report.Valid {
			t.Errorf("expected validation to fail for nil message")
		}
		if len(report.Errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(report.Errors))
		}
		if len(report.Errors) > 0 && report.Errors[0].Code != "NIL_MESSAGE" {
			t.Errorf("expected error code 'NIL_MESSAGE', got '%s'", report.Errors[0].Code)
		}
	})

	t.Run("validate with nil context", func(t *testing.T) {
		message := &types.Message{
			MessageID: "test-message-id",
			Sender:    "user@example.com",
			Schema:    "agntcy:commerce.order.v1",
			Payload:   json.RawMessage(`{"order_id": "12345"}`),
		}

		// This should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ValidateMessage panicked with nil context: %v", r)
			}
		}()

		_, err := manager.ValidateMessage(context.TODO(), message)
		// Error is expected with nil context, but it shouldn't panic
		if err != nil {
			// This is expected
		}
	})
}
