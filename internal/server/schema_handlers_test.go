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

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
	"os"
)

// Test schema management handlers when schema manager is not configured
func TestSchemaHandlers_NoSchemaManager(t *testing.T) {
	server := createTestServer() // This creates server without schema manager

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/v1/admin/schemas", `{"id": "agntcy:example.test.v1", "definition": {}}`},
		{"GET", "/v1/admin/schemas", ""},
		{"GET", "/v1/admin/schemas/agntcy:example.test.v1", ""},
		{"PUT", "/v1/admin/schemas/agntcy:example.test.v1", `{"definition": {}}`},
		{"DELETE", "/v1/admin/schemas/agntcy:example.test.v1", ""},
		{"POST", "/v1/admin/schemas/test.v1/validate", `{"payload": {}}`},
		{"GET", "/v1/admin/schemas/stats", ""},
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint.method+"_"+endpoint.path, func(t *testing.T) {
			var req *http.Request
			if endpoint.body != "" {
				req = httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewBufferString(endpoint.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(endpoint.method, endpoint.path, nil)
			}

			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
			}

			var errorResponse types.ErrorResponse
			err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
			if err != nil {
				t.Fatalf("Failed to unmarshal error response: %v", err)
			}

			if errorResponse.Error.Code != "SCHEMA_MANAGER_UNAVAILABLE" {
				t.Errorf("Expected error code 'SCHEMA_MANAGER_UNAVAILABLE', got %s", errorResponse.Error.Code)
			}
		})
	}
}

// Note: Complex schema management tests are temporarily removed due to setup complexity.
// Schema functionality is tested through integration tests instead.

func TestSchemaHandlers_WithSchemaManager(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "schema_handlers_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	sm, err := schema.NewManager(schema.ManagerConfig{
		RegistryType: "local",
		LocalRegistry: schema.LocalRegistryConfig{
			BasePath:   tempDir,
			CreateDirs: true,
		},
	})
	if err != nil {
		t.Fatalf("failed to create schema manager: %v", err)
	}

	server := createTestServer()
	server.schemaManager = sm

	t.Run("POST /v1/admin/schemas - Valid Schema", func(t *testing.T) {
		body := `{"id":"agntcy:test.domain.v1","definition":{"type":"object"}}`
		req := httptest.NewRequest("POST", "/v1/admin/schemas", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
		}
	})

	t.Run("GET /v1/admin/schemas - List Schemas", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/schemas?domain=test.domain", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("GET /v1/admin/schemas/:id - Get Schema", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/schemas/agntcy:test.domain.v1", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("PUT /v1/admin/schemas/:id - Update Schema", func(t *testing.T) {
		body := `{"id":"agntcy:test.domain.v1","definition":{"type":"object", "properties": {}}}`
		req := httptest.NewRequest("PUT", "/v1/admin/schemas/agntcy:test.domain.v1", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("POST /v1/admin/schemas/:id/validate - Validate Payload", func(t *testing.T) {
		body := `{"payload":{}}`
		req := httptest.NewRequest("POST", "/v1/admin/schemas/agntcy:test.domain.v1/validate", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("GET /v1/admin/schemas/stats - Stats", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/schemas/stats", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("DELETE /v1/admin/schemas/:id - Delete Schema", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/v1/admin/schemas/agntcy:test.domain.v1", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
		}
	})
}
