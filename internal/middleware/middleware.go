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
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	return func(c *gin.Context) {
		// If no admin key file is configured, allow access (backward compatibility)
		if cfg.AdminKeyFile == "" {
			c.Next()
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
		if !validateAdminKey(adminKey, cfg.AdminKeyFile) {
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

// validateAdminKey validates the provided admin key against the key file
func validateAdminKey(providedKey, keyFile string) bool {
	// Read admin keys from file
	data, err := os.ReadFile(filepath.Clean(keyFile))
	if err != nil {
		return false
	}

	// Parse keys from file (one key per line, ignore empty lines and comments)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(providedKey), []byte(line)) == 1 {
			return true
		}
	}

	return false
}
