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

package errors

import (
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(ErrValidationFailed, "Test validation error")

	if err.Code != ErrValidationFailed {
		t.Errorf("Expected code %s, got %s", ErrValidationFailed, err.Code)
	}

	if err.Message != "Test validation error" {
		t.Errorf("Expected message 'Test validation error', got %s", err.Message)
	}

	if err.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	if err.Cause != nil {
		t.Error("Expected no cause for new error")
	}
}

func TestNewf(t *testing.T) {
	err := Newf(ErrInvalidMessageID, "Invalid message ID: %s", "test-id")

	if err.Code != ErrInvalidMessageID {
		t.Errorf("Expected code %s, got %s", ErrInvalidMessageID, err.Code)
	}

	expectedMessage := "Invalid message ID: test-id"
	if err.Message != expectedMessage {
		t.Errorf("Expected message '%s', got %s", expectedMessage, err.Message)
	}
}

func TestWrap(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := Wrap(ErrProcessingFailed, "Processing failed", cause)

	if err.Code != ErrProcessingFailed {
		t.Errorf("Expected code %s, got %s", ErrProcessingFailed, err.Code)
	}

	if err.Message != "Processing failed" {
		t.Errorf("Expected message 'Processing failed', got %s", err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Expected cause to be set to %v, got %v", cause, err.Cause)
	}
}

func TestWrapf(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := Wrapf(ErrDeliveryFailed, cause, "Delivery failed for %s", "test@example.com")

	if err.Code != ErrDeliveryFailed {
		t.Errorf("Expected code %s, got %s", ErrDeliveryFailed, err.Code)
	}

	expectedMessage := "Delivery failed for test@example.com"
	if err.Message != expectedMessage {
		t.Errorf("Expected message '%s', got %s", expectedMessage, err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Expected cause to be set to %v, got %v", cause, err.Cause)
	}
}

func TestWithDetails(t *testing.T) {
	details := map[string]interface{}{
		"field":  "sender",
		"value":  "invalid-email",
		"reason": "invalid format",
	}

	err := New(ErrValidationFailed, "Validation failed").WithDetails(details)

	if err.Details == nil {
		t.Fatal("Expected details to be set")
	}

	if err.Details["field"] != "sender" {
		t.Errorf("Expected field 'sender', got %v", err.Details["field"])
	}

	if err.Details["value"] != "invalid-email" {
		t.Errorf("Expected value 'invalid-email', got %v", err.Details["value"])
	}
}

func TestWithRequestID(t *testing.T) {
	requestID := "req-123456"
	err := New(ErrInternalError, "Internal error").WithRequestID(requestID)

	if err.RequestID != requestID {
		t.Errorf("Expected request ID '%s', got %s", requestID, err.RequestID)
	}
}

func TestError(t *testing.T) {
	// Test error without cause
	err := New(ErrValidationFailed, "Validation failed")
	expectedError := "VALIDATION_FAILED: Validation failed"
	if err.Error() != expectedError {
		t.Errorf("Expected error string '%s', got %s", expectedError, err.Error())
	}

	// Test error with cause
	cause := fmt.Errorf("underlying error")
	errWithCause := Wrap(ErrProcessingFailed, "Processing failed", cause)
	expectedErrorWithCause := "PROCESSING_FAILED: Processing failed (caused by: underlying error)"
	if errWithCause.Error() != expectedErrorWithCause {
		t.Errorf("Expected error string '%s', got %s", expectedErrorWithCause, errWithCause.Error())
	}
}

func TestUnwrap(t *testing.T) {
	// Test error without cause
	err := New(ErrValidationFailed, "Validation failed")
	if err.Unwrap() != nil {
		t.Error("Expected nil when unwrapping error without cause")
	}

	// Test error with cause
	cause := fmt.Errorf("underlying error")
	errWithCause := Wrap(ErrProcessingFailed, "Processing failed", cause)
	if errWithCause.Unwrap() != cause {
		t.Errorf("Expected cause %v when unwrapping, got %v", cause, errWithCause.Unwrap())
	}
}

func TestToErrorResponse(t *testing.T) {
	details := map[string]interface{}{
		"field": "sender",
		"value": "invalid-email",
	}

	err := New(ErrValidationFailed, "Validation failed").
		WithDetails(details).
		WithRequestID("req-123456")

	response := err.ToErrorResponse()

	if response.Error.Code != string(ErrValidationFailed) {
		t.Errorf("Expected code %s, got %s", ErrValidationFailed, response.Error.Code)
	}

	if response.Error.Message != "Validation failed" {
		t.Errorf("Expected message 'Validation failed', got %s", response.Error.Message)
	}

	if response.Error.RequestID != "req-123456" {
		t.Errorf("Expected request ID 'req-123456', got %s", response.Error.RequestID)
	}

	if response.Error.Details == nil {
		t.Fatal("Expected details to be set")
	}

	if response.Error.Details["field"] != "sender" {
		t.Errorf("Expected field 'sender', got %v", response.Error.Details["field"])
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		code      ErrorCode
		cause     error
		retryable bool
	}{
		{ErrServerError, nil, true},
		{ErrTimeout, nil, true},
		{ErrServiceUnavailable, nil, true},
		{ErrRateLimitExceeded, nil, true},
		{ErrHTTPRequestFailed, fmt.Errorf("timeout"), true},
		{ErrHTTPRequestFailed, fmt.Errorf("connection refused"), true},
		{ErrHTTPRequestFailed, fmt.Errorf("no such host"), true},
		{ErrHTTPRequestFailed, fmt.Errorf("network unreachable"), true},
		{ErrHTTPRequestFailed, fmt.Errorf("connection reset"), true},
		{ErrHTTPRequestFailed, fmt.Errorf("some other error"), false},
		{ErrValidationFailed, nil, false},
		{ErrInvalidMessageID, nil, false},
		{ErrMessageNotFound, nil, false},
		{ErrUnauthorized, nil, false},
	}

	for _, test := range tests {
		err := &AMTPError{
			Code:  test.code,
			Cause: test.cause,
		}

		result := err.IsRetryable()
		if result != test.retryable {
			t.Errorf("IsRetryable() for %s with cause %v = %v, expected %v",
				test.code, test.cause, result, test.retryable)
		}
	}
}

func TestGetHTTPStatus(t *testing.T) {
	tests := []struct {
		code           ErrorCode
		expectedStatus int
	}{
		{ErrInvalidRequestFormat, 400},
		{ErrValidationFailed, 400},
		{ErrMessageValidationFailed, 400},
		{ErrInvalidMessageID, 400},
		{ErrInvalidRecipient, 400},
		{ErrMessageTooLarge, 400},
		{ErrUnauthorized, 401},
		{ErrInvalidCredentials, 401},
		{ErrTokenExpired, 401},
		{ErrForbidden, 403},
		{ErrMessageNotFound, 404},
		{ErrStatusNotFound, 404},
		{ErrRateLimitExceeded, 429},
		{ErrQuotaExceeded, 429},
		{ErrInternalError, 500},
		{ErrProcessingFailed, 500},
		{ErrIDGenerationFailed, 500},
		{ErrServiceUnavailable, 503},
		{ErrMaintenanceMode, 503},
		{ErrTimeout, 504},
		{ErrorCode("UNKNOWN_ERROR"), 500}, // Default case
	}

	for _, test := range tests {
		err := &AMTPError{Code: test.code}
		status := err.GetHTTPStatus()
		if status != test.expectedStatus {
			t.Errorf("GetHTTPStatus() for %s = %d, expected %d",
				test.code, status, test.expectedStatus)
		}
	}
}

func TestNewValidationError(t *testing.T) {
	details := map[string]interface{}{
		"field": "sender",
		"value": "invalid-email",
	}

	err := NewValidationError("Invalid sender email", details)

	if err.Code != ErrValidationFailed {
		t.Errorf("Expected code %s, got %s", ErrValidationFailed, err.Code)
	}

	if err.Message != "Invalid sender email" {
		t.Errorf("Expected message 'Invalid sender email', got %s", err.Message)
	}

	if err.Details == nil {
		t.Fatal("Expected details to be set")
	}

	if err.Details["field"] != "sender" {
		t.Errorf("Expected field 'sender', got %v", err.Details["field"])
	}
}

func TestNewProcessingError(t *testing.T) {
	cause := fmt.Errorf("underlying processing error")
	err := NewProcessingError("Message processing failed", cause)

	if err.Code != ErrProcessingFailed {
		t.Errorf("Expected code %s, got %s", ErrProcessingFailed, err.Code)
	}

	if err.Message != "Message processing failed" {
		t.Errorf("Expected message 'Message processing failed', got %s", err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Expected cause to be set to %v, got %v", cause, err.Cause)
	}
}

func TestNewDeliveryError(t *testing.T) {
	cause := fmt.Errorf("HTTP request failed")
	err := NewDeliveryError("Failed to deliver message", cause)

	if err.Code != ErrDeliveryFailed {
		t.Errorf("Expected code %s, got %s", ErrDeliveryFailed, err.Code)
	}

	if err.Message != "Failed to deliver message" {
		t.Errorf("Expected message 'Failed to deliver message', got %s", err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Expected cause to be set to %v, got %v", cause, err.Cause)
	}
}

func TestNewDiscoveryError(t *testing.T) {
	cause := fmt.Errorf("DNS lookup failed")
	err := NewDiscoveryError("Failed to discover capabilities", cause)

	if err.Code != ErrDiscoveryFailed {
		t.Errorf("Expected code %s, got %s", ErrDiscoveryFailed, err.Code)
	}

	if err.Message != "Failed to discover capabilities" {
		t.Errorf("Expected message 'Failed to discover capabilities', got %s", err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Expected cause to be set to %v, got %v", cause, err.Cause)
	}
}

func TestNewNotFoundError(t *testing.T) {
	err := NewNotFoundError("message")

	if err.Code != ErrMessageNotFound {
		t.Errorf("Expected code %s, got %s", ErrMessageNotFound, err.Code)
	}

	expectedMessage := "message not found"
	if err.Message != expectedMessage {
		t.Errorf("Expected message '%s', got %s", expectedMessage, err.Message)
	}
}

func TestNewInternalError(t *testing.T) {
	cause := fmt.Errorf("database connection failed")
	err := NewInternalError("Internal system error", cause)

	if err.Code != ErrInternalError {
		t.Errorf("Expected code %s, got %s", ErrInternalError, err.Code)
	}

	if err.Message != "Internal system error" {
		t.Errorf("Expected message 'Internal system error', got %s", err.Message)
	}

	if err.Cause != cause {
		t.Errorf("Expected cause to be set to %v, got %v", cause, err.Cause)
	}
}

func TestErrorFromCode(t *testing.T) {
	err := ErrorFromCode("CUSTOM_ERROR", "Custom error message")

	if err.Code != ErrorCode("CUSTOM_ERROR") {
		t.Errorf("Expected code CUSTOM_ERROR, got %s", err.Code)
	}

	if err.Message != "Custom error message" {
		t.Errorf("Expected message 'Custom error message', got %s", err.Message)
	}
}

func TestIsAMTPError(t *testing.T) {
	// Test with AMTP error
	amtpErr := New(ErrValidationFailed, "Validation failed")
	if !IsAMTPError(amtpErr) {
		t.Error("Expected IsAMTPError to return true for AMTP error")
	}

	// Test with regular error
	regularErr := fmt.Errorf("regular error")
	if IsAMTPError(regularErr) {
		t.Error("Expected IsAMTPError to return false for regular error")
	}
}

func TestAsAMTPError(t *testing.T) {
	// Test with AMTP error
	amtpErr := New(ErrValidationFailed, "Validation failed")
	convertedErr, ok := AsAMTPError(amtpErr)
	if !ok {
		t.Error("Expected AsAMTPError to return true for AMTP error")
	}
	if convertedErr != amtpErr {
		t.Error("Expected converted error to be the same as original")
	}

	// Test with regular error
	regularErr := fmt.Errorf("regular error")
	_, ok = AsAMTPError(regularErr)
	if ok {
		t.Error("Expected AsAMTPError to return false for regular error")
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		s          string
		substrings []string
		expected   bool
	}{
		{"timeout occurred", []string{"timeout", "connection"}, true},
		{"connection refused", []string{"timeout", "connection"}, true},
		{"no such host", []string{"timeout", "connection", "host"}, true},
		{"some other error", []string{"timeout", "connection"}, false},
		{"", []string{"timeout"}, false},
		{"timeout", []string{}, false},
	}

	for _, test := range tests {
		result := containsAny(test.s, test.substrings)
		if result != test.expected {
			t.Errorf("containsAny(%s, %v) = %v, expected %v",
				test.s, test.substrings, result, test.expected)
		}
	}
}

func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = New(ErrValidationFailed, "Validation failed")
	}
}

func BenchmarkWrap(b *testing.B) {
	cause := fmt.Errorf("underlying error")
	for i := 0; i < b.N; i++ {
		_ = Wrap(ErrProcessingFailed, "Processing failed", cause)
	}
}

func BenchmarkToErrorResponse(b *testing.B) {
	err := New(ErrValidationFailed, "Validation failed").
		WithDetails(map[string]interface{}{"field": "sender"}).
		WithRequestID("req-123456")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.ToErrorResponse()
	}
}

func BenchmarkIsRetryable(b *testing.B) {
	err := &AMTPError{
		Code:  ErrHTTPRequestFailed,
		Cause: fmt.Errorf("timeout"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.IsRetryable()
	}
}

func BenchmarkGetHTTPStatus(b *testing.B) {
	err := &AMTPError{Code: ErrValidationFailed}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.GetHTTPStatus()
	}
}
