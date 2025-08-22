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
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// ErrorCode represents standardized error codes
type ErrorCode string

const (
	// Request validation errors
	ErrInvalidRequestFormat    ErrorCode = "INVALID_REQUEST_FORMAT"
	ErrValidationFailed        ErrorCode = "VALIDATION_FAILED"
	ErrMessageValidationFailed ErrorCode = "MESSAGE_VALIDATION_FAILED"
	ErrInvalidMessageID        ErrorCode = "INVALID_MESSAGE_ID"
	ErrInvalidRecipient        ErrorCode = "INVALID_RECIPIENT"
	ErrMessageTooLarge         ErrorCode = "MESSAGE_TOO_LARGE"

	// Processing errors
	ErrProcessingFailed        ErrorCode = "PROCESSING_FAILED"
	ErrIDGenerationFailed      ErrorCode = "ID_GENERATION_FAILED"
	ErrPayloadMarshalFailed    ErrorCode = "PAYLOAD_MARSHAL_FAILED"
	ErrUnsupportedCoordination ErrorCode = "UNSUPPORTED_COORDINATION"

	// Discovery errors
	ErrDiscoveryFailed    ErrorCode = "DISCOVERY_FAILED"
	ErrInvalidGateway     ErrorCode = "INVALID_GATEWAY"
	ErrSchemaCheckFailed  ErrorCode = "SCHEMA_CHECK_FAILED"
	ErrSchemaNotSupported ErrorCode = "SCHEMA_NOT_SUPPORTED"

	// Delivery errors
	ErrDeliveryFailed        ErrorCode = "DELIVERY_FAILED"
	ErrHTTPRequestFailed     ErrorCode = "HTTP_REQUEST_FAILED"
	ErrRequestCreationFailed ErrorCode = "REQUEST_CREATION_FAILED"
	ErrResponseReadFailed    ErrorCode = "RESPONSE_READ_FAILED"
	ErrClientError           ErrorCode = "CLIENT_ERROR"
	ErrServerError           ErrorCode = "SERVER_ERROR"
	ErrUnexpectedStatus      ErrorCode = "UNEXPECTED_STATUS"

	// Resource errors
	ErrMessageNotFound  ErrorCode = "MESSAGE_NOT_FOUND"
	ErrStatusNotFound   ErrorCode = "STATUS_NOT_FOUND"
	ErrContextCancelled ErrorCode = "CONTEXT_CANCELLED"

	// Authentication and authorization errors
	ErrUnauthorized       ErrorCode = "UNAUTHORIZED"
	ErrForbidden          ErrorCode = "FORBIDDEN"
	ErrInvalidCredentials ErrorCode = "INVALID_CREDENTIALS"
	ErrTokenExpired       ErrorCode = "TOKEN_EXPIRED"

	// Rate limiting errors
	ErrRateLimitExceeded ErrorCode = "RATE_LIMIT_EXCEEDED"
	ErrQuotaExceeded     ErrorCode = "QUOTA_EXCEEDED"

	// System errors
	ErrInternalError      ErrorCode = "INTERNAL_ERROR"
	ErrServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	ErrTimeout            ErrorCode = "TIMEOUT"
	ErrMaintenanceMode    ErrorCode = "MAINTENANCE_MODE"
)

// AMTPError represents a structured AMTP error
type AMTPError struct {
	Code      ErrorCode              `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id,omitempty"`
	Cause     error                  `json:"-"` // Internal cause, not exposed in JSON
}

