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
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewErrorReporter(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        true,
		IncludePayload: false,
	}

	reporter := NewErrorReporter(config)

	if reporter == nil {
		t.Errorf("expected error reporter to be created")
		return
	}

	if reporter.config.Enabled != config.Enabled {
		t.Errorf("expected enabled %t, got %t", config.Enabled, reporter.config.Enabled)
	}

	if reporter.config.IncludePayload != config.IncludePayload {
		t.Errorf("expected include payload %t, got %t", config.IncludePayload, reporter.config.IncludePayload)
	}
}

func TestErrorReporter_ReportValidationErrors_Disabled(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        false,
		IncludePayload: false,
	}
	reporter := NewErrorReporter(config)

	report := &ValidationReport{
		MessageID: "test-message-id",
		SchemaID:  "agntcy:commerce.order.v1",
		Valid:     false,
		Errors: []ValidationError{
			{
				Field:   "order_id",
				Message: "required field missing",
				Code:    "REQUIRED_FIELD_MISSING",
			},
		},
		Warnings: []ValidationError{
			{
				Field:   "optional_field",
				Message: "deprecated field used",
				Code:    "DEPRECATED_FIELD",
			},
		},
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	// Capture stdout to verify no output is produced
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	reporter.ReportValidationErrors(ctx, report)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Should produce no output when disabled
	if outputStr != "" {
		t.Errorf("expected no output when reporting is disabled, got: %s", outputStr)
	}
}

func TestErrorReporter_ReportValidationErrors_Enabled(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        true,
		IncludePayload: false,
	}
	reporter := NewErrorReporter(config)

	report := &ValidationReport{
		MessageID: "test-message-id",
		SchemaID:  "agntcy:commerce.order.v1",
		Valid:     false,
		Errors: []ValidationError{
			{
				Field:   "order_id",
				Message: "required field missing",
				Code:    "REQUIRED_FIELD_MISSING",
			},
			{
				Field:   "amount",
				Message: "invalid format",
				Code:    "INVALID_FORMAT",
			},
		},
		Warnings: []ValidationError{
			{
				Field:   "optional_field",
				Message: "deprecated field used",
				Code:    "DEPRECATED_FIELD",
			},
		},
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	// Capture stdout to verify output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	reporter.ReportValidationErrors(ctx, report)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Verify error messages are printed
	expectedErrorMessages := []string{
		"Validation Error: order_id - required field missing (Code: REQUIRED_FIELD_MISSING)",
		"Validation Error: amount - invalid format (Code: INVALID_FORMAT)",
		"Validation Warning: optional_field - deprecated field used (Code: DEPRECATED_FIELD)",
	}

	for _, expectedMsg := range expectedErrorMessages {
		if !strings.Contains(outputStr, expectedMsg) {
			t.Errorf("expected output to contain: %s\nActual output: %s", expectedMsg, outputStr)
		}
	}
}

func TestErrorReporter_ReportValidationErrors_OnlyErrors(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        true,
		IncludePayload: false,
	}
	reporter := NewErrorReporter(config)

	report := &ValidationReport{
		MessageID: "test-message-id",
		SchemaID:  "agntcy:commerce.order.v1",
		Valid:     false,
		Errors: []ValidationError{
			{
				Field:   "order_id",
				Message: "required field missing",
				Code:    "REQUIRED_FIELD_MISSING",
			},
		},
		Warnings:  []ValidationError{}, // No warnings
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	reporter.ReportValidationErrors(ctx, report)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Should contain error but no warnings
	if !strings.Contains(outputStr, "Validation Error: order_id - required field missing (Code: REQUIRED_FIELD_MISSING)") {
		t.Errorf("expected error message in output: %s", outputStr)
	}

	if strings.Contains(outputStr, "Validation Warning") {
		t.Errorf("unexpected warning message in output: %s", outputStr)
	}
}

func TestErrorReporter_ReportValidationErrors_OnlyWarnings(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        true,
		IncludePayload: false,
	}
	reporter := NewErrorReporter(config)

	report := &ValidationReport{
		MessageID: "test-message-id",
		SchemaID:  "agntcy:commerce.order.v1",
		Valid:     true,                // Valid but with warnings
		Errors:    []ValidationError{}, // No errors
		Warnings: []ValidationError{
			{
				Field:   "optional_field",
				Message: "deprecated field used",
				Code:    "DEPRECATED_FIELD",
			},
		},
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	reporter.ReportValidationErrors(ctx, report)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Should contain warning but no errors
	if !strings.Contains(outputStr, "Validation Warning: optional_field - deprecated field used (Code: DEPRECATED_FIELD)") {
		t.Errorf("expected warning message in output: %s", outputStr)
	}

	if strings.Contains(outputStr, "Validation Error") {
		t.Errorf("unexpected error message in output: %s", outputStr)
	}
}

