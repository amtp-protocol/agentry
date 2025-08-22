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
	"encoding/json"
	"testing"
	"time"
)

func TestParseSchemaIdentifier(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    *SchemaIdentifier
		expectError bool
	}{
		{
			name:  "valid schema identifier",
			input: "agntcy:commerce.order.v1",
			expected: &SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			expectError: false,
		},
		{
			name:  "valid schema with underscores and hyphens",
			input: "agntcy:e-commerce.order_item.v2",
			expected: &SchemaIdentifier{
				Domain:  "e-commerce",
				Entity:  "order_item",
				Version: "v2",
				Raw:     "agntcy:e-commerce.order_item.v2",
			},
			expectError: false,
		},
		{
			name:        "empty string",
			input:       "",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "missing agntcy prefix",
			input:       "commerce.order.v1",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid format - missing version",
			input:       "agntcy:commerce.order",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid format - too many parts",
			input:       "agntcy:commerce.order.v1.extra",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid version format",
			input:       "agntcy:commerce.order.1",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid characters in domain",
			input:       "agntcy:commerce@.order.v1",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSchemaIdentifier(tt.input)

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
				t.Errorf("expected result but got nil")
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

func TestSchemaIdentifier_String(t *testing.T) {
	tests := []struct {
		name     string
		schema   SchemaIdentifier
		expected string
	}{
		{
			name: "with raw value",
			schema: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			expected: "agntcy:commerce.order.v1",
		},
		{
			name: "without raw value",
			schema: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v2",
			},
			expected: "agntcy:messaging.notification.v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.schema.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSchemaIdentifier_MatchesPattern(t *testing.T) {
	schema := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{
			name:     "empty pattern matches all",
			pattern:  "",
			expected: true,
		},
		{
			name:     "exact match",
			pattern:  "agntcy:commerce.order.v1",
			expected: true,
		},
		{
			name:     "domain match",
			pattern:  "commerce",
			expected: true,
		},
		{
			name:     "domain.entity match",
			pattern:  "commerce.order",
			expected: true,
		},
		{
			name:     "no match",
			pattern:  "messaging",
			expected: false,
		},
		{
			name:     "partial domain no match",
			pattern:  "comm",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := schema.MatchesPattern(tt.pattern)
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestSchemaIdentifier_IsCompatibleWith(t *testing.T) {
	schema1 := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
	}

	tests := []struct {
		name     string
		other    SchemaIdentifier
		expected bool
	}{
		{
			name: "same domain and entity, different version",
			other: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v2",
			},
			expected: true,
		},
		{
			name: "same domain and entity, same version",
			other: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
			},
			expected: true,
		},
		{
			name: "different domain",
			other: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "order",
				Version: "v1",
			},
			expected: false,
		},
		{
			name: "different entity",
			other: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "product",
				Version: "v1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := schema1.IsCompatibleWith(&tt.other)
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestValidationResult_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		result   ValidationResult
		expected bool
	}{
		{
			name: "valid with no errors",
			result: ValidationResult{
				Valid:  true,
				Errors: []ValidationError{},
			},
			expected: true,
		},
		{
			name: "invalid flag",
			result: ValidationResult{
				Valid:  false,
				Errors: []ValidationError{},
			},
			expected: false,
		},
		{
			name: "valid flag but has errors",
			result: ValidationResult{
				Valid: true,
				Errors: []ValidationError{
					{Field: "test", Message: "error", Code: "TEST"},
				},
			},
			expected: false,
		},
		{
			name: "invalid with errors",
			result: ValidationResult{
				Valid: false,
				Errors: []ValidationError{
					{Field: "test", Message: "error", Code: "TEST"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.result.IsValid()
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestValidationResult_AddError(t *testing.T) {
	result := &ValidationResult{Valid: true}

	result.AddError("field1", "error message", "ERROR_CODE", "test_value")

	if result.Valid {
		t.Errorf("expected Valid to be false after adding error")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	error := result.Errors[0]
	if error.Field != "field1" {
		t.Errorf("expected field 'field1', got '%s'", error.Field)
	}
	if error.Message != "error message" {
		t.Errorf("expected message 'error message', got '%s'", error.Message)
	}
	if error.Code != "ERROR_CODE" {
		t.Errorf("expected code 'ERROR_CODE', got '%s'", error.Code)
	}
	if error.Value != "test_value" {
		t.Errorf("expected value 'test_value', got '%v'", error.Value)
	}
}

func TestValidationResult_AddWarning(t *testing.T) {
	result := &ValidationResult{Valid: true}

	result.AddWarning("field1", "warning message", "WARNING_CODE", "test_value")

	if !result.Valid {
		t.Errorf("expected Valid to remain true after adding warning")
	}

	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}

	warning := result.Warnings[0]
	if warning.Field != "field1" {
		t.Errorf("expected field 'field1', got '%s'", warning.Field)
	}
	if warning.Message != "warning message" {
		t.Errorf("expected message 'warning message', got '%s'", warning.Message)
	}
	if warning.Code != "WARNING_CODE" {
		t.Errorf("expected code 'WARNING_CODE', got '%s'", warning.Code)
	}
	if warning.Value != "test_value" {
		t.Errorf("expected value 'test_value', got '%v'", warning.Value)
	}
}

func TestSchema_JSONSerialization(t *testing.T) {
	schema := Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition:  json.RawMessage(`{"type": "object", "properties": {"id": {"type": "string"}}}`),
		PublishedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		Signature:   "test-signature",
	}

	// Test marshaling
	data, err := json.Marshal(schema)
	if err != nil {
		t.Errorf("failed to marshal schema: %v", err)
	}

	// Test unmarshaling
	var unmarshaled Schema
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Errorf("failed to unmarshal schema: %v", err)
	}

	// Verify fields
	if unmarshaled.ID.Domain != schema.ID.Domain {
		t.Errorf("expected domain %s, got %s", schema.ID.Domain, unmarshaled.ID.Domain)
	}
	if unmarshaled.ID.Entity != schema.ID.Entity {
		t.Errorf("expected entity %s, got %s", schema.ID.Entity, unmarshaled.ID.Entity)
	}
	if unmarshaled.ID.Version != schema.ID.Version {
		t.Errorf("expected version %s, got %s", schema.ID.Version, unmarshaled.ID.Version)
	}
	// JSON marshaling may change formatting, so we need to compare the actual JSON content
	var originalDef, unmarshaledDef interface{}
	json.Unmarshal(schema.Definition, &originalDef)
	json.Unmarshal(unmarshaled.Definition, &unmarshaledDef)

	originalJSON, _ := json.Marshal(originalDef)
	unmarshaledJSON, _ := json.Marshal(unmarshaledDef)

	if string(originalJSON) != string(unmarshaledJSON) {
		t.Errorf("expected definition content to match")
	}
	if !unmarshaled.PublishedAt.Equal(schema.PublishedAt) {
		t.Errorf("expected published_at %v, got %v", schema.PublishedAt, unmarshaled.PublishedAt)
	}
	if unmarshaled.Signature != schema.Signature {
		t.Errorf("expected signature %s, got %s", schema.Signature, unmarshaled.Signature)
	}
}

func TestSchemaMetadata_JSONSerialization(t *testing.T) {
	metadata := SchemaMetadata{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Version:   "v1",
		CreatedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
		FilePath:  "commerce/order/v1.json",
		Size:      1024,
		Checksum:  "abc123",
	}

	// Test marshaling
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Errorf("failed to marshal metadata: %v", err)
	}

	// Test unmarshaling
	var unmarshaled SchemaMetadata
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Errorf("failed to unmarshal metadata: %v", err)
	}

	// Verify fields
	if unmarshaled.ID.Domain != metadata.ID.Domain {
		t.Errorf("expected domain %s, got %s", metadata.ID.Domain, unmarshaled.ID.Domain)
	}
	if unmarshaled.Version != metadata.Version {
		t.Errorf("expected version %s, got %s", metadata.Version, unmarshaled.Version)
	}
	if !unmarshaled.CreatedAt.Equal(metadata.CreatedAt) {
		t.Errorf("expected created_at %v, got %v", metadata.CreatedAt, unmarshaled.CreatedAt)
	}
	if !unmarshaled.UpdatedAt.Equal(metadata.UpdatedAt) {
		t.Errorf("expected updated_at %v, got %v", metadata.UpdatedAt, unmarshaled.UpdatedAt)
	}
	if unmarshaled.FilePath != metadata.FilePath {
		t.Errorf("expected file_path %s, got %s", metadata.FilePath, unmarshaled.FilePath)
	}
	if unmarshaled.Size != metadata.Size {
		t.Errorf("expected size %d, got %d", metadata.Size, unmarshaled.Size)
	}
	if unmarshaled.Checksum != metadata.Checksum {
		t.Errorf("expected checksum %s, got %s", metadata.Checksum, unmarshaled.Checksum)
	}
}

func TestValidationError_JSONSerialization(t *testing.T) {
	validationError := ValidationError{
		Field:   "test_field",
		Message: "test message",
		Code:    "TEST_CODE",
		Value:   "test_value",
	}

	// Test marshaling
	data, err := json.Marshal(validationError)
	if err != nil {
		t.Errorf("failed to marshal validation error: %v", err)
	}

	// Test unmarshaling
	var unmarshaled ValidationError
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Errorf("failed to unmarshal validation error: %v", err)
	}

	// Verify fields
	if unmarshaled.Field != validationError.Field {
		t.Errorf("expected field %s, got %s", validationError.Field, unmarshaled.Field)
	}
	if unmarshaled.Message != validationError.Message {
		t.Errorf("expected message %s, got %s", validationError.Message, unmarshaled.Message)
	}
	if unmarshaled.Code != validationError.Code {
		t.Errorf("expected code %s, got %s", validationError.Code, unmarshaled.Code)
	}
	if unmarshaled.Value != validationError.Value {
		t.Errorf("expected value %v, got %v", validationError.Value, unmarshaled.Value)
	}
}
