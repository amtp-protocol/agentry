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

package middleware

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/gin-gonic/gin"
)

func TestAdminAuth_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create config with admin auth disabled
	cfg := config.AuthConfig{
		RequireAuth:       false, // Admin auth disabled
		AdminKeyFile:      "",
		AdminAPIKeyHeader: "X-Admin-Key",
	}

	// Create test router
	router := gin.New()
	router.Use(AdminAuth(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test request without any auth header
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should pass when auth is disabled
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d when auth disabled, got %d", http.StatusOK, w.Code)
	}
}

func TestAdminAuth_NoKeyFile_BackwardCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create config with no admin key file (backward compatibility mode)
	cfg := config.AuthConfig{
		RequireAuth:       true, // This doesn't matter for admin auth
		AdminKeyFile:      "",   // No key file - should allow access
		AdminAPIKeyHeader: "X-Admin-Key",
	}

	// Create test router
	router := gin.New()
	router.Use(AdminAuth(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	tests := []struct {
		name           string
		adminKey       string
		expectedStatus int
	}{
		{
			name:           "no admin key - should pass (backward compatibility)",
			adminKey:       "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "any admin key - should pass (backward compatibility)",
			adminKey:       "any-key",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.adminKey != "" {
				req.Header.Set("X-Admin-Key", tt.adminKey)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestAdminAuth_WithKeyFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temporary admin keys file
	tempDir, err := os.MkdirTemp("", "admin_auth_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	adminKeysFile := filepath.Join(tempDir, "admin.keys")
	adminKeysContent := `# Admin keys for testing
test-admin-key-1
test-admin-key-2
# Another key
test-admin-key-3`

	err = os.WriteFile(adminKeysFile, []byte(adminKeysContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write admin keys file: %v", err)
	}

	// Create config with admin auth enabled and key file
	cfg := config.AuthConfig{
		RequireAuth:       true,
		AdminKeyFile:      adminKeysFile,
		AdminAPIKeyHeader: "X-Admin-Key",
	}

	// Create test router
	router := gin.New()
	router.Use(AdminAuth(cfg))
	router.GET("/admin/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "admin access granted"})
	})

	tests := []struct {
		name           string
		adminKey       string
		expectedStatus int
	}{
		{
			name:           "valid admin key 1",
			adminKey:       "test-admin-key-1",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid admin key 2",
			adminKey:       "test-admin-key-2",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid admin key 3",
			adminKey:       "test-admin-key-3",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "no admin key",
			adminKey:       "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid admin key",
			adminKey:       "invalid-key",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/test", nil)
			if tt.adminKey != "" {
				req.Header.Set("X-Admin-Key", tt.adminKey)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// Test the validateAdminKey function directly (if it were exported)
// Since it's not exported, we'll test it indirectly through the middleware
func TestAdminAuth_KeyValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a simple test to verify that the middleware correctly validates keys
	// This is more of an integration test since we can't test the internal function directly

	// Create temporary admin keys file
	tempDir, err := os.MkdirTemp("", "key_validation_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	adminKeysFile := filepath.Join(tempDir, "admin.keys")
	adminKeysContent := `admin-key-123
another-valid-key`

	err = os.WriteFile(adminKeysFile, []byte(adminKeysContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write admin keys file: %v", err)
	}

	// Create config
	cfg := config.AuthConfig{
		RequireAuth:       true,
		AdminKeyFile:      adminKeysFile,
		AdminAPIKeyHeader: "X-Admin-Key",
	}

	// Create test router
	router := gin.New()
	router.Use(AdminAuth(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test case-sensitive comparison
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Admin-Key", "Admin-Key-123") // Different case
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should fail due to case sensitivity
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for case-sensitive key comparison, got %d", http.StatusForbidden, w.Code)
	}

	// Test similar but different key
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Admin-Key", "admin-key-12X") // Changed last character
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should fail for similar but different key
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for similar but different key, got %d", http.StatusForbidden, w.Code)
	}
}

// Test custom admin key header
func TestAdminAuth_CustomHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temporary admin keys file
	tempDir, err := os.MkdirTemp("", "custom_header_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	adminKeysFile := filepath.Join(tempDir, "admin.keys")
	err = os.WriteFile(adminKeysFile, []byte("test-key"), 0600)
	if err != nil {
		t.Fatalf("Failed to write admin keys file: %v", err)
	}

	// Create config with custom header and key file
	cfg := config.AuthConfig{
		RequireAuth:       true,
		AdminKeyFile:      adminKeysFile,
		AdminAPIKeyHeader: "X-Custom-Admin-Key",
	}

	// Create test router
	router := gin.New()
	router.Use(AdminAuth(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test with wrong header name
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Admin-Key", "test-key") // Wrong header name
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should fail because key is in wrong header
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d when using wrong header, got %d", http.StatusUnauthorized, w.Code)
	}

	// Test with correct header name and valid key
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-Admin-Key", "test-key") // Correct header name and valid key
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should succeed
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d when using correct header and valid key, got %d", http.StatusOK, w.Code)
	}

	// Test with correct header name but invalid key
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-Admin-Key", "invalid-key") // Correct header name but invalid key
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should fail because key is invalid
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d when using invalid key, got %d", http.StatusForbidden, w.Code)
	}
}

// Test Logger middleware
func TestLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		config config.LoggingConfig
	}{
		{
			name: "json format",
			config: config.LoggingConfig{
				Format: "json",
			},
		},
		{
			name: "default format",
			config: config.LoggingConfig{
				Format: "text",
			},
		},
		{
			name: "empty format (defaults to text)",
			config: config.LoggingConfig{
				Format: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(Logger(tt.config))
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "success"})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-Request-ID", "test-request-id")
			req.Header.Set("User-Agent", "test-agent")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}
		})
	}
}

// Test RequestID middleware
func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("existing request ID", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestID())
		router.GET("/test", func(c *gin.Context) {
			requestID := c.GetString("request_id")
			c.JSON(http.StatusOK, gin.H{"request_id": requestID})
		})

		existingID := "existing-request-id"
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Request-ID", existingID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		// Check that existing ID is preserved
		if w.Header().Get("X-Request-ID") != existingID {
			t.Errorf("Expected request ID %s, got %s", existingID, w.Header().Get("X-Request-ID"))
		}
	})

	t.Run("generate new request ID", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestID())
		router.GET("/test", func(c *gin.Context) {
			requestID := c.GetString("request_id")
			if requestID == "" {
				t.Error("Expected request_id to be set in context")
			}
			c.JSON(http.StatusOK, gin.H{"request_id": requestID})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		// Check that a new ID was generated
		requestID := w.Header().Get("X-Request-ID")
		if requestID == "" {
			t.Error("Expected X-Request-ID header to be set")
		}
	})
}

// Test CORS middleware
func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("preflight request", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.OPTIONS("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "should not reach here"})
		})

		req := httptest.NewRequest("OPTIONS", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d for OPTIONS request, got %d", http.StatusNoContent, w.Code)
		}

		// Check CORS headers
		expectedHeaders := map[string]string{
			"Access-Control-Allow-Origin":   "https://example.com",
			"Access-Control-Allow-Methods":  "GET, POST, OPTIONS",
			"Access-Control-Allow-Headers":  "Content-Type, Authorization, X-Request-ID, X-AMTP-Version, X-Admin-Key",
			"Access-Control-Expose-Headers": "X-Request-ID",
			"Access-Control-Max-Age":        "86400",
		}

		for header, expected := range expectedHeaders {
			if got := w.Header().Get(header); got != expected {
				t.Errorf("Expected %s header to be %s, got %s", header, expected, got)
			}
		}
	})

	t.Run("regular request", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		// Check that CORS headers are still set
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
			t.Errorf("Expected Access-Control-Allow-Origin to be https://example.com, got %s", got)
		}
	})
}

