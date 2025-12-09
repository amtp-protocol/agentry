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
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/logging"
	"github.com/amtp-protocol/agentry/internal/metrics"
	"github.com/amtp-protocol/agentry/internal/middleware"
	"github.com/amtp-protocol/agentry/internal/processing"
	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/validation"
)

// AgentManagerAdapter adapts agents.Registry to validation.AgentManager
type AgentManagerAdapter struct {
	agentRegistry agents.AgentRegistry
}

func (a *AgentManagerAdapter) GetLocalAgents() map[string]*validation.LocalAgent {
	processingAgents := a.agentRegistry.GetAllAgents(context.Background())
	validationAgents := make(map[string]*validation.LocalAgent)

	for address, agent := range processingAgents {
		validationAgents[address] = &validation.LocalAgent{
			Address:          agent.Address,
			SupportedSchemas: agent.SupportedSchemas,
			RequiresSchema:   agent.RequiresSchema,
		}
	}

	return validationAgents
}

// Server represents the AMTP HTTP server
type Server struct {
	config        *config.Config
	httpServer    *http.Server
	router        *gin.Engine
	discovery     processing.DiscoveryService
	validator     *validation.Validator
	processor     processing.MessageProcessorService
	storage       storage.Storage
	agentRegistry agents.AgentRegistry
	schemaManager *schema.Manager
	logger        *logging.Logger
	metrics       metrics.MetricsProvider
}

