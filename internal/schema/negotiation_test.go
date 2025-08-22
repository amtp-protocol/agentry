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
	"testing"
)

func TestNewNegotiationEngine(t *testing.T) {
	mockRegistry := NewMockRegistryClient()

	tests := []struct {
		name                     string
		config                   NegotiationConfig
		expectedFallbackStrategy string
		expectedMaxVersionDrift  int
	}{
		{
			name: "default values",
			config: NegotiationConfig{
				Enabled: true,
			},
			expectedFallbackStrategy: "latest",
			expectedMaxVersionDrift:  3,
		},
		{
			name: "custom values",
			config: NegotiationConfig{
				Enabled:          true,
				FallbackStrategy: "previous",
				MaxVersionDrift:  5,
			},
			expectedFallbackStrategy: "previous",
			expectedMaxVersionDrift:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewNegotiationEngine(mockRegistry, tt.config)

			if engine == nil {
				t.Errorf("expected negotiation engine to be created")
				return
			}

			if engine.config.FallbackStrategy != tt.expectedFallbackStrategy {
				t.Errorf("expected fallback strategy %s, got %s", tt.expectedFallbackStrategy, engine.config.FallbackStrategy)
			}

			if engine.config.MaxVersionDrift != tt.expectedMaxVersionDrift {
				t.Errorf("expected max version drift %d, got %d", tt.expectedMaxVersionDrift, engine.config.MaxVersionDrift)
			}
		})
	}
}

func TestNegotiationEngine_NegotiateSchema_Disabled(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled: false,
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	requestedSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	result, err := engine.NegotiateSchema(ctx, requestedSchema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.String() != requestedSchema.String() {
		t.Errorf("expected same schema when negotiation is disabled, got %s", result.String())
	}
}

func TestNegotiationEngine_NegotiateSchema_ExactMatch(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:          true,
		FallbackStrategy: "latest",
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	requestedSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Add the exact schema to mock registry
	schema := &Schema{
		ID:         requestedSchema,
		Definition: []byte(`{"type": "object"}`),
	}
	mockRegistry.AddSchema(schema)

	ctx := context.Background()
	result, err := engine.NegotiateSchema(ctx, requestedSchema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.String() != requestedSchema.String() {
		t.Errorf("expected exact match, got %s", result.String())
	}
}

func TestNegotiationEngine_NegotiateSchema_FallbackLatest(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:          true,
		FallbackStrategy: "latest",
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	requestedSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v3", // Request v3 which doesn't exist
		Raw:     "agntcy:commerce.order.v3",
	}

	// Add v1 and v2 schemas to mock registry
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: []byte(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
				Raw:     "agntcy:commerce.order.v2",
			},
			Definition: []byte(`{"type": "object"}`),
		},
	}

	for _, schema := range schemas {
		mockRegistry.AddSchema(schema)
	}

	ctx := context.Background()
	result, err := engine.NegotiateSchema(ctx, requestedSchema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	// Should return v2 (latest available)
	if result.Version != "v2" {
		t.Errorf("expected latest version v2, got %s", result.Version)
	}
}

func TestNegotiationEngine_NegotiateSchema_FallbackPrevious(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:          true,
		FallbackStrategy: "previous",
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	requestedSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v3", // Request v3 which doesn't exist
		Raw:     "agntcy:commerce.order.v3",
	}

	// Add v1, v2, and v4 schemas to mock registry
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: []byte(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
				Raw:     "agntcy:commerce.order.v2",
			},
			Definition: []byte(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v4",
				Raw:     "agntcy:commerce.order.v4",
			},
			Definition: []byte(`{"type": "object"}`),
		},
	}

	for _, schema := range schemas {
		mockRegistry.AddSchema(schema)
	}

	ctx := context.Background()
	result, err := engine.NegotiateSchema(ctx, requestedSchema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	// Should return v2 (highest version lower than v3)
	if result.Version != "v2" {
		t.Errorf("expected previous version v2, got %s", result.Version)
	}
}

