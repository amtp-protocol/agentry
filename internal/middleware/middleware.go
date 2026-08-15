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
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/amtp-protocol/agentry/internal/config"
)

// Logger creates a structured logging middleware
func Logger(cfg config.LoggingConfig) gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		if cfg.Format == "json" {
			return fmt.Sprintf(`{"time":"%s","method":"%s","path":"%s","status":%d,"latency":"%s","ip":"%s","user_agent":"%s","request_id":"%s"}%s`,
				param.TimeStamp.Format(time.RFC3339),
				param.Method,
				param.Path,
				param.StatusCode,
				param.Latency,
				param.ClientIP,
				param.Request.UserAgent(),
				param.Request.Header.Get("X-Request-ID"),
				"\n",
			)
		}

		// Default format
		return fmt.Sprintf("[%s] %s %s %d %s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
		)
	})
}

// RequestID adds a unique request ID to each request
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	}
}

// CORS adds CORS headers
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// For AMTP, we're more restrictive with CORS
		// Only allow specific origins or use a whitelist
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, X-AMTP-Version, X-Admin-Key")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// SecurityHeaders adds security-related headers
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Security headers
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// HSTS header for HTTPS
		if c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}

// RequestSizeLimit limits the size of incoming requests
func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxSize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": gin.H{
					"code":    "PAYLOAD_TOO_LARGE",
					"message": fmt.Sprintf("Request body too large. Maximum size is %d bytes", maxSize),
				},
			})
			c.Abort()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()
	}
}

// Auth provides authentication middleware
func Auth(cfg config.AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.RequireAuth {
			c.Next()
			return
		}

		// NOTE: For agent-specific API key validation, use the registry directly in handlers
		// This middleware only handles general authentication methods like domain/oauth
		if contains(cfg.Methods, "apikey") {
			// API key validation is handled per-endpoint in handlers
			// where the specific agent context is known
			c.Set("auth_method", "apikey")
			c.Set("authenticated", true)
			c.Next()
			return
		}

		// Check for domain-based authentication via TLS client certificates
		if contains(cfg.Methods, "domain") && c.Request.TLS != nil {
			if len(c.Request.TLS.PeerCertificates) > 0 {
				// Validate client certificate (placeholder)
				if validateClientCertificate(c.Request.TLS.PeerCertificates[0]) {
					c.Set("auth_method", "domain")
					c.Set("authenticated", true)
					c.Next()
					return
				}
			}
		}

		// Check for Bearer token (OAuth, JWT, etc.)
		if contains(cfg.Methods, "oauth") {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				if validateBearerToken(token) {
					c.Set("auth_method", "oauth")
					c.Set("authenticated", true)
					c.Next()
					return
				}
			}
		}

		// No valid authentication found
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"code":    "AUTHENTICATION_REQUIRED",
				"message": "Valid authentication is required",
			},
		})
		c.Abort()
	}
}

// AdminAuth provides admin authentication middleware for administrative operations
func AdminAuth(cfg config.AuthConfig) gin.HandlerFunc {
	// Parse the key file once and cache it: the admin control plane is
	// low-traffic, but each request still skips the filesystem read.
	validator := NewAdminKeyValidator(cfg.AdminKeyFile)
	return func(c *gin.Context) {
		// Fail closed when no admin key file is configured. The admin plane
		// registers agents, rotates their keys and hands the plaintext back,
		// so an unconfigured gateway must not serve it to anonymous callers.
		// This matches the server's own admin-identity check, which likewise
		// grants nothing without a configured key file.
		if cfg.AdminKeyFile == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "ADMIN_AUTH_NOT_CONFIGURED",
					"message": "Administrative operations are unavailable: no admin key file is configured",
					"details": gin.H{
						"required_config": "auth.admin_key_file (AMTP_ADMIN_KEY_FILE)",
						"endpoint":        c.Request.URL.Path,
					},
				},
			})
			c.Abort()
			return
		}

		// Get admin API key from header
		adminKey := c.GetHeader(cfg.AdminAPIKeyHeader)
		if adminKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"code":    "ADMIN_AUTHENTICATION_REQUIRED",
					"message": "Admin API key required for administrative operations",
					"details": gin.H{
						"required_header": cfg.AdminAPIKeyHeader,
						"endpoint":        c.Request.URL.Path,
					},
				},
			})
			c.Abort()
			return
		}

		// Validate admin key against file
		if !validator.Validate(adminKey) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "ADMIN_ACCESS_DENIED",
					"message": "Invalid admin API key",
					"details": gin.H{
						"endpoint": c.Request.URL.Path,
					},
				},
			})
			c.Abort()
			return
		}

		// Set admin authentication context
		c.Set("admin_authenticated", true)
		c.Set("auth_method", "admin_key")
		c.Next()
	}
}

// RateLimit provides basic rate limiting (placeholder implementation)
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Placeholder for rate limiting implementation
		// In production, this would use Redis or similar for distributed rate limiting

		clientIP := c.ClientIP()

		// Simple in-memory rate limiting (not suitable for production)
		if isRateLimited(clientIP) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"code":    "RATE_LIMIT_EXCEEDED",
					"message": "Too many requests. Please try again later.",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AMTPVersion validates the AMTP protocol version
