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

	"github.com/amtp-protocol/agentry/internal/types"
)

// PipelineConfig holds configuration for validation pipeline
type PipelineConfig struct {
	Enabled            bool `yaml:"enabled" json:"enabled"`
	ParallelValidation bool `yaml:"parallel_validation" json:"parallel_validation"`
}

// ValidationPipeline orchestrates the validation process
type ValidationPipeline struct {
	registryClient RegistryClient
	validator      Validator
	config         PipelineConfig
}

// NewValidationPipeline creates a new validation pipeline
func NewValidationPipeline(registryClient RegistryClient, validator Validator, config PipelineConfig) *ValidationPipeline {
	return &ValidationPipeline{
		registryClient: registryClient,
		validator:      validator,
		config:         config,
	}
}

// ValidateMessage validates a message through the pipeline
func (vp *ValidationPipeline) ValidateMessage(ctx context.Context, message *types.Message, schemaID SchemaIdentifier) (*ValidationResult, error) {
	if !vp.config.Enabled {
		return &ValidationResult{Valid: true}, nil
	}

	// Handle nil message gracefully
	if message == nil {
		return &ValidationResult{
			Valid: false,
			Errors: []ValidationError{
				{
					Field:   "message",
					Message: "message cannot be nil",
					Code:    "NIL_MESSAGE",
				},
			},
		}, nil
	}

	// Convert payload to json.RawMessage
	var payload json.RawMessage
	if message.Payload != nil {
		payload = json.RawMessage(message.Payload)
	}

	// Validate payload against schema
	return vp.validator.ValidatePayload(ctx, payload, schemaID)
}

// ValidatePayload validates a payload against a schema
func (vp *ValidationPipeline) ValidatePayload(ctx context.Context, payload json.RawMessage, schemaID SchemaIdentifier) (*ValidationResult, error) {
	if !vp.config.Enabled {
		return &ValidationResult{Valid: true}, nil
	}

	return vp.validator.ValidatePayload(ctx, payload, schemaID)
}
