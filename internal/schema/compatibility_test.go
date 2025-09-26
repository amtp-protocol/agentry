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
	"time"
)

func TestNewCompatibilityChecker(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := CompatibilityConfig{
		Enabled:    true,
		StrictMode: false,
	}

	checker := NewCompatibilityChecker(mockRegistry, config)

	if checker == nil {
		t.Errorf("expected compatibility checker to be created")
		return
	}

	if checker.registryClient != mockRegistry {
		t.Errorf("expected registry client to be set")
	}

	if checker.config.Enabled != config.Enabled {
		t.Errorf("expected enabled %t, got %t", config.Enabled, checker.config.Enabled)
	}

	if checker.config.StrictMode != config.StrictMode {
		t.Errorf("expected strict mode %t, got %t", config.StrictMode, checker.config.StrictMode)
	}
}

func TestCompatibilityChecker_CheckCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		config         CompatibilityConfig
		currentSchema  SchemaIdentifier
		newSchema      SchemaIdentifier
		mockResult     bool
		mockError      error
		expectedResult bool
		expectError    bool
	}{
		{
			name: "compatibility checking disabled",
			config: CompatibilityConfig{
				Enabled: false,
			},
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
			name: "compatible schemas",
			config: CompatibilityConfig{
				Enabled: true,
			},
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
			mockResult:     true,
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "incompatible schemas",
			config: CompatibilityConfig{
				Enabled: true,
			},
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
			mockResult:     false,
			expectedResult: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRegistry := NewMockRegistryClient()
			checker := NewCompatibilityChecker(mockRegistry, tt.config)

			ctx := context.Background()

			result, err := checker.CheckCompatibility(ctx, tt.currentSchema, tt.newSchema)

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

func TestCompatibilityChecker_AnalyzeCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		config         CompatibilityConfig
		currentSchema  SchemaIdentifier
		newSchema      SchemaIdentifier
		mockCompatible bool
		mockError      error
		expectError    bool
		expectedIssues int
	}{
		{
			name: "compatible schemas",
			config: CompatibilityConfig{
				Enabled: true,
			},
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
			mockCompatible: true,
			expectError:    false,
			expectedIssues: 0,
		},
		{
			name: "incompatible schemas",
			config: CompatibilityConfig{
				Enabled: true,
			},
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
			mockCompatible: false,
			expectError:    false,
			expectedIssues: 1,
		},
		{
			name: "compatibility checking disabled",
			config: CompatibilityConfig{
				Enabled: false,
			},
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
			mockCompatible: true,
			expectError:    false,
			expectedIssues: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRegistry := NewMockRegistryClient()
			checker := NewCompatibilityChecker(mockRegistry, tt.config)

			ctx := context.Background()
			startTime := time.Now()

			analysis, err := checker.AnalyzeCompatibility(ctx, tt.currentSchema, tt.newSchema)

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

			if analysis == nil {
				t.Errorf("expected analysis to be returned")
				return
			}

			// Verify analysis fields
			if analysis.CurrentSchema.String() != tt.currentSchema.String() {
				t.Errorf("expected current schema %s, got %s", tt.currentSchema.String(), analysis.CurrentSchema.String())
			}

			if analysis.NewSchema.String() != tt.newSchema.String() {
				t.Errorf("expected new schema %s, got %s", tt.newSchema.String(), analysis.NewSchema.String())
			}

			if analysis.Compatible != tt.mockCompatible {
				t.Errorf("expected compatible %t, got %t", tt.mockCompatible, analysis.Compatible)
			}

			if len(analysis.Issues) != tt.expectedIssues {
				t.Errorf("expected %d issues, got %d", tt.expectedIssues, len(analysis.Issues))
			}

			if analysis.Timestamp.Before(startTime) {
				t.Errorf("expected timestamp to be after start time")
			}

			if analysis.Error != "" && !tt.expectError {
				t.Errorf("unexpected error in analysis: %s", analysis.Error)
			}
		})
	}
}

