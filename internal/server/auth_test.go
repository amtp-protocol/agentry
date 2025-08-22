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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Test authentication header parsing logic (unit test)
func TestAuthenticationHeaderParsing_Unit(t *testing.T) {
	tests := []struct {
		name           string
		authHeader     string
		expectedPrefix bool
		expectedKey    string
	}{
		{
			name:           "valid Bearer token",
			authHeader:     "Bearer abc123",
			expectedPrefix: true,
			expectedKey:    "abc123",
		},
		{
			name:           "Bearer with empty key",
			authHeader:     "Bearer ",
			expectedPrefix: true,
			expectedKey:    "",
		},
		{
			name:           "missing Bearer prefix",
			authHeader:     "abc123",
			expectedPrefix: false,
			expectedKey:    "",
		},
		{
			name:           "empty header",
			authHeader:     "",
			expectedPrefix: false,
			expectedKey:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasPrefix := strings.HasPrefix(tt.authHeader, "Bearer ")
			if hasPrefix != tt.expectedPrefix {
				t.Errorf("Expected HasPrefix %v, got %v", tt.expectedPrefix, hasPrefix)
			}

			var key string
			if hasPrefix {
				key = strings.TrimPrefix(tt.authHeader, "Bearer ")
			}

			if key != tt.expectedKey {
				t.Errorf("Expected key '%s', got '%s'", tt.expectedKey, key)
			}
		})
	}
}

// Test agent inbox authentication with mock endpoints
func TestInboxEndpoints_Authentication_Mock(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a simple router to test authentication middleware behavior
	router := gin.New()

	// Add a mock inbox endpoint that simulates the authentication check
	router.GET("/v1/inbox/:recipient", func(c *gin.Context) {
		recipient := c.Param("recipient")

		// Simulate the authentication logic
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "AUTHENTICATION_REQUIRED",
					"message": "Bearer token required",
				},
			})
			return
		}

		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "AUTHENTICATION_REQUIRED",
					"message": "API key required",
				},
			})
			return
		}

		// For testing, accept only "valid-key" for "test@localhost"
		if recipient == "test@localhost" && apiKey == "valid-key" {
			c.JSON(http.StatusOK, gin.H{"messages": []interface{}{}})
			return
		}

		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"code":    "ACCESS_DENIED",
				"message": "Invalid API key",
			},
		})
	})

	tests := []struct {
		name           string
		recipient      string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "valid API key",
			recipient:      "test@localhost",
			authHeader:     "Bearer valid-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid API key",
			recipient:      "test@localhost",
			authHeader:     "Bearer invalid-key",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "missing Bearer prefix",
			recipient:      "test@localhost",
			authHeader:     "valid-key",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "missing Authorization header",
			recipient:      "test@localhost",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "empty API key",
			recipient:      "test@localhost",
			authHeader:     "Bearer ",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/inbox/"+tt.recipient, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

// Test message acknowledgment authentication with mock endpoints
func TestAckEndpoints_Authentication_Mock(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a simple router to test authentication middleware behavior
	router := gin.New()

	// Add a mock ack endpoint that simulates the authentication check
	router.POST("/v1/inbox/:recipient/ack", func(c *gin.Context) {
		recipient := c.Param("recipient")

		// Simulate the authentication logic
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "AUTHENTICATION_REQUIRED",
					"message": "Bearer token required",
				},
			})
			return
		}

		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "AUTHENTICATION_REQUIRED",
					"message": "API key required",
				},
			})
			return
		}

		// For testing, accept only "valid-key" for "test@localhost"
		if recipient == "test@localhost" && apiKey == "valid-key" {
			c.JSON(http.StatusOK, gin.H{"success": true})
			return
		}

		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"code":    "ACCESS_DENIED",
				"message": "Invalid API key",
			},
		})
	})

	ackRequest := map[string]string{
		"message_id": "test-message-123",
	}
	ackBody, _ := json.Marshal(ackRequest)

	tests := []struct {
		name           string
		recipient      string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "valid API key",
			recipient:      "test@localhost",
			authHeader:     "Bearer valid-key",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid API key",
			recipient:      "test@localhost",
			authHeader:     "Bearer invalid-key",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "missing Authorization header",
			recipient:      "test@localhost",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/inbox/"+tt.recipient+"/ack", bytes.NewReader(ackBody))
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

// Test authentication header parsing logic
func TestAuthenticationHeaderParsing(t *testing.T) {
	tests := []struct {
		name           string
		authHeader     string
		expectedPrefix bool
		expectedKey    string
	}{
		{
			name:           "valid Bearer token",
			authHeader:     "Bearer abc123",
			expectedPrefix: true,
			expectedKey:    "abc123",
		},
		{
			name:           "Bearer with empty key",
			authHeader:     "Bearer ",
			expectedPrefix: true,
			expectedKey:    "",
		},
		{
			name:           "Bearer with whitespace key",
			authHeader:     "Bearer   key123   ",
			expectedPrefix: true,
			expectedKey:    "  key123   ", // TrimPrefix doesn't trim whitespace
		},
		{
			name:           "missing Bearer prefix",
			authHeader:     "abc123",
			expectedPrefix: false,
			expectedKey:    "",
		},
		{
			name:           "wrong prefix",
			authHeader:     "Basic abc123",
			expectedPrefix: false,
			expectedKey:    "",
		},
		{
			name:           "empty header",
			authHeader:     "",
			expectedPrefix: false,
			expectedKey:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasPrefix := strings.HasPrefix(tt.authHeader, "Bearer ")
			if hasPrefix != tt.expectedPrefix {
				t.Errorf("Expected HasPrefix %v, got %v", tt.expectedPrefix, hasPrefix)
			}

			var key string
			if hasPrefix {
				key = strings.TrimPrefix(tt.authHeader, "Bearer ")
			}

			if key != tt.expectedKey {
				t.Errorf("Expected key '%s', got '%s'", tt.expectedKey, key)
			}
		})
	}
}
