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

package server

import (
	"time"

	"github.com/amtp-protocol/agentry/internal/errors"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/gin-gonic/gin"
)

// respondWithError sends a standardized error response
func (s *Server) respondWithError(c *gin.Context, statusCode int, code, message string, details map[string]interface{}) {
	requestID := c.GetString("request_id")

	errorResponse := types.ErrorResponse{
		Error: types.ErrorDetail{
			Code:      code,
			Message:   message,
			Details:   details,
			Timestamp: time.Now().UTC(),
			RequestID: requestID,
		},
	}

	// Log the error
	logger := s.logger.WithContext(c.Request.Context()).WithFields(map[string]interface{}{
		"status_code": statusCode,
		"error_code":  code,
		"method":      c.Request.Method,
		"path":        c.Request.URL.Path,
		"remote_addr": c.ClientIP(),
	})

	if statusCode >= 500 {
		logger.Error(message, nil)
	} else {
		logger.Warn(message)
	}

	// Record error metrics
	if s.metrics != nil {
		s.metrics.RecordError("server", code, getErrorType(statusCode))
	}

	c.JSON(statusCode, errorResponse)
}

// respondWithAMTPError sends an error response from an AMTPError
func (s *Server) respondWithAMTPError(c *gin.Context, err *errors.AMTPError) {
	requestID := c.GetString("request_id")
	err.RequestID = requestID

	statusCode := err.GetHTTPStatus()
	errorResponse := err.ToErrorResponse()

	// Log the error
	logger := s.logger.WithContext(c.Request.Context()).WithFields(map[string]interface{}{
		"status_code": statusCode,
		"error_code":  err.Code,
		"method":      c.Request.Method,
		"path":        c.Request.URL.Path,
		"remote_addr": c.ClientIP(),
	})

	if statusCode >= 500 {
		logger.Error(err.Message, err.Cause)
	} else {
		logger.Warn(err.Message)
	}

	// Record error metrics
	if s.metrics != nil {
		s.metrics.RecordError("server", string(err.Code), getErrorType(statusCode))
	}

	c.JSON(statusCode, errorResponse)
}

// getErrorType categorizes errors by HTTP status code
func getErrorType(statusCode int) string {
	switch {
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	case statusCode >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

// respondWithSuccess sends a successful response with metrics
func (s *Server) respondWithSuccess(c *gin.Context, statusCode int, data interface{}) {
	// Record success metrics
	if s.metrics != nil {
		s.metrics.RecordHTTPRequest(
			c.Request.Method,
			c.FullPath(),
			statusCode,
			time.Since(c.GetTime("start_time")),
		)
	}

	c.JSON(statusCode, data)
}

// withRequestMetrics wraps a handler with request metrics
func (s *Server) withRequestMetrics(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Set("start_time", start)

		// Increment in-flight requests (if metrics enabled)
		if s.metrics != nil {
			s.metrics.IncHTTPRequestsInFlight()
			defer s.metrics.DecHTTPRequestsInFlight()
		}

		// Process request
		handler(c)

		// Record metrics (if metrics enabled)
		duration := time.Since(start)
		if s.metrics != nil {
			s.metrics.RecordHTTPRequest(
				c.Request.Method,
				c.FullPath(),
				c.Writer.Status(),
				duration,
			)
		}

		// Log request
		s.logger.LogRequest(
			c.Request.Method,
			c.Request.URL.Path,
			c.ClientIP(),
			c.Request.UserAgent(),
			c.Writer.Status(),
			duration,
		)
	}
}