// New creates a new AMTP server
func New(cfg *config.Config) (*Server, error) {
	// Create discovery service
	var discoveryService processing.DiscoveryService
	if cfg.DNS.MockMode {
		discoveryService = discovery.NewMockDiscovery(cfg.DNS.MockRecords, cfg.DNS.CacheTTL)
	} else {
		discoveryService = discovery.NewDiscovery(
			cfg.DNS.Timeout,
			cfg.DNS.CacheTTL,
			cfg.DNS.Resolvers,
		)
	}

	// Create logger
	logger := logging.NewLogger(cfg.Logging).WithComponent("server")

	// Create metrics if enabled
	var metricsInstance metrics.MetricsProvider
	if cfg.Metrics != nil && cfg.Metrics.Enabled {
		metricsInstance = metrics.NewMetricsProvider()
	}

	// Create schema manager (if configured)
	var schemaManager *schema.Manager
	if cfg.Schema != nil {
		var err error
		schemaManager, err = schema.NewManager(*cfg.Schema)
		if err != nil {
			return nil, fmt.Errorf("failed to create schema manager: %w", err)
		}
	}

	// Create storage
	var storageConfig storage.StorageConfig
	if cfg.Storage.Type == "database" {
		storageConfig = storage.StorageConfig{
			Type: cfg.Storage.Type,
			Database: &storage.DatabaseStorageConfig{
				Driver:           cfg.Storage.Database.Driver,
				ConnectionString: cfg.Storage.Database.ConnectionString,
				MaxConnections:   cfg.Storage.Database.MaxConnections,
				MaxIdleTime:      cfg.Storage.Database.MaxIdleTime,
			},
		}
	} else {
		storageConfig = storage.DefaultStorageConfig() // Default to memory storage
	}
	storage, err := storage.NewStorage(storageConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create message storage: %w", err)
	}

	// Create agent registry first
	agentRegistryConfig := agents.RegistryConfig{
		LocalDomain:   cfg.Server.Domain,
		SchemaManager: schemaManager,
		APIKeySalt:    cfg.Auth.APIKeySalt,
	}
	agentRegistry := agents.NewRegistry(agentRegistryConfig, storage)

	// Create delivery engine with agent registry
	deliveryConfig := processing.DeliveryConfig{
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
		MaxConnections: 100,
		IdleTimeout:    90 * time.Second,
		UserAgent:      "AMTP-Gateway/1.0",
		MaxMessageSize: cfg.Message.MaxSize,
		AllowHTTP:      cfg.DNS.AllowHTTP,
		LocalDomain:    cfg.Server.Domain,
	}
	deliveryEngine := processing.NewDeliveryEngine(discoveryService, agentRegistry, deliveryConfig)

	// Create validator with agent management capabilities
	agentManagerAdapter := &AgentManagerAdapter{agentRegistry: agentRegistry}
	var validator *validation.Validator
	if schemaManager != nil {
		validator = validation.NewWithAgentManager(cfg.Message.MaxSize, schemaManager, agentManagerAdapter)
	} else {
		validator = validation.NewWithAgentManager(cfg.Message.MaxSize, nil, agentManagerAdapter)
	}

	// Create message processor
	processor := processing.NewMessageProcessor(discoveryService, deliveryEngine, storage)

	// Set Gin mode based on environment
	if cfg.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create router
	router := gin.New()

	// Create server
	server := &Server{
		config:        cfg,
		router:        router,
		discovery:     discoveryService,
		validator:     validator,
		processor:     processor,
		storage:       storage,
		agentRegistry: agentRegistry,
		schemaManager: schemaManager,
		logger:        logger,
		metrics:       metricsInstance,
	}

	// Setup middleware
	server.setupMiddleware()

	// Setup routes
	server.setupRoutes()

	// Create HTTP server
	server.httpServer = &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      server.router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Configure TLS if enabled
	if cfg.TLS.Enabled {
		tlsConfig, err := server.createTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		server.httpServer.TLSConfig = tlsConfig
	}

	return server, nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	if s.config.TLS.Enabled {
		return s.httpServer.ListenAndServeTLS(s.config.TLS.CertFile, s.config.TLS.KeyFile)
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// GetRouter returns the Gin router for testing purposes
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}

// setupMiddleware configures middleware for the server
func (s *Server) setupMiddleware() {
	// Recovery middleware
	s.router.Use(gin.Recovery())

	// Logging middleware
	s.router.Use(middleware.Logger(s.config.Logging))

	// CORS middleware
	s.router.Use(middleware.CORS())

	// Request ID middleware
	s.router.Use(middleware.RequestID())

	// Rate limiting middleware (if configured)
	if s.config.Auth.RequireAuth {
		s.router.Use(middleware.RateLimit())
	}

	// Authentication middleware (if required)
	if s.config.Auth.RequireAuth {
		s.router.Use(middleware.Auth(s.config.Auth))
	}

	// Request size limit middleware
	s.router.Use(middleware.RequestSizeLimit(s.config.Message.MaxSize))

	// Security headers middleware
	s.router.Use(middleware.SecurityHeaders())
}

// setupRoutes configures routes for the server
func (s *Server) setupRoutes() {
	// Capture server instance to avoid method value binding issues
	server := s

	// Health check endpoints
	server.router.GET("/health", func(c *gin.Context) { server.handleHealth(c) })
	server.router.GET("/ready", func(c *gin.Context) { server.handleReady(c) })

	// AMTP API v1
	v1 := server.router.Group("/v1")
	{
		// Message endpoints (public)
		v1.POST("/messages", server.withRequestMetrics(func(c *gin.Context) { server.handleSendMessage(c) }))
		v1.GET("/messages/:id", server.withRequestMetrics(func(c *gin.Context) { server.handleGetMessage(c) }))
		v1.GET("/messages/:id/status", server.withRequestMetrics(func(c *gin.Context) { server.handleGetMessageStatus(c) }))
		v1.GET("/messages", server.withRequestMetrics(func(c *gin.Context) { server.handleListMessages(c) }))

		// Discovery endpoints (public)
		v1.GET("/capabilities/:domain", server.withRequestMetrics(func(c *gin.Context) { server.handleGetCapabilities(c) }))

		// Agent discovery endpoints (public)
		discoveryGroup := v1.Group("/discovery")
		{
			discoveryGroup.GET("/agents", server.withRequestMetrics(func(c *gin.Context) { server.handleDiscoverAgents(c) }))
			discoveryGroup.GET("/agents/:domain", server.withRequestMetrics(func(c *gin.Context) { server.handleDiscoverAgentsByDomain(c) }))
		}

		// Inbox endpoints (agent protected - these use agent API keys, not admin keys)
		v1.GET("/inbox/:recipient", server.withRequestMetrics(func(c *gin.Context) { server.handleGetInbox(c) }))
		v1.DELETE("/inbox/:recipient/:messageId", server.withRequestMetrics(func(c *gin.Context) { server.handleAcknowledgeMessage(c) }))

		// Admin endpoints (admin protected)
		admin := v1.Group("/admin")
		admin.Use(middleware.AdminAuth(server.config.Auth))
		{
			// Agent management endpoints
			admin.POST("/agents", server.withRequestMetrics(func(c *gin.Context) { server.handleRegisterAgent(c) }))
			admin.DELETE("/agents/:address", server.withRequestMetrics(func(c *gin.Context) { server.handleUnregisterAgent(c) }))
			admin.GET("/agents", server.withRequestMetrics(func(c *gin.Context) { server.handleListAgents(c) }))

			// Schema management endpoints
			admin.POST("/schemas", server.withRequestMetrics(func(c *gin.Context) { server.handleRegisterSchema(c) }))
			admin.GET("/schemas", server.withRequestMetrics(func(c *gin.Context) { server.handleListSchemas(c) }))
			admin.GET("/schemas/:id", server.withRequestMetrics(func(c *gin.Context) { server.handleGetSchema(c) }))
			admin.PUT("/schemas/:id", server.withRequestMetrics(func(c *gin.Context) { server.handleUpdateSchema(c) }))
			admin.DELETE("/schemas/:id", server.withRequestMetrics(func(c *gin.Context) { server.handleDeleteSchema(c) }))
			admin.POST("/schemas/:id/validate", server.withRequestMetrics(func(c *gin.Context) { server.handleValidateSchema(c) }))
			admin.GET("/schemas/stats", server.withRequestMetrics(func(c *gin.Context) { server.handleSchemaStats(c) }))
		}
	}

	if server.metrics != nil {
		server.router.GET("/metrics", func(c *gin.Context) { server.handleMetrics(c) })
	}
}

// createTLSConfig creates TLS configuration
func (s *Server) createTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13, // Default to TLS 1.3
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		PreferServerCipherSuites: true,
	}

	// Set minimum TLS version based on configuration
	switch s.config.TLS.MinVersion {
	case "1.2":
		tlsConfig.MinVersion = tls.VersionTLS12
	case "1.3":
		tlsConfig.MinVersion = tls.VersionTLS13
	default:
		tlsConfig.MinVersion = tls.VersionTLS13
	}

	return tlsConfig, nil
}