func TestNegotiationEngine_NegotiateSchema_FallbackFail(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:          true,
		FallbackStrategy: "fail",
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	requestedSchema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v3", // Request v3 which doesn't exist
		Raw:     "agntcy:commerce.order.v3",
	}

	// Add other versions but not v3
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: []byte(`{"type": "object"}`),
	}
	mockRegistry.AddSchema(schema)

	ctx := context.Background()
	_, err := engine.NegotiateSchema(ctx, requestedSchema)

	if err == nil {
		t.Errorf("expected error when fallback strategy is 'fail'")
	}
}

func TestNegotiationEngine_parseVersion(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{Enabled: true}
	engine := NewNegotiationEngine(mockRegistry, config)

	tests := []struct {
		name     string
		version  string
		expected int
	}{
		{
			name:     "v1",
			version:  "v1",
			expected: 1,
		},
		{
			name:     "v10",
			version:  "v10",
			expected: 10,
		},
		{
			name:     "without v prefix",
			version:  "5",
			expected: 5,
		},
		{
			name:     "invalid version",
			version:  "invalid",
			expected: 0,
		},
		{
			name:     "empty version",
			version:  "",
			expected: 0,
		},
		{
			name:     "version with letters",
			version:  "v1.2.3",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.parseVersion(tt.version)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestNegotiationEngine_CheckVersionDrift(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:         true,
		MaxVersionDrift: 2,
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	tests := []struct {
		name       string
		requested  SchemaIdentifier
		negotiated SchemaIdentifier
		expected   bool
	}{
		{
			name: "within drift limit",
			requested: SchemaIdentifier{
				Version: "v3",
			},
			negotiated: SchemaIdentifier{
				Version: "v2",
			},
			expected: true,
		},
		{
			name: "at drift limit",
			requested: SchemaIdentifier{
				Version: "v5",
			},
			negotiated: SchemaIdentifier{
				Version: "v3",
			},
			expected: true,
		},
		{
			name: "exceeds drift limit",
			requested: SchemaIdentifier{
				Version: "v5",
			},
			negotiated: SchemaIdentifier{
				Version: "v1",
			},
			expected: false,
		},
		{
			name: "same version",
			requested: SchemaIdentifier{
				Version: "v2",
			},
			negotiated: SchemaIdentifier{
				Version: "v2",
			},
			expected: true,
		},
		{
			name: "negotiated higher than requested",
			requested: SchemaIdentifier{
				Version: "v1",
			},
			negotiated: SchemaIdentifier{
				Version: "v3",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.CheckVersionDrift(tt.requested, tt.negotiated)
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestNegotiationEngine_CheckVersionDrift_Disabled(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled: false, // Disabled
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	requested := SchemaIdentifier{Version: "v10"}
	negotiated := SchemaIdentifier{Version: "v1"}

	// Should always return true when negotiation is disabled
	result := engine.CheckVersionDrift(requested, negotiated)
	if !result {
		t.Errorf("expected true when negotiation is disabled")
	}
}

func TestNegotiationEngine_GetNegotiationInfo(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:          true,
		FallbackStrategy: "latest",
		MaxVersionDrift:  3,
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	// Add schemas to mock registry
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition: []byte(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v3",
				Raw:     "agntcy:commerce.order.v3",
			},
			Definition: []byte(`{"type": "object"}`),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
				Raw:     "agntcy:commerce.order.v2",
			},
			Definition: []byte(`{"type": "object"}`),
		},
	}

	for _, schema := range schemas {
		mockRegistry.AddSchema(schema)
	}

	ctx := context.Background()
	info, err := engine.GetNegotiationInfo(ctx, "commerce", "order")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if info == nil {
		t.Errorf("expected negotiation info to be returned")
		return
	}

	if info.Domain != "commerce" {
		t.Errorf("expected domain 'commerce', got '%s'", info.Domain)
	}

	if info.Entity != "order" {
		t.Errorf("expected entity 'order', got '%s'", info.Entity)
	}

	if len(info.AvailableVersions) != 3 {
		t.Errorf("expected 3 available versions, got %d", len(info.AvailableVersions))
	}

	if info.LatestVersion != "v3" {
		t.Errorf("expected latest version 'v3', got '%s'", info.LatestVersion)
	}

	if !info.NegotiationEnabled {
		t.Errorf("expected negotiation enabled to be true")
	}

	if info.FallbackStrategy != "latest" {
		t.Errorf("expected fallback strategy 'latest', got '%s'", info.FallbackStrategy)
	}

	if info.MaxVersionDrift != 3 {
		t.Errorf("expected max version drift 3, got %d", info.MaxVersionDrift)
	}
}

func TestNegotiationEngine_GetNegotiationInfo_NoSchemas(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled: true,
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	ctx := context.Background()
	_, err := engine.GetNegotiationInfo(ctx, "nonexistent", "schema")

	if err == nil {
		t.Errorf("expected error when no schemas found")
	}
}

func TestNegotiationEngine_findLatestVersion(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{Enabled: true}
	engine := NewNegotiationEngine(mockRegistry, config)

	tests := []struct {
		name       string
		candidates []SchemaIdentifier
		expected   string
	}{
		{
			name: "multiple versions",
			candidates: []SchemaIdentifier{
				{Version: "v1"},
				{Version: "v3"},
				{Version: "v2"},
			},
			expected: "v3",
		},
		{
			name: "single version",
			candidates: []SchemaIdentifier{
				{Version: "v1"},
			},
			expected: "v1",
		},
		{
			name:       "empty candidates",
			candidates: []SchemaIdentifier{},
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.findLatestVersion(tt.candidates)

			if len(tt.candidates) == 0 {
				if result != nil {
					t.Errorf("expected nil for empty candidates")
				}
				return
			}

			if result == nil {
				t.Errorf("expected result to be returned")
				return
			}

			if result.Version != tt.expected {
				t.Errorf("expected version %s, got %s", tt.expected, result.Version)
			}
		})
	}
}

func TestNegotiationEngine_findPreviousVersion(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{Enabled: true}
	engine := NewNegotiationEngine(mockRegistry, config)

	requested := SchemaIdentifier{Version: "v3"}
	candidates := []SchemaIdentifier{
		{Version: "v1"},
		{Version: "v2"},
		{Version: "v4"},
		{Version: "v5"},
	}

	result := engine.findPreviousVersion(requested, candidates)

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	// Should return v2 (highest version lower than v3)
	if result.Version != "v2" {
		t.Errorf("expected version v2, got %s", result.Version)
	}
}

func TestNegotiationEngine_findPreviousVersion_NoPrevious(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{Enabled: true}
	engine := NewNegotiationEngine(mockRegistry, config)

	requested := SchemaIdentifier{Version: "v1"}
	candidates := []SchemaIdentifier{
		{Version: "v2"},
		{Version: "v3"},
	}

	result := engine.findPreviousVersion(requested, candidates)

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	// Should fall back to latest when no previous version found
	if result.Version != "v3" {
		t.Errorf("expected fallback to latest version v3, got %s", result.Version)
	}
}

func TestNegotiationEngine_EdgeCases(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := NegotiationConfig{
		Enabled:          true,
		FallbackStrategy: "latest",
	}
	engine := NewNegotiationEngine(mockRegistry, config)

	t.Run("no matching domain/entity", func(t *testing.T) {
		// Add schema with different domain/entity
		schema := &Schema{
			ID: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			Definition: []byte(`{"type": "object"}`),
		}
		mockRegistry.AddSchema(schema)

		requestedSchema := SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		}

		ctx := context.Background()
		_, err := engine.NegotiateSchema(ctx, requestedSchema)

		if err == nil {
			t.Errorf("expected error when no matching domain/entity found")
		}
	})

	t.Run("invalid version numbers", func(t *testing.T) {
		candidates := []SchemaIdentifier{
			{Version: "invalid"},
			{Version: "v1"},
			{Version: "also-invalid"},
		}

		result := engine.findLatestVersion(candidates)
		if result == nil {
			t.Errorf("expected result to be returned")
			return
		}

		// Should return v1 as it's the only valid version
		if result.Version != "v1" {
			t.Errorf("expected version v1, got %s", result.Version)
		}
	})
}