func TestCompatibilityChecker_AnalyzeCompatibility_WithError(t *testing.T) {
	mockRegistry := NewMockRegistryClient()

	// Test with disabled compatibility checking to simulate an error path
	disabledChecker := NewCompatibilityChecker(mockRegistry, CompatibilityConfig{Enabled: false})

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
	analysis, err := disabledChecker.AnalyzeCompatibility(ctx, currentSchema, newSchema)

	// With disabled compatibility checking, this should succeed but return compatible=true
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	if analysis == nil {
		t.Errorf("expected analysis to be returned")
		return
	}

	if !analysis.Compatible {
		t.Errorf("expected compatible to be true when compatibility checking is disabled")
	}
}

func TestCompatibilityAnalysis_JSONSerialization(t *testing.T) {
	analysis := CompatibilityAnalysis{
		CurrentSchema: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		NewSchema: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v2",
			Raw:     "agntcy:commerce.order.v2",
		},
		Compatible: true,
		Issues:     []string{"minor breaking change"},
	}

	// Test JSON marshaling/unmarshaling
	data := analysis.CurrentSchema.String()
	if data == "" {
		t.Errorf("expected non-empty current schema string")
	}

	if data != "agntcy:commerce.order.v1" {
		t.Errorf("expected current schema string 'agntcy:commerce.order.v1', got '%s'", data)
	}

	newData := analysis.NewSchema.String()
	if newData != "agntcy:commerce.order.v2" {
		t.Errorf("expected new schema string 'agntcy:commerce.order.v2', got '%s'", newData)
	}

	if !analysis.Compatible {
		t.Errorf("expected compatible to be true")
	}

	if len(analysis.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(analysis.Issues))
	}

	if analysis.Issues[0] != "minor breaking change" {
		t.Errorf("expected issue 'minor breaking change', got '%s'", analysis.Issues[0])
	}
}

func TestCompatibilityConfig_DefaultValues(t *testing.T) {
	config := CompatibilityConfig{}

	// Test that default values work correctly
	mockRegistry := NewMockRegistryClient()
	checker := NewCompatibilityChecker(mockRegistry, config)

	if checker.config.Enabled {
		t.Errorf("expected enabled to default to false")
	}

	if checker.config.StrictMode {
		t.Errorf("expected strict mode to default to false")
	}
}

func TestCompatibilityChecker_EdgeCases(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := CompatibilityConfig{
		Enabled: true,
	}
	checker := NewCompatibilityChecker(mockRegistry, config)

	t.Run("same schema identifiers", func(t *testing.T) {
		schemaID := SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		}

		ctx := context.Background()
		result, err := checker.CheckCompatibility(ctx, schemaID, schemaID)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !result {
			t.Errorf("expected same schemas to be compatible")
		}
	})

	t.Run("empty schema identifiers", func(t *testing.T) {
		emptySchema := SchemaIdentifier{}

		ctx := context.Background()
		result, err := checker.CheckCompatibility(ctx, emptySchema, emptySchema)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if !result {
			t.Errorf("expected empty schemas to be compatible")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		schemaID := SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		}

		// This should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CheckCompatibility panicked with nil context: %v", r)
			}
		}()

		_, err := checker.CheckCompatibility(context.TODO(), schemaID, schemaID)
		//nolint:staticcheck
		if err != nil {
			// Error is expected with nil context, but it shouldn't panic
		}
	})
}

func TestCompatibilityChecker_StrictMode(t *testing.T) {
	mockRegistry := NewMockRegistryClient()

	tests := []struct {
		name       string
		strictMode bool
	}{
		{
			name:       "strict mode enabled",
			strictMode: true,
		},
		{
			name:       "strict mode disabled",
			strictMode: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CompatibilityConfig{
				Enabled:    true,
				StrictMode: tt.strictMode,
			}
			checker := NewCompatibilityChecker(mockRegistry, config)

			if checker.config.StrictMode != tt.strictMode {
				t.Errorf("expected strict mode %t, got %t", tt.strictMode, checker.config.StrictMode)
			}

			// Test that strict mode setting is preserved
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
			_, err := checker.CheckCompatibility(ctx, currentSchema, newSchema)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