// handleHealth handles health check requests (liveness probe)
func (s *Server) handleHealth(c *gin.Context) {
	health := s.checkHealth()

	statusCode := http.StatusOK
	if !health.Healthy {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, health)
}

// handleReady handles readiness check requests (readiness probe)
func (s *Server) handleReady(c *gin.Context) {
	readiness := s.checkReadiness()

	statusCode := http.StatusOK
	if !readiness.Ready {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, readiness)
}

// handleMetrics handles metrics requests
func (s *Server) handleMetrics(c *gin.Context) {
	if s.metrics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "metrics not enabled"})
		return
	}

	data, err := s.metrics.ToJSON()
	if err != nil {
		s.logger.Error("Failed to serialize metrics", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to serialize metrics"})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Data(http.StatusOK, "application/json", data)
}

// HealthStatus represents the health status of the gateway
type HealthStatus struct {
	Status     string            `json:"status"`
	Healthy    bool              `json:"healthy"`
	Timestamp  time.Time         `json:"timestamp"`
	Version    string            `json:"version"`
	Components map[string]string `json:"components"`
}

// ReadinessStatus represents the readiness status of the gateway
type ReadinessStatus struct {
	Status       string            `json:"status"`
	Ready        bool              `json:"ready"`
	Timestamp    time.Time         `json:"timestamp"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

// checkHealth performs basic health checks (liveness)
func (s *Server) checkHealth() HealthStatus {
	healthy := true
	components := make(map[string]string)

	// Check if server components are initialized
	if s.router == nil {
		healthy = false
		components["router"] = "not_initialized"
	} else {
		components["router"] = "healthy"
	}

	if s.processor == nil {
		healthy = false
		components["message_processor"] = "not_initialized"
	} else {
		components["message_processor"] = "healthy"
	}

	if s.agentRegistry == nil {
		healthy = false
		components["agent_registry"] = "not_initialized"
	} else {
		components["agent_registry"] = "healthy"
	}

	if s.discovery == nil {
		healthy = false
		components["discovery_service"] = "not_initialized"
	} else {
		components["discovery_service"] = "healthy"
	}

	// Schema manager is optional
	if s.schemaManager != nil {
		components["schema_manager"] = "healthy"
	} else {
		components["schema_manager"] = "not_configured"
	}

	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	return HealthStatus{
		Status:     status,
		Healthy:    healthy,
		Timestamp:  time.Now().UTC(),
		Version:    "1.0",
		Components: components,
	}
}

// checkReadiness performs comprehensive readiness checks
func (s *Server) checkReadiness() ReadinessStatus {
	ready := true
	dependencies := make(map[string]string)

	// Check agent registry functionality
	if s.agentRegistry != nil {
		// Test basic agent registry operations
		stats := s.agentRegistry.GetStats()
		if stats != nil {
			dependencies["agent_registry"] = "ready"
		} else {
			ready = false
			dependencies["agent_registry"] = "unavailable"
		}
	} else {
		ready = false
		dependencies["agent_registry"] = "not_initialized"
	}

	// Check schema manager (if configured)
	if s.schemaManager != nil {
		// Test schema registry connectivity
		registry := s.schemaManager.GetRegistry()
		if registry != nil {
			stats := registry.GetStats()
			if stats.TotalSchemas >= 0 { // Basic sanity check
				dependencies["schema_manager"] = "ready"
			} else {
				ready = false
				dependencies["schema_manager"] = "unavailable"
			}
		} else {
			ready = false
			dependencies["schema_manager"] = "registry_unavailable"
		}
	} else {
		dependencies["schema_manager"] = "not_configured"
	}

	// Check discovery service
	if s.discovery != nil {
		dependencies["discovery_service"] = "ready"
	} else {
		ready = false
		dependencies["discovery_service"] = "not_initialized"
	}

	// Check message processor
	if s.processor != nil {
		dependencies["message_processor"] = "ready"
	} else {
		ready = false
		dependencies["message_processor"] = "not_initialized"
	}

	// Check validator
	if s.validator != nil {
		dependencies["validator"] = "ready"
	} else {
		ready = false
		dependencies["validator"] = "not_initialized"
	}

	status := "ready"
	if !ready {
		status = "not_ready"
	}

	return ReadinessStatus{
		Status:       status,
		Ready:        ready,
		Timestamp:    time.Now().UTC(),
		Version:      "1.0",
		Dependencies: dependencies,
	}
}