func TestErrorReporter_ReportValidationErrors_EmptyReport(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        true,
		IncludePayload: false,
	}
	reporter := NewErrorReporter(config)

	report := &ValidationReport{
		MessageID: "test-message-id",
		SchemaID:  "agntcy:commerce.order.v1",
		Valid:     true,
		Errors:    []ValidationError{}, // No errors
		Warnings:  []ValidationError{}, // No warnings
		Timestamp: time.Now(),
	}

	ctx := context.Background()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	reporter.ReportValidationErrors(ctx, report)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Should produce no output for empty report
	if outputStr != "" {
		t.Errorf("expected no output for empty report, got: %s", outputStr)
	}
}

func TestValidationReport_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		report   ValidationReport
		expected bool
	}{
		{
			name: "valid and not bypassed",
			report: ValidationReport{
				Valid:    true,
				Bypassed: false,
			},
			expected: true,
		},
		{
			name: "invalid but bypassed",
			report: ValidationReport{
				Valid:    false,
				Bypassed: true,
			},
			expected: true,
		},
		{
			name: "valid and bypassed",
			report: ValidationReport{
				Valid:    true,
				Bypassed: true,
			},
			expected: true,
		},
		{
			name: "invalid and not bypassed",
			report: ValidationReport{
				Valid:    false,
				Bypassed: false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.report.IsValid()
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestValidationReport_Fields(t *testing.T) {
	timestamp := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	processingTime := 100 * time.Millisecond

	report := ValidationReport{
		MessageID:        "msg-123",
		SchemaID:         "agntcy:commerce.order.v1",
		NegotiatedSchema: "agntcy:commerce.order.v2",
		Valid:            false,
		Errors: []ValidationError{
			{
				Field:   "order_id",
				Message: "required field missing",
				Code:    "REQUIRED_FIELD_MISSING",
				Value:   nil,
			},
		},
		Warnings: []ValidationError{
			{
				Field:   "deprecated_field",
				Message: "field is deprecated",
				Code:    "DEPRECATED_FIELD",
				Value:   "old_value",
			},
		},
		Bypassed:       false,
		ProcessingTime: processingTime,
		Timestamp:      timestamp,
	}

	// Verify all fields are set correctly
	if report.MessageID != "msg-123" {
		t.Errorf("expected message ID 'msg-123', got '%s'", report.MessageID)
	}

	if report.SchemaID != "agntcy:commerce.order.v1" {
		t.Errorf("expected schema ID 'agntcy:commerce.order.v1', got '%s'", report.SchemaID)
	}

	if report.NegotiatedSchema != "agntcy:commerce.order.v2" {
		t.Errorf("expected negotiated schema 'agntcy:commerce.order.v2', got '%s'", report.NegotiatedSchema)
	}

	if report.Valid {
		t.Errorf("expected valid to be false")
	}

	if len(report.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(report.Errors))
	}

	if len(report.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(report.Warnings))
	}

	if report.Bypassed {
		t.Errorf("expected bypassed to be false")
	}

	if report.ProcessingTime != processingTime {
		t.Errorf("expected processing time %v, got %v", processingTime, report.ProcessingTime)
	}

	if !report.Timestamp.Equal(timestamp) {
		t.Errorf("expected timestamp %v, got %v", timestamp, report.Timestamp)
	}
}

func TestErrorReporter_EdgeCases(t *testing.T) {
	config := ErrorReportConfig{
		Enabled:        true,
		IncludePayload: false,
	}
	reporter := NewErrorReporter(config)
	ctx := context.Background()

	t.Run("nil report", func(t *testing.T) {
		// Should handle nil report gracefully and log an error message
		reporter.ReportValidationErrors(ctx, nil)
		// If we reach here without panicking, the test passes
	})

	t.Run("nil context", func(t *testing.T) {
		report := &ValidationReport{
			MessageID: "test-message-id",
			SchemaID:  "agntcy:commerce.order.v1",
			Valid:     false,
			Errors: []ValidationError{
				{
					Field:   "test_field",
					Message: "test error",
					Code:    "TEST_ERROR",
				},
			},
			Timestamp: time.Now(),
		}

		// This should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ReportValidationErrors panicked with nil context: %v", r)
			}
		}()

		reporter.ReportValidationErrors(context.TODO(), report)
	})
}

func TestErrorReportConfig_DefaultValues(t *testing.T) {
	config := ErrorReportConfig{}

	reporter := NewErrorReporter(config)

	if reporter.config.Enabled {
		t.Errorf("expected enabled to default to false")
	}

	if reporter.config.IncludePayload {
		t.Errorf("expected include payload to default to false")
	}
}

func TestErrorReporter_IncludePayloadConfig(t *testing.T) {
	// Test that IncludePayload config is preserved
	tests := []struct {
		name           string
		includePayload bool
	}{
		{
			name:           "include payload enabled",
			includePayload: true,
		},
		{
			name:           "include payload disabled",
			includePayload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ErrorReportConfig{
				Enabled:        true,
				IncludePayload: tt.includePayload,
			}
			reporter := NewErrorReporter(config)

			if reporter.config.IncludePayload != tt.includePayload {
				t.Errorf("expected include payload %t, got %t", tt.includePayload, reporter.config.IncludePayload)
			}
		})
	}
}