// Error implements the error interface
func (e *AMTPError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause error
func (e *AMTPError) Unwrap() error {
	return e.Cause
}

// ToErrorResponse converts AMTPError to types.ErrorResponse
func (e *AMTPError) ToErrorResponse() types.ErrorResponse {
	return types.ErrorResponse{
		Error: types.ErrorDetail{
			Code:      string(e.Code),
			Message:   e.Message,
			Details:   e.Details,
			Timestamp: e.Timestamp,
			RequestID: e.RequestID,
		},
	}
}

// New creates a new AMTPError
func New(code ErrorCode, message string) *AMTPError {
	return &AMTPError{
		Code:      code,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}
}

// Newf creates a new AMTPError with formatted message
func Newf(code ErrorCode, format string, args ...interface{}) *AMTPError {
	return &AMTPError{
		Code:      code,
		Message:   fmt.Sprintf(format, args...),
		Timestamp: time.Now().UTC(),
	}
}

// Wrap creates a new AMTPError wrapping an existing error
func Wrap(code ErrorCode, message string, cause error) *AMTPError {
	return &AMTPError{
		Code:      code,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now().UTC(),
	}
}

// Wrapf creates a new AMTPError wrapping an existing error with formatted message
func Wrapf(code ErrorCode, cause error, format string, args ...interface{}) *AMTPError {
	return &AMTPError{
		Code:      code,
		Message:   fmt.Sprintf(format, args...),
		Cause:     cause,
		Timestamp: time.Now().UTC(),
	}
}

// WithDetails adds details to an AMTPError
func (e *AMTPError) WithDetails(details map[string]interface{}) *AMTPError {
	e.Details = details
	return e
}

// WithRequestID adds a request ID to an AMTPError
func (e *AMTPError) WithRequestID(requestID string) *AMTPError {
	e.RequestID = requestID
	return e
}

// IsRetryable determines if an error is retryable
func (e *AMTPError) IsRetryable() bool {
	switch e.Code {
	case ErrServerError, ErrTimeout, ErrServiceUnavailable:
		return true
	case ErrRateLimitExceeded:
		return true
	case ErrHTTPRequestFailed:
		// Check if it's a network-related error
		if e.Cause != nil {
			causeStr := e.Cause.Error()
			return containsAny(causeStr, []string{
				"timeout", "connection refused", "no such host",
				"network unreachable", "connection reset",
			})
		}
		return false
	default:
		return false
	}
}

// GetHTTPStatus returns the appropriate HTTP status code for the error
func (e *AMTPError) GetHTTPStatus() int {
	switch e.Code {
	case ErrInvalidRequestFormat, ErrValidationFailed, ErrMessageValidationFailed,
		ErrInvalidMessageID, ErrInvalidRecipient, ErrMessageTooLarge:
		return 400 // Bad Request

	case ErrUnauthorized, ErrInvalidCredentials, ErrTokenExpired:
		return 401 // Unauthorized

	case ErrForbidden:
		return 403 // Forbidden

	case ErrMessageNotFound, ErrStatusNotFound:
		return 404 // Not Found

	case ErrRateLimitExceeded, ErrQuotaExceeded:
		return 429 // Too Many Requests

	case ErrInternalError, ErrProcessingFailed, ErrIDGenerationFailed:
		return 500 // Internal Server Error

	case ErrServiceUnavailable, ErrMaintenanceMode:
		return 503 // Service Unavailable

	case ErrTimeout:
		return 504 // Gateway Timeout

	default:
		return 500 // Default to Internal Server Error
	}
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// Common error constructors for convenience

// NewValidationError creates a validation error
func NewValidationError(message string, details map[string]interface{}) *AMTPError {
	return New(ErrValidationFailed, message).WithDetails(details)
}

// NewProcessingError creates a processing error
func NewProcessingError(message string, cause error) *AMTPError {
	return Wrap(ErrProcessingFailed, message, cause)
}

// NewDeliveryError creates a delivery error
func NewDeliveryError(message string, cause error) *AMTPError {
	return Wrap(ErrDeliveryFailed, message, cause)
}

// NewDiscoveryError creates a discovery error
func NewDiscoveryError(message string, cause error) *AMTPError {
	return Wrap(ErrDiscoveryFailed, message, cause)
}

// NewNotFoundError creates a not found error
func NewNotFoundError(resource string) *AMTPError {
	return Newf(ErrMessageNotFound, "%s not found", resource)
}

// NewInternalError creates an internal error
func NewInternalError(message string, cause error) *AMTPError {
	return Wrap(ErrInternalError, message, cause)
}

// ErrorFromCode creates an error from a code and message
func ErrorFromCode(code string, message string) *AMTPError {
	return New(ErrorCode(code), message)
}

// IsAMTPError checks if an error is an AMTPError
func IsAMTPError(err error) bool {
	_, ok := err.(*AMTPError)
	return ok
}

// AsAMTPError converts an error to AMTPError if possible
func AsAMTPError(err error) (*AMTPError, bool) {
	if amtpErr, ok := err.(*AMTPError); ok {
		return amtpErr, true
	}
	return nil, false
}
