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
	"testing"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestNewValidationPipeline(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled:            true,
		ParallelValidation: false,
	}

	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	if pipeline == nil {
		t.Errorf("expected validation pipeline to be created")
		return
	}

	if pipeline.registryClient != mockRegistry {
		t.Errorf("expected registry client to be set")
	}

	if pipeline.validator != mockValidator {
		t.Errorf("expected validator to be set")
	}

	if pipeline.config.Enabled != config.Enabled {
		t.Errorf("expected enabled %t, got %t", config.Enabled, pipeline.config.Enabled)
	}

	if pipeline.config.ParallelValidation != config.ParallelValidation {
		t.Errorf("expected parallel validation %t, got %t", config.ParallelValidation, pipeline.config.ParallelValidation)
	}
}

func TestValidationPipeline_ValidateMessage_Disabled(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: false, // Disabled
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "test@example.com",
		Payload:   json.RawMessage(`{"order_id": "12345"}`),
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	result, err := pipeline.ValidateMessage(ctx, message, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass when pipeline is disabled")
	}
}

func TestValidationPipeline_ValidateMessage_Enabled(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "test@example.com",
		Payload:   json.RawMessage(`{"order_id": "12345"}`),
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator to return a specific result
	expectedResult := &ValidationResult{
		Valid:  true,
		Errors: []ValidationError{},
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidateMessage(ctx, message, schemaID)

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
}

func TestValidationPipeline_ValidateMessage_WithErrors(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "test@example.com",
		Payload:   json.RawMessage(`{"invalid": "payload"}`),
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator to return validation errors
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
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidateMessage(ctx, message, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	if result.Errors[0].Field != "order_id" {
		t.Errorf("expected error field 'order_id', got '%s'", result.Errors[0].Field)
	}
}

func TestValidationPipeline_ValidateMessage_NilPayload(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "test@example.com",
		Payload:   nil, // Nil payload
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator to handle nil payload
	expectedResult := &ValidationResult{
		Valid: true,
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidateMessage(ctx, message, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass with nil payload")
	}
}

func TestValidationPipeline_ValidateMessage_EmptyPayload(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "test@example.com",
		Payload:   json.RawMessage(`{}`), // Empty JSON object
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator
	expectedResult := &ValidationResult{
		Valid: true,
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidateMessage(ctx, message, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass with empty payload")
	}
}

func TestValidationPipeline_ValidatePayload_Disabled(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: false, // Disabled
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	payload := json.RawMessage(`{"order_id": "12345"}`)
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	result, err := pipeline.ValidatePayload(ctx, payload, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass when pipeline is disabled")
	}
}

func TestValidationPipeline_ValidatePayload_Enabled(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	payload := json.RawMessage(`{"order_id": "12345", "amount": 100.50}`)
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator to return success
	expectedResult := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationError{},
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidatePayload(ctx, payload, schemaID)

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

func TestValidationPipeline_ValidatePayload_WithWarnings(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	payload := json.RawMessage(`{"order_id": "12345", "deprecated_field": "value"}`)
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator to return warnings
	expectedResult := &ValidationResult{
		Valid:  true,
		Errors: []ValidationError{},
		Warnings: []ValidationError{
			{
				Field:   "deprecated_field",
				Message: "field is deprecated",
				Code:    "DEPRECATED_FIELD",
			},
		},
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidatePayload(ctx, payload, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass with warnings")
	}

	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}

	if result.Warnings[0].Field != "deprecated_field" {
		t.Errorf("expected warning field 'deprecated_field', got '%s'", result.Warnings[0].Field)
	}
}

func TestValidationPipeline_ParallelValidation(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled:            true,
		ParallelValidation: true, // Enable parallel validation
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	// Test that parallel validation setting is preserved
	if !pipeline.config.ParallelValidation {
		t.Errorf("expected parallel validation to be enabled")
	}

	// Test normal validation still works
	payload := json.RawMessage(`{"order_id": "12345"}`)
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	expectedResult := &ValidationResult{
		Valid: true,
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidatePayload(ctx, payload, schemaID)

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
}

func TestValidationPipeline_EdgeCases(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	t.Run("nil message", func(t *testing.T) {
		schemaID := SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		}

		ctx := context.Background()

		// Should handle nil message gracefully and return validation error
		result, err := pipeline.ValidateMessage(ctx, nil, schemaID)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result == nil {
			t.Errorf("expected result to be returned")
			return
		}
		if result.Valid {
			t.Errorf("expected validation to fail for nil message")
		}
		if len(result.Errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(result.Errors))
		}
		if len(result.Errors) > 0 && result.Errors[0].Code != "NIL_MESSAGE" {
			t.Errorf("expected error code 'NIL_MESSAGE', got '%s'", result.Errors[0].Code)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		message := &types.Message{
			MessageID: "test-message-id",
			Sender:    "test@example.com",
			Payload:   json.RawMessage(`{"order_id": "12345"}`),
		}

		schemaID := SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		}

		// This should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ValidateMessage panicked with nil context: %v", r)
			}
		}()

		_, err := pipeline.ValidateMessage(context.TODO(), message, schemaID)
		// Error is expected with nil context, but it shouldn't panic
		if err != nil {
			// This is expected
		}
	})

	t.Run("empty schema identifier", func(t *testing.T) {
		message := &types.Message{
			MessageID: "test-message-id",
			Sender:    "test@example.com",
			Payload:   json.RawMessage(`{"order_id": "12345"}`),
		}

		emptySchemaID := SchemaIdentifier{}

		ctx := context.Background()
		result, err := pipeline.ValidateMessage(ctx, message, emptySchemaID)

		if err != nil {
			t.Errorf("unexpected error with empty schema ID: %v", err)
		}

		if result == nil {
			t.Errorf("expected result to be returned")
		}
	})
}

func TestPipelineConfig_DefaultValues(t *testing.T) {
	config := PipelineConfig{}

	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	if pipeline.config.Enabled {
		t.Errorf("expected enabled to default to false")
	}

	if pipeline.config.ParallelValidation {
		t.Errorf("expected parallel validation to default to false")
	}
}

func TestValidationPipeline_LargePayload(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	// Create a large payload
	largeData := make(map[string]interface{})
	for i := 0; i < 1000; i++ {
		largeData[fmt.Sprintf("field_%d", i)] = fmt.Sprintf("value_%d", i)
	}

	payloadBytes, err := json.Marshal(largeData)
	if err != nil {
		t.Fatalf("failed to marshal large payload: %v", err)
	}

	payload := json.RawMessage(payloadBytes)
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator
	expectedResult := &ValidationResult{
		Valid: true,
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidatePayload(ctx, payload, schemaID)

	if err != nil {
		t.Errorf("unexpected error with large payload: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if !result.Valid {
		t.Errorf("expected validation to pass with large payload")
	}
}

func TestValidationPipeline_InvalidJSON(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	mockValidator := NewMockValidator()
	config := PipelineConfig{
		Enabled: true,
	}
	pipeline := NewValidationPipeline(mockRegistry, mockValidator, config)

	message := &types.Message{
		MessageID: "test-message-id",
		Sender:    "test@example.com",
		Payload:   json.RawMessage(`{invalid json`), // Invalid JSON
	}

	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	// Set up mock validator to handle invalid JSON
	expectedResult := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{
				Field:   "payload",
				Message: "invalid JSON",
				Code:    "INVALID_JSON",
			},
		},
	}
	mockValidator.SetValidationResult(schemaID.String(), expectedResult)

	ctx := context.Background()
	result, err := pipeline.ValidateMessage(ctx, message, schemaID)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Errorf("expected result to be returned")
		return
	}

	if result.Valid {
		t.Errorf("expected validation to fail with invalid JSON")
	}
}
