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
	"fmt"
	"time"
)

// ErrorReportConfig holds configuration for error reporting
type ErrorReportConfig struct {
	Enabled        bool `yaml:"enabled" json:"enabled"`
	IncludePayload bool `yaml:"include_payload" json:"include_payload"`
}

// ErrorReporter handles validation error reporting and logging
type ErrorReporter struct {
	config ErrorReportConfig
}

// NewErrorReporter creates a new error reporter
func NewErrorReporter(config ErrorReportConfig) *ErrorReporter {
	return &ErrorReporter{
		config: config,
	}
}

// ReportValidationErrors reports validation errors
func (er *ErrorReporter) ReportValidationErrors(ctx context.Context, report *ValidationReport) {
	if !er.config.Enabled {
		return
	}

	// Handle nil report gracefully
	if report == nil {
		fmt.Printf("Validation Error: report - validation report cannot be nil (Code: NIL_REPORT)\n")
		return
	}

	// Log validation errors
	for _, err := range report.Errors {
		fmt.Printf("Validation Error: %s - %s (Code: %s)\n", err.Field, err.Message, err.Code)
	}

	// Log warnings
	for _, warning := range report.Warnings {
		fmt.Printf("Validation Warning: %s - %s (Code: %s)\n", warning.Field, warning.Message, warning.Code)
	}
}

// ValidationReport represents a validation report with detailed error information
type ValidationReport struct {
	MessageID        string            `json:"message_id"`
	SchemaID         string            `json:"schema_id"`
	NegotiatedSchema string            `json:"negotiated_schema,omitempty"`
	Valid            bool              `json:"valid"`
	Errors           []ValidationError `json:"errors,omitempty"`
	Warnings         []ValidationError `json:"warnings,omitempty"`
	Bypassed         bool              `json:"bypassed,omitempty"`
	BypassReason     string            `json:"bypass_reason,omitempty"`
	ProcessingTime   time.Duration     `json:"processing_time"`
	Timestamp        time.Time         `json:"timestamp"`
}

// IsValid returns true if validation passed
func (vr *ValidationReport) IsValid() bool {
	return vr.Valid || vr.Bypassed
}