// Test SecurityHeaders middleware
func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("HTTP request", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		expectedHeaders := map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"X-XSS-Protection":       "1; mode=block",
			"Referrer-Policy":        "strict-origin-when-cross-origin",
		}

		for header, expected := range expectedHeaders {
			if got := w.Header().Get(header); got != expected {
				t.Errorf("Expected %s header to be %s, got %s", header, expected, got)
			}
		}

		// HSTS header should not be set for HTTP
		if got := w.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Expected Strict-Transport-Security header to be empty for HTTP, got %s", got)
		}
	})

	t.Run("HTTPS request", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.TLS = &tls.ConnectionState{} // Simulate HTTPS
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		// HSTS header should be set for HTTPS
		expectedHSTS := "max-age=31536000; includeSubDomains"
		if got := w.Header().Get("Strict-Transport-Security"); got != expectedHSTS {
			t.Errorf("Expected Strict-Transport-Security header to be %s, got %s", expectedHSTS, got)
		}
	})
}

// Test RequestSizeLimit middleware
func TestRequestSizeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("request within limit", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestSizeLimit(1024)) // 1KB limit
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		body := strings.NewReader("small body")
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("request exceeds limit", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestSizeLimit(10)) // 10 bytes limit
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "should not reach here"})
		})

		body := strings.NewReader("this body is definitely longer than 10 bytes")
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("Expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
		}

		// Check error response
		if !strings.Contains(w.Body.String(), "PAYLOAD_TOO_LARGE") {
			t.Error("Expected error response to contain PAYLOAD_TOO_LARGE")
		}
	})

	t.Run("request without content-length", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestSizeLimit(1024))
		router.POST("/test", func(c *gin.Context) {
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"body_length": len(body)})
		})

		body := strings.NewReader("test body")
		req := httptest.NewRequest("POST", "/test", body)
		req.ContentLength = -1 // Simulate unknown content length
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})
}

