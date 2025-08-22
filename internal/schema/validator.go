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
	"strings"
	"time"
)

// ValidatorConfig holds configuration for schema validation
type ValidatorConfig struct {
	Enabled           bool          `yaml:"enabled" json:"enabled"`
	StrictMode        bool          `yaml:"strict_mode" json:"strict_mode"`
	Timeout           time.Duration `yaml:"timeout" json:"timeout"`
	MaxPayloadSize    int64         `yaml:"max_payload_size" json:"max_payload_size"`
	AllowUnknownProps bool          `yaml:"allow_unknown_props" json:"allow_unknown_props"`
}

// JSONSchemaValidator implements Validator interface using JSON Schema validation
type JSONSchemaValidator struct {
	registryClient RegistryClient
	config         ValidatorConfig
}

// NewJSONSchemaValidator creates a new JSON schema validator
func NewJSONSchemaValidator(registryClient RegistryClient, config ValidatorConfig) *JSONSchemaValidator {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxPayloadSize == 0 {
		config.MaxPayloadSize = 10 * 1024 * 1024 // 10MB
	}

	return &JSONSchemaValidator{
		registryClient: registryClient,
		config:         config,
	}
}

// ValidatePayload validates a payload against a schema
func (v *JSONSchemaValidator) ValidatePayload(ctx context.Context, payload json.RawMessage, schemaID SchemaIdentifier) (*ValidationResult, error) {
	// Check payload size
	if int64(len(payload)) > v.config.MaxPayloadSize {
		result := &ValidationResult{Valid: false}
		result.AddError("payload", "payload too large", "PAYLOAD_TOO_LARGE", len(payload))
		return result, nil
	}

	// Get schema from registry
	schema, err := v.registryClient.GetSchema(ctx, schemaID)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema %s: %w", schemaID.String(), err)
	}

	return v.ValidateWithSchema(ctx, payload, schema)
}

// ValidateWithSchema validates a payload against a provided schema definition
func (v *JSONSchemaValidator) ValidateWithSchema(ctx context.Context, payload json.RawMessage, schema *Schema) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Parse payload as JSON to ensure it's valid
	var payloadData interface{}
	if err := json.Unmarshal(payload, &payloadData); err != nil {
		result.AddError("payload", "invalid JSON", "INVALID_JSON", string(payload))
		return result, nil
	}

	// Parse schema definition
	var schemaData map[string]interface{}
	if err := json.Unmarshal(schema.Definition, &schemaData); err != nil {
		return nil, fmt.Errorf("invalid schema definition: %w", err)
	}

	// Perform validation
	if err := v.validateAgainstSchema(payloadData, schemaData, "", result); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	return result, nil
}

// validateAgainstSchema performs the actual validation logic
func (v *JSONSchemaValidator) validateAgainstSchema(data interface{}, schema map[string]interface{}, path string, result *ValidationResult) error {
	// This is a simplified JSON Schema validator
	// In a production system, you would use a proper JSON Schema library like github.com/xeipuuv/gojsonschema

	// Check type
	if schemaType, ok := schema["type"].(string); ok {
		if !v.validateType(data, schemaType) {
			result.AddError(path, fmt.Sprintf("expected type %s", schemaType), "TYPE_MISMATCH", data)
		}
	}

	// Check required properties for objects
	if dataObj, ok := data.(map[string]interface{}); ok {
		if required, ok := schema["required"].([]interface{}); ok {
			for _, reqField := range required {
				if fieldName, ok := reqField.(string); ok {
					if _, exists := dataObj[fieldName]; !exists {
						fieldPath := path
						if fieldPath != "" {
							fieldPath += "."
						}
						fieldPath += fieldName
						result.AddError(fieldPath, "required field missing", "REQUIRED_FIELD_MISSING", nil)
					}
				}
			}
		}

		// Validate properties
		if properties, ok := schema["properties"].(map[string]interface{}); ok {
			for fieldName, fieldValue := range dataObj {
				fieldPath := path
				if fieldPath != "" {
					fieldPath += "."
				}
				fieldPath += fieldName

				if fieldSchema, ok := properties[fieldName].(map[string]interface{}); ok {
					if err := v.validateAgainstSchema(fieldValue, fieldSchema, fieldPath, result); err != nil {
						return err
					}
				} else if v.config.StrictMode && !v.config.AllowUnknownProps {
					result.AddWarning(fieldPath, "unknown property", "UNKNOWN_PROPERTY", fieldValue)
				}
			}
		}
	}

	// Check array items
	if dataArray, ok := data.([]interface{}); ok {
		if items, ok := schema["items"].(map[string]interface{}); ok {
			for i, item := range dataArray {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := v.validateAgainstSchema(item, items, itemPath, result); err != nil {
					return err
				}
			}
		}
	}

	// Check string format
	if dataStr, ok := data.(string); ok {
		if format, ok := schema["format"].(string); ok {
			if !v.validateFormat(dataStr, format) {
				result.AddError(path, fmt.Sprintf("invalid format %s", format), "INVALID_FORMAT", dataStr)
			}
		}
	}

	// Check numeric constraints
	if dataNum, ok := data.(float64); ok {
		if minimum, ok := schema["minimum"].(float64); ok {
			if dataNum < minimum {
				result.AddError(path, fmt.Sprintf("value %f is less than minimum %f", dataNum, minimum), "VALUE_TOO_SMALL", dataNum)
			}
		}
		if maximum, ok := schema["maximum"].(float64); ok {
			if dataNum > maximum {
				result.AddError(path, fmt.Sprintf("value %f is greater than maximum %f", dataNum, maximum), "VALUE_TOO_LARGE", dataNum)
			}
		}
	}

	// Check enum values
	if enum, ok := schema["enum"].([]interface{}); ok {
		valid := false
		for _, enumValue := range enum {
			if data == enumValue {
				valid = true
				break
			}
		}
		if !valid {
			result.AddError(path, "value not in enum", "INVALID_ENUM_VALUE", data)
		}
	}

	return nil
}

