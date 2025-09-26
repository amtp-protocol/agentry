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
	"testing"
	"time"
)

func TestNewJSONSchemaValidator(t *testing.T) {
	mockRegistry := NewMockRegistryClient()

	tests := []struct {
		name               string
		config             ValidatorConfig
		expectedTimeout    time.Duration
		expectedMaxPayload int64
	}{
		{
			name: "default values",
			config: ValidatorConfig{
				Enabled: true,
			},
			expectedTimeout:    30 * time.Second,
			expectedMaxPayload: 10 * 1024 * 1024, // 10MB
		},
		{
			name: "custom values",
			config: ValidatorConfig{
				Enabled:        true,
				Timeout:        60 * time.Second,
				MaxPayloadSize: 5 * 1024 * 1024, // 5MB
			},
			expectedTimeout:    60 * time.Second,
			expectedMaxPayload: 5 * 1024 * 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewJSONSchemaValidator(mockRegistry, tt.config)

			if validator == nil {
				t.Errorf("expected validator to be created")
				return
			}

			if validator.config.Timeout != tt.expectedTimeout {
				t.Errorf("expected timeout %v, got %v", tt.expectedTimeout, validator.config.Timeout)
			}

			if validator.config.MaxPayloadSize != tt.expectedMaxPayload {
				t.Errorf("expected max payload size %d, got %d", tt.expectedMaxPayload, validator.config.MaxPayloadSize)
			}
		})
	}
}

func TestJSONSchemaValidator_ValidatePayload_PayloadTooLarge(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled:        true,
		MaxPayloadSize: 100, // Very small limit
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	// Add a schema to the mock registry first
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
	mockRegistry.AddSchema(schema)

	largePayload := json.RawMessage(`{"data": "this payload is larger than 100 bytes and should be rejected by the validator because it exceeds the configured maximum payload size limit"}`)

	ctx := context.Background()
	result, err := validator.ValidatePayload(ctx, largePayload, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail for large payload")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	if len(result.Errors) > 0 && result.Errors[0].Code != "PAYLOAD_TOO_LARGE" {
		t.Errorf("expected error code 'PAYLOAD_TOO_LARGE', got '%s'", result.Errors[0].Code)
	}
}

func TestJSONSchemaValidator_ValidatePayload_SchemaNotFound(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	payload := json.RawMessage(`{"order_id": "12345"}`)
	schemaID := SchemaIdentifier{
		Domain:  "nonexistent",
		Entity:  "schema",
		Version: "v1",
		Raw:     "agntcy:nonexistent.schema.v1",
	}

	ctx := context.Background()
	_, err := validator.ValidatePayload(ctx, payload, schemaID)

	if err == nil {
		t.Errorf("expected error when schema not found")
	}
}