// Test Auth middleware
func TestAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("auth disabled", func(t *testing.T) {
		cfg := config.AuthConfig{
			RequireAuth: false,
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d when auth disabled, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("apikey method", func(t *testing.T) {
		cfg := config.AuthConfig{
			RequireAuth: true,
			Methods:     []string{"apikey"},
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			authMethod := c.GetString("auth_method")
			authenticated := c.GetBool("authenticated")
			c.JSON(http.StatusOK, gin.H{
				"auth_method":   authMethod,
				"authenticated": authenticated,
			})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for apikey method, got %d", http.StatusOK, w.Code)
		}

		// Check that auth context is set
		if !strings.Contains(w.Body.String(), `"auth_method":"apikey"`) {
			t.Error("Expected auth_method to be set to apikey")
		}
	})

	t.Run("domain method with TLS", func(t *testing.T) {
		// NOTE: This test uses placeholder validateClientCertificate that always returns true
		cfg := config.AuthConfig{
			RequireAuth: true,
			Methods:     []string{"domain"},
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			authMethod := c.GetString("auth_method")
			authenticated := c.GetBool("authenticated")
			c.JSON(http.StatusOK, gin.H{
				"auth_method":   authMethod,
				"authenticated": authenticated,
			})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		// Simulate TLS with client certificate
		req.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{{}}, // Mock certificate
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for domain method with TLS, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("domain method without TLS", func(t *testing.T) {
		cfg := config.AuthConfig{
			RequireAuth: true,
			Methods:     []string{"domain"},
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "should not reach here"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		// No TLS
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for domain method without TLS, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("oauth method with valid token", func(t *testing.T) {
		// NOTE: This test uses placeholder validateBearerToken that only checks len(token) > 0
		cfg := config.AuthConfig{
			RequireAuth: true,
			Methods:     []string{"oauth"},
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			authMethod := c.GetString("auth_method")
			authenticated := c.GetBool("authenticated")
			c.JSON(http.StatusOK, gin.H{
				"auth_method":   authMethod,
				"authenticated": authenticated,
			})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for oauth method with valid token, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("oauth method with invalid token", func(t *testing.T) {
		// NOTE: This test uses placeholder validateBearerToken that only checks len(token) > 0
		cfg := config.AuthConfig{
			RequireAuth: true,
			Methods:     []string{"oauth"},
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "should not reach here"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer ") // Empty token (len = 0)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for oauth method with invalid token, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("no valid authentication", func(t *testing.T) {
		cfg := config.AuthConfig{
			RequireAuth: true,
			Methods:     []string{"oauth"},
		}

		router := gin.New()
		router.Use(Auth(cfg))
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "should not reach here"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for no valid authentication, got %d", http.StatusUnauthorized, w.Code)
		}

		if !strings.Contains(w.Body.String(), "AUTHENTICATION_REQUIRED") {
			t.Error("Expected error response to contain AUTHENTICATION_REQUIRED")
		}
	})
}

// Test RateLimit middleware
func TestRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// NOTE: This test uses placeholder isRateLimited that always returns false
	// TODO: Replace with proper tests when real rate limiting is implemented

	router := gin.New()
	router.Use(RateLimit())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Since isRateLimited always returns false in the placeholder implementation,
	// the request should always succeed
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// Test AMTPVersion middleware
func TestAMTPVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("no version header", func(t *testing.T) {
		router := gin.New()
		router.Use(AMTPVersion())
		router.GET("/test", func(c *gin.Context) {
			version := c.GetString("amtp_version")
			c.JSON(http.StatusOK, gin.H{"amtp_version": version})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d when no version header, got %d", http.StatusOK, w.Code)
		}

		if !strings.Contains(w.Body.String(), `"amtp_version":"1.0"`) {
			t.Error("Expected amtp_version to be set to 1.0")
		}
	})

	t.Run("supported version", func(t *testing.T) {
		router := gin.New()
		router.Use(AMTPVersion())
		router.GET("/test", func(c *gin.Context) {
			version := c.GetString("amtp_version")
			c.JSON(http.StatusOK, gin.H{"amtp_version": version})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-AMTP-Version", "1.0")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for supported version, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		router := gin.New()
		router.Use(AMTPVersion())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "should not reach here"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-AMTP-Version", "2.0")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d for unsupported version, got %d", http.StatusBadRequest, w.Code)
		}

		if !strings.Contains(w.Body.String(), "UNSUPPORTED_VERSION") {
			t.Error("Expected error response to contain UNSUPPORTED_VERSION")
		}
	})
}

// Test helper functions
func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "item exists",
			slice:    []string{"a", "b", "c"},
			item:     "b",
			expected: true,
		},
		{
			name:     "item does not exist",
			slice:    []string{"a", "b", "c"},
			item:     "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "a",
			expected: false,
		},
		{
			name:     "case sensitive",
			slice:    []string{"A", "B", "C"},
			item:     "a",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.slice, tt.item)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestValidateClientCertificate(t *testing.T) {
	// NOTE: This is testing a placeholder implementation that always returns true
	// TODO: Replace with proper tests when real certificate validation is implemented
	t.Skip("Skipping test for placeholder implementation - validateClientCertificate always returns true")
}

func TestValidateBearerToken(t *testing.T) {
	// NOTE: This is testing a placeholder implementation that only checks len(token) > 0
	// TODO: Replace with proper tests when real JWT/OAuth token validation is implemented
	t.Skip("Skipping test for placeholder implementation - validateBearerToken only checks token length")
}

func TestIsRateLimited(t *testing.T) {
	// NOTE: This is testing a placeholder implementation that always returns false
	// TODO: Replace with proper tests when real rate limiting logic is implemented
	t.Skip("Skipping test for placeholder implementation - isRateLimited always returns false")
}

func TestValidateAdminKey(t *testing.T) {
	// Create temporary admin keys file
	tempDir, err := os.MkdirTemp("", "validate_admin_key_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	adminKeysFile := filepath.Join(tempDir, "admin.keys")
	adminKeysContent := `# Admin keys for testing
key1
key2
# Comment line
key3

# Empty line above
key4`

	err = os.WriteFile(adminKeysFile, []byte(adminKeysContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write admin keys file: %v", err)
	}

	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{
			name:     "valid key 1",
			key:      "key1",
			expected: true,
		},
		{
			name:     "valid key 2",
			key:      "key2",
			expected: true,
		},
		{
			name:     "valid key 3",
			key:      "key3",
			expected: true,
		},
		{
			name:     "valid key 4",
			key:      "key4",
			expected: true,
		},
		{
			name:     "invalid key",
			key:      "invalid",
			expected: false,
		},
		{
			name:     "empty key",
			key:      "",
			expected: false,
		},
		{
			name:     "comment as key",
			key:      "# Admin keys for testing",
			expected: false,
		},
		{
			name:     "case sensitive",
			key:      "Key1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateAdminKey(tt.key, adminKeysFile)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}

	// Test with non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		result := validateAdminKey("any-key", "/non/existent/file")
		if result {
			t.Error("Expected validateAdminKey to return false for non-existent file")
		}
	})
}