// validateType checks if data matches the expected JSON Schema type
func (v *JSONSchemaValidator) validateType(data interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := data.(string)
		return ok
	case "number":
		_, ok := data.(float64)
		return ok
	case "integer":
		if num, ok := data.(float64); ok {
			return num == float64(int64(num))
		}
		return false
	case "boolean":
		_, ok := data.(bool)
		return ok
	case "array":
		_, ok := data.([]interface{})
		return ok
	case "object":
		_, ok := data.(map[string]interface{})
		return ok
	case "null":
		return data == nil
	default:
		return true // Unknown type, assume valid
	}
}

// validateFormat validates string formats
func (v *JSONSchemaValidator) validateFormat(value, format string) bool {
	switch format {
	case "email":
		return strings.Contains(value, "@") && strings.Contains(value, ".")
	case "uri":
		return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
	case "date":
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, value)
		return err == nil
	default:
		return true // Unknown format, assume valid
	}
}

// performBasicStructuralValidation performs basic structural validation
func (v *JSONSchemaValidator) performBasicStructuralValidation(payload json.RawMessage, schema *Schema) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Parse payload
	var payloadData map[string]interface{}
	if err := json.Unmarshal(payload, &payloadData); err != nil {
		result.AddError("payload", "invalid JSON", "INVALID_JSON", string(payload))
		return result, nil
	}

	// Basic validation based on schema domain/entity
	switch schema.ID.Domain {
	case "commerce":
		if schema.ID.Entity == "order" {
			// Validate commerce order structure
			if _, ok := payloadData["order_id"]; !ok {
				result.AddError("order_id", "required field missing", "REQUIRED_FIELD_MISSING", nil)
			}
			if _, ok := payloadData["customer"]; !ok {
				result.AddError("customer", "required field missing", "REQUIRED_FIELD_MISSING", nil)
			}
		}
	case "messaging":
		// Validate messaging structure
		if _, ok := payloadData["message_id"]; !ok {
			result.AddError("message_id", "required field missing", "REQUIRED_FIELD_MISSING", nil)
		}
	}

	return result, nil
}

// MockValidator implements Validator interface for testing
type MockValidator struct {
	schemas map[string]*Schema
	results map[string]*ValidationResult
}

// NewMockValidator creates a new mock validator
func NewMockValidator() *MockValidator {
	return &MockValidator{
		schemas: make(map[string]*Schema),
		results: make(map[string]*ValidationResult),
	}
}

// SetValidationResult sets a predefined validation result for a schema
func (m *MockValidator) SetValidationResult(schemaID string, result *ValidationResult) {
	m.results[schemaID] = result
}

// ValidatePayload validates a payload using predefined results
func (m *MockValidator) ValidatePayload(ctx context.Context, payload json.RawMessage, schemaID SchemaIdentifier) (*ValidationResult, error) {
	if result, exists := m.results[schemaID.String()]; exists {
		return result, nil
	}

	// Default to valid
	return &ValidationResult{Valid: true}, nil
}

// ValidateWithSchema validates a payload against a schema using predefined results
func (m *MockValidator) ValidateWithSchema(ctx context.Context, payload json.RawMessage, schema *Schema) (*ValidationResult, error) {
	if result, exists := m.results[schema.ID.String()]; exists {
		return result, nil
	}

	// Default to valid
	return &ValidationResult{Valid: true}, nil
}