func TestJSONSchemaValidator_ValidatePayload_Success(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	// Add schema to mock registry
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
				"order_id": {"type": "string"},
				"amount": {"type": "number"}
			},
			"required": ["order_id"]
		}`),
		PublishedAt: time.Now(),
	}
	mockRegistry.AddSchema(schema)

	payload := json.RawMessage(`{"order_id": "12345", "amount": 100.50}`)

	ctx := context.Background()
	result, err := validator.ValidatePayload(ctx, payload, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass")
	}

	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %d", len(result.Errors))
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_InvalidJSON(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	invalidPayload := json.RawMessage(`{invalid json}`)
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{"type": "object"}`),
	}

	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, invalidPayload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail for invalid JSON")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	if result.Errors[0].Code != "INVALID_JSON" {
		t.Errorf("expected error code 'INVALID_JSON', got '%s'", result.Errors[0].Code)
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_TypeMismatch(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	payload := json.RawMessage(`{"order_id": 12345}`) // number instead of string
	schema := &Schema{
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
	}

	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail for type mismatch")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	if result.Errors[0].Code != "TYPE_MISMATCH" {
		t.Errorf("expected error code 'TYPE_MISMATCH', got '%s'", result.Errors[0].Code)
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_RequiredFieldMissing(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	payload := json.RawMessage(`{"amount": 100.50}`) // missing required order_id
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{
			"type": "object",
			"properties": {
				"order_id": {"type": "string"},
				"amount": {"type": "number"}
			},
			"required": ["order_id"]
		}`),
	}

	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail for missing required field")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	if result.Errors[0].Code != "REQUIRED_FIELD_MISSING" {
		t.Errorf("expected error code 'REQUIRED_FIELD_MISSING', got '%s'", result.Errors[0].Code)
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_UnknownProperty(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled:           true,
		StrictMode:        true,
		AllowUnknownProps: false,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	payload := json.RawMessage(`{"order_id": "12345", "unknown_field": "value"}`)
	schema := &Schema{
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
	}

	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass (unknown properties should only generate warnings)")
	}

	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}

	if result.Warnings[0].Code != "UNKNOWN_PROPERTY" {
		t.Errorf("expected warning code 'UNKNOWN_PROPERTY', got '%s'", result.Warnings[0].Code)
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_ArrayValidation(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	payload := json.RawMessage(`{"items": [{"name": "item1"}, {"name": "item2"}]}`)
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{
			"type": "object",
			"properties": {
				"items": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"name": {"type": "string"}
						},
						"required": ["name"]
					}
				}
			}
		}`),
	}

	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass for valid array")
	}

	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %d", len(result.Errors))
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_ArrayValidationError(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	payload := json.RawMessage(`{"items": [{"name": "item1"}, {"invalid": "item2"}]}`) // second item missing required name
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{
			"type": "object",
			"properties": {
				"items": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"name": {"type": "string"}
						},
						"required": ["name"]
					}
				}
			}
		}`),
	}

	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail for invalid array item")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	// Should have error for items[1].name
	if result.Errors[0].Field != "items[1].name" {
		t.Errorf("expected error field 'items[1].name', got '%s'", result.Errors[0].Field)
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_NumericConstraints(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	tests := []struct {
		name        string
		payload     string
		expectValid bool
		expectError string
	}{
		{
			name:        "valid number within range",
			payload:     `{"amount": 50}`,
			expectValid: true,
		},
		{
			name:        "number too small",
			payload:     `{"amount": 5}`,
			expectValid: false,
			expectError: "VALUE_TOO_SMALL",
		},
		{
			name:        "number too large",
			payload:     `{"amount": 150}`,
			expectValid: false,
			expectError: "VALUE_TOO_LARGE",
		},
	}

	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{
			"type": "object",
			"properties": {
				"amount": {
					"type": "number",
					"minimum": 10,
					"maximum": 100
				}
			}
		}`),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := json.RawMessage(tt.payload)

			ctx := context.Background()
			result, err := validator.ValidateWithSchema(ctx, payload, schema)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if result == nil {
				t.Errorf("expected result to be returned")
				return
			}

			if result.Valid != tt.expectValid {
				t.Errorf("expected valid %t, got %t", tt.expectValid, result.Valid)
			}

			if !tt.expectValid && len(result.Errors) > 0 {
				if result.Errors[0].Code != tt.expectError {
					t.Errorf("expected error code '%s', got '%s'", tt.expectError, result.Errors[0].Code)
				}
			}
		})
	}
}

