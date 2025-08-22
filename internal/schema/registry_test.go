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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPRegistryClient(t *testing.T) {
	tests := []struct {
		name            string
		config          RegistryConfig
		expectedTimeout time.Duration
	}{
		{
			name: "default timeout",
			config: RegistryConfig{
				BaseURL: "https://registry.example.com",
			},
			expectedTimeout: 30 * time.Second,
		},
		{
			name: "custom timeout",
			config: RegistryConfig{
				BaseURL: "https://registry.example.com",
				Timeout: 60 * time.Second,
			},
			expectedTimeout: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewHTTPRegistryClient(tt.config)

			if client == nil {
				t.Errorf("expected HTTP registry client to be created")
				return
			}

			if client.baseURL != "https://registry.example.com" {
				t.Errorf("expected base URL 'https://registry.example.com', got '%s'", client.baseURL)
			}

			if client.httpClient.Timeout != tt.expectedTimeout {
				t.Errorf("expected timeout %v, got %v", tt.expectedTimeout, client.httpClient.Timeout)
			}
		})
	}
}

func TestHTTPRegistryClient_GetSchema(t *testing.T) {
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
		PublishedAt: time.Now(),
		Signature:   "test-signature",
	}

	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		expectedSchema *Schema
	}{
		{
			name: "successful response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(schema)
			},
			expectError:    false,
			expectedSchema: &schema,
		},
		{
			name: "schema not found",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectError: true,
		},
		{
			name: "server error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError: true,
		},
		{
			name: "invalid JSON response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			config := RegistryConfig{
				BaseURL: server.URL,
			}
			client := NewHTTPRegistryClient(config)

			ctx := context.Background()
			result, err := client.GetSchema(ctx, schemaID)

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

			if result == nil {
				t.Errorf("expected schema to be returned")
				return
			}

			if result.ID.String() != tt.expectedSchema.ID.String() {
				t.Errorf("expected schema ID %s, got %s", tt.expectedSchema.ID.String(), result.ID.String())
			}
		})
	}
}

func TestHTTPRegistryClient_ListSchemas(t *testing.T) {
	schemas := []SchemaIdentifier{
		{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		{
			Domain:  "commerce",
			Entity:  "product",
			Version: "v1",
			Raw:     "agntcy:commerce.product.v1",
		},
	}

	tests := []struct {
		name           string
		pattern        string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		expectedCount  int
	}{
		{
			name:    "successful response",
			pattern: "commerce",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				response := struct {
					Schemas []SchemaIdentifier `json:"schemas"`
				}{
					Schemas: schemas,
				}
				json.NewEncoder(w).Encode(response)
			},
			expectError:   false,
			expectedCount: 2,
		},
		{
			name:    "empty pattern",
			pattern: "",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				response := struct {
					Schemas []SchemaIdentifier `json:"schemas"`
				}{
					Schemas: schemas,
				}
				json.NewEncoder(w).Encode(response)
			},
			expectError:   false,
			expectedCount: 2,
		},
		{
			name:    "server error",
			pattern: "commerce",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError: true,
		},
		{
			name:    "invalid JSON response",
			pattern: "commerce",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			config := RegistryConfig{
				BaseURL: server.URL,
			}
			client := NewHTTPRegistryClient(config)

			ctx := context.Background()
			result, err := client.ListSchemas(ctx, tt.pattern)

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

			if len(result) != tt.expectedCount {
				t.Errorf("expected %d schemas, got %d", tt.expectedCount, len(result))
			}
		})
	}
}

