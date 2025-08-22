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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