func TestJSONSchemaValidator_ValidateWithSchema_EnumValidation(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{
		Enabled: true,
	}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	tests := []struct {
		name        string
		payload     string
		expectValid bool
	}{
		{
			name:        "valid enum value",
			payload:     `{"status": "pending"}`,
			expectValid: true,
		},
		{
			name:        "invalid enum value",
			payload:     `{"status": "invalid"}`,
			expectValid: false,
		},
	}

	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {
					"type": "string",
					"enum": ["pending", "completed", "canceled"]
				}
			}
		}`),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := json.RawMessage(tt.payload)

			ctx := context.Background()
			result, err := validator.ValidateWithSchema(ctx, payload, schema)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if result == nil {
				t.Errorf("expected result to be returned")
				return
			}

			if result.Valid != tt.expectValid {
				t.Errorf("expected valid %t, got %t", tt.expectValid, result.Valid)
			}

			if !tt.expectValid && len(result.Errors) > 0 {
				if result.Errors[0].Code != "INVALID_ENUM_VALUE" {
					t.Errorf("expected error code 'INVALID_ENUM_VALUE', got '%s'", result.Errors[0].Code)
				}
			}
		})
	}
}

func TestJSONSchemaValidator_ValidateType(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{Enabled: true}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	tests := []struct {
		name         string
		data         interface{}
		expectedType string
		expected     bool
	}{
		{"string type", "hello", "string", true},
		{"string type mismatch", 123, "string", false},
		{"number type", 123.45, "number", true},
		{"number type mismatch", "hello", "number", false},
		{"integer type", float64(123), "integer", true},
		{"integer type with decimal", 123.45, "integer", false},
		{"boolean type", true, "boolean", true},
		{"boolean type mismatch", "true", "boolean", false},
		{"array type", []interface{}{1, 2, 3}, "array", true},
		{"array type mismatch", "not array", "array", false},
		{"object type", map[string]interface{}{"key": "value"}, "object", true},
		{"object type mismatch", "not object", "object", false},
		{"null type", nil, "null", true},
		{"null type mismatch", "not null", "null", false},
		{"unknown type", "anything", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.validateType(tt.data, tt.expectedType)
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestJSONSchemaValidator_ValidateFormat(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{Enabled: true}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	tests := []struct {
		name     string
		value    string
		format   string
		expected bool
	}{
		{"valid email", "user@example.com", "email", true},
		{"invalid email", "invalid-email", "email", false},
		{"valid uri", "https://example.com", "uri", true},
		{"valid uri http", "http://example.com", "uri", true},
		{"invalid uri", "not-a-uri", "uri", false},
		{"valid date", "2023-01-01", "date", true},
		{"invalid date", "not-a-date", "date", false},
		{"valid date-time", "2023-01-01T12:00:00Z", "date-time", true},
		{"invalid date-time", "not-a-datetime", "date-time", false},
		{"unknown format", "anything", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.validateFormat(tt.value, tt.format)
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestJSONSchemaValidator_PerformBasicStructuralValidation(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	config := ValidatorConfig{Enabled: true}
	validator := NewJSONSchemaValidator(mockRegistry, config)

	tests := []struct {
		name        string
		payload     string
		schema      *Schema
		expectValid bool
		expectError string
	}{
		{
			name:    "commerce order with required fields",
			payload: `{"order_id": "12345", "customer": "john@example.com"}`,
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain: "commerce",
					Entity: "order",
				},
			},
			expectValid: true,
		},
		{
			name:    "commerce order missing order_id",
			payload: `{"customer": "john@example.com"}`,
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain: "commerce",
					Entity: "order",
				},
			},
			expectValid: false,
			expectError: "REQUIRED_FIELD_MISSING",
		},
		{
			name:    "messaging with message_id",
			payload: `{"message_id": "msg-123", "content": "hello"}`,
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain: "messaging",
					Entity: "notification",
				},
			},
			expectValid: true,
		},
		{
			name:    "messaging missing message_id",
			payload: `{"content": "hello"}`,
			schema: &Schema{
				ID: SchemaIdentifier{
					Domain: "messaging",
					Entity: "notification",
				},
			},
			expectValid: false,
			expectError: "REQUIRED_FIELD_MISSING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := json.RawMessage(tt.payload)

			result, err := validator.performBasicStructuralValidation(payload, tt.schema)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if result == nil {
				t.Errorf("expected result to be returned")
				return
			}

			if result.Valid != tt.expectValid {
				t.Errorf("expected valid %t, got %t", tt.expectValid, result.Valid)
			}

			if !tt.expectValid && len(result.Errors) > 0 {
				if result.Errors[0].Code != tt.expectError {
					t.Errorf("expected error code '%s', got '%s'", tt.expectError, result.Errors[0].Code)
				}
			}
		})
	}
}

func TestNewMockValidator(t *testing.T) {
	validator := NewMockValidator()

	if validator == nil {
		t.Errorf("expected mock validator to be created")
		return
	}

	if validator.schemas == nil {
		t.Errorf("expected schemas map to be initialized")
	}

	if validator.results == nil {
		t.Errorf("expected results map to be initialized")
	}
}

func TestMockValidator_SetValidationResult(t *testing.T) {
	validator := NewMockValidator()

	schemaID := "agntcy:commerce.order.v1"
	expectedResult := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{
				Field:   "order_id",
				Message: "required field missing",
				Code:    "REQUIRED_FIELD_MISSING",
			},
		},
	}

	validator.SetValidationResult(schemaID, expectedResult)

	// Verify result was set
	if len(validator.results) != 1 {
		t.Errorf("expected 1 result, got %d", len(validator.results))
	}

	stored, exists := validator.results[schemaID]
	if !exists {
		t.Errorf("expected result to be stored")
	}

	if stored.Valid != expectedResult.Valid {
		t.Errorf("expected valid %t, got %t", expectedResult.Valid, stored.Valid)
	}

	if len(stored.Errors) != len(expectedResult.Errors) {
		t.Errorf("expected %d errors, got %d", len(expectedResult.Errors), len(stored.Errors))
	}
}

func TestMockValidator_ValidatePayload(t *testing.T) {
	validator := NewMockValidator()

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	payload := json.RawMessage(`{"order_id": "12345"}`)

	// Test default behavior (should return valid)
	ctx := context.Background()
	result, err := validator.ValidatePayload(ctx, payload, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected default validation to pass")
	}

	// Test with predefined result
	expectedResult := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{
				Field:   "amount",
				Message: "required field missing",
				Code:    "REQUIRED_FIELD_MISSING",
			},
		},
	}
	validator.SetValidationResult(schemaID.String(), expectedResult)

	result, err = validator.ValidatePayload(ctx, payload, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail with predefined result")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestMockValidator_ValidateWithSchema(t *testing.T) {
	validator := NewMockValidator()

	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition: json.RawMessage(`{"type": "object"}`),
	}

	payload := json.RawMessage(`{"order_id": "12345"}`)

	// Test default behavior (should return valid)
	ctx := context.Background()
	result, err := validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected default validation to pass")
	}

	// Test with predefined result
	expectedResult := &ValidationResult{
		Valid: true,
		Warnings: []ValidationError{
			{
				Field:   "deprecated_field",
				Message: "field is deprecated",
				Code:    "DEPRECATED_FIELD",
			},
		},
	}
	validator.SetValidationResult(schema.ID.String(), expectedResult)

	result, err = validator.ValidateWithSchema(ctx, payload, schema)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass with predefined result")
	}

	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}
}