func TestHTTPRegistryClient_ValidateSchema(t *testing.T) {
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

	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
	}{
		{
			name: "validation success",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			expectError: false,
		},
		{
			name: "validation failure",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				errorResp := struct {
					Error string `json:"error"`
				}{
					Error: "invalid schema definition",
				}
				json.NewEncoder(w).Encode(errorResp)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			config := RegistryConfig{
				BaseURL: server.URL,
			}
			client := NewHTTPRegistryClient(config)

			ctx := context.Background()
			err := client.ValidateSchema(ctx, schema)

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

func TestHTTPRegistryClient_CheckCompatibility(t *testing.T) {
	currentSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	newSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v2",
		Raw:     "agntcy:commerce.order.v2",
	}

	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		expectedResult bool
	}{
		{
			name: "compatible schemas",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				result := CompatibilityResult{
					Compatible: true,
					Reason:     "schemas are compatible",
				}
				json.NewEncoder(w).Encode(result)
			},
			expectError:    false,
			expectedResult: true,
		},
		{
			name: "incompatible schemas",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				result := CompatibilityResult{
					Compatible: false,
					Reason:     "breaking changes detected",
				}
				json.NewEncoder(w).Encode(result)
			},
			expectError:    false,
			expectedResult: false,
		},
		{
			name: "server error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			config := RegistryConfig{
				BaseURL: server.URL,
			}
			client := NewHTTPRegistryClient(config)

			ctx := context.Background()
			result, err := client.CheckCompatibility(ctx, currentSchema, newSchema)

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

func TestHTTPRegistryClient_NotImplementedMethods(t *testing.T) {
	config := RegistryConfig{
		BaseURL: "https://registry.example.com",
	}
	client := NewHTTPRegistryClient(config)

	ctx := context.Background()
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{"type": "object"}`),
	}
	metadata := &SchemaMetadata{}

	// Test RegisterSchema
	err := client.RegisterSchema(ctx, schema, metadata)
	if err == nil {
		t.Errorf("expected RegisterSchema to return error (not implemented)")
	}

	// Test RegisterOrUpdateSchema
	err = client.RegisterOrUpdateSchema(ctx, schema, metadata)
	if err == nil {
		t.Errorf("expected RegisterOrUpdateSchema to return error (not implemented)")
	}

	// Test DeleteSchema
	err = client.DeleteSchema(ctx, schema.ID)
	if err == nil {
		t.Errorf("expected DeleteSchema to return error (not implemented)")
	}

	// Test GetStats
	stats := client.GetStats()
	if stats.TotalSchemas != 0 {
		t.Errorf("expected empty stats from HTTP client")
	}
}

func TestHTTPRegistryClient_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type header to be 'application/json'")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("expected Accept header to be 'application/json'")
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header to be 'Bearer test-token'")
		}
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("expected X-Custom-Header to be 'custom-value'")
		}

		w.WriteHeader(http.StatusNotFound) // Return 404 to avoid processing
	}))
	defer server.Close()

	config := RegistryConfig{
		BaseURL:   server.URL,
		AuthToken: "test-token",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}
	client := NewHTTPRegistryClient(config)

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	_, err := client.GetSchema(ctx, schemaID)

	// We expect an error (404), but headers should have been set correctly
	if err == nil {
		t.Errorf("expected error due to 404 response")
	}
}

func TestNewMockRegistryClient(t *testing.T) {
	client := NewMockRegistryClient()

	if client == nil {
		t.Errorf("expected mock registry client to be created")
		return
	}

	if client.schemas == nil {
		t.Errorf("expected schemas map to be initialized")
	}

	if client.errors == nil {
		t.Errorf("expected errors map to be initialized")
	}
}

func TestMockRegistryClient_AddSchema(t *testing.T) {
	client := NewMockRegistryClient()

	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{"type": "object"}`),
	}

	client.AddSchema(schema)

	// Verify schema was added
	if len(client.schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(client.schemas))
	}

	stored, exists := client.schemas[schema.ID.String()]
	if !exists {
		t.Errorf("expected schema to be stored")
	}

	if stored.ID.String() != schema.ID.String() {
		t.Errorf("expected stored schema ID %s, got %s", schema.ID.String(), stored.ID.String())
	}
}

func TestMockRegistryClient_SetError(t *testing.T) {
	client := NewMockRegistryClient()

	schemaID := "agntcy:commerce.order.v1"
	testError := fmt.Errorf("test error")

	client.SetError(schemaID, testError)

	// Verify error was set
	if len(client.errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(client.errors))
	}

	stored, exists := client.errors[schemaID]
	if !exists {
		t.Errorf("expected error to be stored")
	}

	if stored.Error() != testError.Error() {
		t.Errorf("expected error message '%s', got '%s'", testError.Error(), stored.Error())
	}
}