func AMTPVersion() gin.HandlerFunc {
	return func(c *gin.Context) {
		version := c.GetHeader("X-AMTP-Version")
		if version != "" && version != "1.0" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":    "UNSUPPORTED_VERSION",
					"message": fmt.Sprintf("Unsupported AMTP version: %s", version),
				},
			})
			c.Abort()
			return
		}

		c.Set("amtp_version", "1.0")
		c.Next()
	}
}

// Helper functions (placeholders for actual implementations)

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func validateClientCertificate(cert interface{}) bool {
	// TODO: Implement proper client certificate validation
	// This should verify the certificate chain, check revocation status,
	// validate certificate fields, and ensure it's from a trusted CA
	return true
}

func validateBearerToken(token string) bool {
	// TODO: Implement proper bearer token validation
	// This should validate JWT tokens, OAuth tokens, etc.
	// For JWT: verify signature, check expiration, validate claims
	// For OAuth: validate against authorization server
	return len(token) > 0
}

func isRateLimited(clientIP string) bool {
	// TODO: Implement proper rate limiting logic
	// This should use Redis for distributed rate limiting, or
	// an in-memory cache with TTL for single-instance deployments
	// Consider implementing sliding window or token bucket algorithms
	return false
}

// ValidateAdminKey validates the provided admin key against the key file.
// It is exported so the server layer can grant the same admin identity on
// message query endpoints (get/status/list), where the admin middleware is
// not installed, keeping admin-key semantics consistent across the API.
//
// This one-shot form reads the file on every call; long-lived callers on the
// request path should use NewAdminKeyValidator, which caches the parsed key
// set and re-reads it when the file changes or the cache ages out.
func ValidateAdminKey(providedKey, keyFile string) bool {
	return NewAdminKeyValidator(keyFile).Validate(providedKey)
}

// adminKeyCacheTTL bounds how long a parsed key file may be served from
// cache. mtime and size alone cannot detect every rewrite — a restore from
// backup, cp -p or rsync --times can reproduce both — so the entry ages out
// and forces a re-read, capping how long a revoked key keeps working.
const adminKeyCacheTTL = 5 * time.Second

// AdminKeyValidator validates admin keys against a key file, caching the
// parsed key set and re-reading the file when its mtime or size changes or
// the cache entry ages out. The admin key check sits on the message query
// read path (GET /v1/messages, GET /v1/messages/:id,
// GET /v1/messages/:id/status), so a naive read-per-request turns agent
// polling into a blocking filesystem read per request — including for
// requests that fail agent-key auth. The cache makes the hot path a map
// lookup. Removing the file invalidates the cache: validation fails rather
// than serving stale keys.
type AdminKeyValidator struct {
	keyFile string
	ttl     time.Duration

	mu       sync.RWMutex
	cached   map[string]struct{}
	modTime  time.Time
	size     int64
	loadedAt time.Time
}

// NewAdminKeyValidator returns a validator for the given key file. The file
// is read lazily on first use and re-read when it changes or the cached key
// set ages out.
func NewAdminKeyValidator(keyFile string) *AdminKeyValidator {
	return &AdminKeyValidator{keyFile: filepath.Clean(keyFile), ttl: adminKeyCacheTTL}
}

// Validate reports whether the provided key is present in the key file. A
// missing or unreadable file yields false.
func (v *AdminKeyValidator) Validate(providedKey string) bool {
	if providedKey == "" {
		return false
	}
	keys, err := v.keys()
	if err != nil {
		return false
	}
	_, ok := keys[providedKey]
	return ok
}

// keys returns the parsed key set, refreshing the cache when the file
// changed since the previous read or the cached set aged out.
func (v *AdminKeyValidator) keys() (map[string]struct{}, error) {
	// Fast path: fresh cache entry over an unchanged file — no read.
	v.mu.RLock()
	if v.cacheUsable() {
		keys := v.cached
		v.mu.RUnlock()
		return keys, nil
	}
	v.mu.RUnlock()

	// Slow path: (re)load under the write lock. Re-check after acquiring the
	// lock so concurrent validators do not re-read a file another goroutine
	// just refreshed.
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cacheUsable() {
		return v.cached, nil
	}

	// Stat before reading, never after: a rewrite landing between the two
	// would otherwise pair the pre-write content with the post-write mtime
	// and size, and the resulting cache entry would look valid forever. With
	// this order the worst case is one redundant reload.
	st, err := os.Stat(v.keyFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(v.keyFile)
	if err != nil {
		return nil, err
	}
	v.cached = parseAdminKeys(data)
	v.size = st.Size()
	v.modTime = st.ModTime()
	v.loadedAt = time.Now()
	return v.cached, nil
}

// cacheUsable reports whether the cached key set may still be served: it
// must be loaded, within its TTL, and backed by a file whose mtime and size
// still match. A stat failure (file removed/unreadable) counts as changed so
// the next load attempt surfaces the error.
func (v *AdminKeyValidator) cacheUsable() bool {
	if v.cached == nil || time.Since(v.loadedAt) >= v.ttl {
		return false
	}
	st, err := os.Stat(v.keyFile)
	if err != nil {
		return false
	}
	return st.Size() == v.size && st.ModTime().Equal(v.modTime)
}

// parseAdminKeys extracts the keys from a key file: one key per line,
// ignoring empty lines and comments.
func parseAdminKeys(data []byte) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys[line] = struct{}{}
	}
	return keys
}