func TestMockRegistryClient_GetSchema(t *testing.T) {
	client := NewMockRegistryClient()

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

	// Test getting non-existent schema
	ctx := context.Background()
	_, err := client.GetSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error for non-existent schema")
	}

	// Add schema and test getting it
	client.AddSchema(schema)
	result, err := client.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected schema to be returned")
		return
	}

	if result.ID.String() != schemaID.String() {
		t.Errorf("expected schema ID %s, got %s", schemaID.String(), result.ID.String())
	}

	// Test error case
	testError := fmt.Errorf("test error")
	client.SetError(schemaID.String(), testError)
	_, err = client.GetSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error to be returned")
	}
}

func TestMockRegistryClient_ListSchemas(t *testing.T) {
	client := NewMockRegistryClient()

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

	for _, schema := range schemas {
		client.AddSchema(schema)
	}

	ctx := context.Background()

	// Test listing all schemas
	result, err := client.ListSchemas(ctx, "")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 schemas, got %d", len(result))
	}

	// Test pattern matching
	result, err = client.ListSchemas(ctx, "commerce")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 commerce schemas, got %d", len(result))
	}
}

func TestMockRegistryClient_RegisterSchema(t *testing.T) {
	client := NewMockRegistryClient()

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

	metadata := &SchemaMetadata{
		ID: schemaID,
	}

	ctx := context.Background()

	// Test registering new schema
	err := client.RegisterSchema(ctx, schema, metadata)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify schema was registered
	result, err := client.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting registered schema: %v", err)
	}

	if result.ID.String() != schemaID.String() {
		t.Errorf("expected registered schema ID %s, got %s", schemaID.String(), result.ID.String())
	}

	// Test registering duplicate schema
	err = client.RegisterSchema(ctx, schema, metadata)
	if err == nil {
		t.Errorf("expected error when registering duplicate schema")
	}
}

func TestMockRegistryClient_RegisterOrUpdateSchema(t *testing.T) {
	client := NewMockRegistryClient()

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

	metadata := &SchemaMetadata{
		ID: schemaID,
	}

	ctx := context.Background()

	// Test registering new schema
	err := client.RegisterOrUpdateSchema(ctx, schema, metadata)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Test updating existing schema
	updatedSchema := &Schema{
		ID:         schemaID,
		Definition: json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
	}

	err = client.RegisterOrUpdateSchema(ctx, updatedSchema, metadata)
	if err != nil {
		t.Errorf("unexpected error updating schema: %v", err)
	}

	// Verify schema was updated
	result, err := client.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting updated schema: %v", err)
	}

	if string(result.Definition) != string(updatedSchema.Definition) {
		t.Errorf("expected updated definition, got %s", string(result.Definition))
	}
}

func TestMockRegistryClient_DeleteSchema(t *testing.T) {
	client := NewMockRegistryClient()

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

	// Test deleting non-existent schema
	err := client.DeleteSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error when deleting non-existent schema")
	}

	// Add schema and test deletion
	client.AddSchema(schema)
	err = client.DeleteSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error deleting schema: %v", err)
	}

	// Verify schema was deleted
	_, err = client.GetSchema(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error after deleting schema")
	}
}

func TestMockRegistryClient_GetStats(t *testing.T) {
	client := NewMockRegistryClient()

	// Initially empty
	stats := client.GetStats()
	if stats.TotalSchemas != 0 {
		t.Errorf("expected 0 total schemas, got %d", stats.TotalSchemas)
	}

	// Add schemas
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

	for _, schema := range schemas {
		client.AddSchema(schema)
	}

	stats = client.GetStats()
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

func TestMockRegistryClient_CheckCompatibility(t *testing.T) {
	client := NewMockRegistryClient()

	currentSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	newSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v2",
		Raw:     "agntcy:commerce.order.v2",
	}

	ctx := context.Background()

	// Test compatible schemas (same domain and entity)
	result, err := client.CheckCompatibility(ctx, currentSchema, newSchema)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !result {
		t.Errorf("expected schemas to be compatible")
	}

	// Test incompatible schemas (different domain)
	incompatibleSchema := SchemaIdentifier{
		Domain:  "messaging",
		Entity:  "notification",
		Version: "v1",
		Raw:     "agntcy:messaging.notification.v1",
	}

	result, err = client.CheckCompatibility(ctx, currentSchema, incompatibleSchema)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result {
		t.Errorf("expected schemas to be incompatible")
	}
}
