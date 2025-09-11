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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/processing"
	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/pkg/uuid"
	"github.com/gin-gonic/gin"
)

// generateIdempotencyKey creates a deterministic idempotency key based on request content
func generateIdempotencyKey(req *types.SendMessageRequest) string {
	// Create a canonical representation of the request for hashing
	canonical := struct {
		Sender       string                    `json:"sender"`
		Recipients   []string                  `json:"recipients"`
		Subject      string                    `json:"subject"`
		Schema       string                    `json:"schema"`
		Coordination *types.CoordinationConfig `json:"coordination"`
		Headers      map[string]interface{}    `json:"headers"`
		Payload      json.RawMessage           `json:"payload"`
		Attachments  []types.Attachment        `json:"attachments"`
	}{
		Sender:       req.Sender,
		Recipients:   req.Recipients,
		Subject:      req.Subject,
		Schema:       req.Schema,
		Coordination: req.Coordination,
		Headers:      req.Headers,
		Payload:      req.Payload,
		Attachments:  req.Attachments,
	}

	// Marshal to JSON for consistent hashing
	data, _ := json.Marshal(canonical)

	// Create SHA256 hash
	hash := sha256.Sum256(data)

	// Format as UUIDv4 (8-4-4-4-12 format with version 4 indicator)
	hashHex := hex.EncodeToString(hash[:])
	// Take first 32 hex chars and format them as UUID
	// For UUIDv4: version = 4, variant = 8/9/a/b
	return fmt.Sprintf("%s-%s-4%s-8%s-%s",
		hashHex[0:8],   // 8 chars
		hashHex[8:12],  // 4 chars
		hashHex[13:16], // 3 chars (version '4' prepended)
		hashHex[16:19], // 3 chars (variant '8' prepended)
		hashHex[20:32]) // 12 chars
}

// handleSendMessage handles POST /v1/messages
func (s *Server) handleSendMessage(c *gin.Context) {
	timer := time.Now()
	if s.metrics != nil {
		s.metrics.IncMessagesInFlight()
		defer s.metrics.DecMessagesInFlight()
	}
	var req types.SendMessageRequest

	// Parse request body
	if err := c.ShouldBindJSON(&req); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_REQUEST_FORMAT",
			"Invalid request format", map[string]interface{}{
				"parse_error": err.Error(),
			})
		return
	}

	// Validate request
	if err := s.validator.ValidateSendRequest(&req); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "VALIDATION_FAILED",
			"Request validation failed", map[string]interface{}{
				"validation_error": err.Error(),
			})
		return
	}

	// Generate message ID and deterministic idempotency key
	messageID, err := uuid.GenerateV7()
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "ID_GENERATION_FAILED",
			"Failed to generate message ID", nil)
		return
	}

	// Generate deterministic idempotency key based on request content
	idempotencyKey := generateIdempotencyKey(&req)

	// Create AMTP message
	message := &types.Message{
		Version:        "1.0",
		MessageID:      messageID,
		IdempotencyKey: idempotencyKey,
		Timestamp:      time.Now().UTC(),
		Sender:         req.Sender,
		Recipients:     req.Recipients,
		Subject:        req.Subject,
		Schema:         req.Schema,
		Coordination:   req.Coordination,
		Headers:        req.Headers,
		Payload:        req.Payload,
		Attachments:    req.Attachments,
	}

	// Validate the complete message
	if err := s.validator.ValidateMessage(message); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "MESSAGE_VALIDATION_FAILED",
			"Message validation failed", map[string]interface{}{
				"validation_error": err.Error(),
			})
		return
	}

	// Process message using the message processor
	processingOptions := processing.ProcessingOptions{
		ImmediatePath: true, // Use immediate path for now
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	result, err := s.processor.ProcessMessage(c.Request.Context(), message, processingOptions)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "PROCESSING_FAILED",
			"Message processing failed", map[string]interface{}{
				"processing_error": err.Error(),
			})
		return
	}

	// Determine response status based on processing result
	var httpStatus int
	var status string
	switch result.Status {
	case types.StatusDelivered:
		httpStatus = http.StatusOK
		status = "delivered"
	case types.StatusDelivering:
		httpStatus = http.StatusAccepted
		status = "delivering"
	case types.StatusQueued:
		httpStatus = http.StatusAccepted
		status = "queued"
	case types.StatusFailed:
		httpStatus = http.StatusBadRequest
		status = "failed"
	default:
		httpStatus = http.StatusAccepted
		status = "accepted"
	}

	// Return response
	response := types.SendMessageResponse{
		MessageID:  result.MessageID,
		Status:     status,
		Recipients: result.Recipients,
	}

	// Record message processing metrics
	coordinationType := "immediate"
	if message.Coordination != nil {
		coordinationType = message.Coordination.Type
	}

	if s.metrics != nil {
		s.metrics.RecordMessage(
			string(result.Status),
			coordinationType,
			time.Since(timer),
			message.Size(),
			message.Schema,
		)
	}

	// Log message processing
	s.logger.LogMessageProcessing(
		messageID,
		"send",
		string(result.Status),
		func() *time.Duration { d := time.Since(timer); return &d }(),
		err,
	)

	s.respondWithSuccess(c, httpStatus, response)
}

// handleGetMessage handles GET /v1/messages/:id
func (s *Server) handleGetMessage(c *gin.Context) {
	messageID := c.Param("id")

	// Validate message ID format
	if !uuid.IsValidV7(messageID) {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_MESSAGE_ID",
			"Invalid message ID format", nil)
		return
	}

	// Retrieve message from storage
	message, err := s.storage.GetMessage(c.Request.Context(), messageID)
	if err != nil {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message not found", nil)
		return
	}

	s.respondWithSuccess(c, http.StatusOK, message)
}

// handleGetMessageStatus handles GET /v1/messages/:id/status
func (s *Server) handleGetMessageStatus(c *gin.Context) {
	messageID := c.Param("id")

	// Validate message ID format
	if !uuid.IsValidV7(messageID) {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_MESSAGE_ID",
			"Invalid message ID format", nil)
		return
	}

	// Retrieve message status from storage
	status, err := s.storage.GetStatus(c.Request.Context(), messageID)
	if err != nil {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message status not found", nil)
		return
	}

	s.respondWithSuccess(c, http.StatusOK, status)
}

// handleListMessages handles GET /v1/messages
func (s *Server) handleListMessages(c *gin.Context) {
	// Parse query parameters
	status := c.Query("status")
	sender := c.Query("sender")
	recipient := c.Query("recipient")
	since := c.Query("since")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	// Validate limit and offset
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 1000 {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_LIMIT",
			"Limit must be between 1 and 1000", nil)
		return
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_OFFSET",
			"Offset must be non-negative", nil)
		return
	}

	// Parse since timestamp if provided
	var sinceTime *time.Time
	if since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			s.respondWithError(c, http.StatusBadRequest, "INVALID_SINCE_FORMAT",
				"Since parameter must be in RFC3339 format", nil)
			return
		}
		sinceTime = &parsed
	}

	// For Phase 1, return empty list
	// In later phases, this would query storage with filters
	_ = status
	_ = sender
	_ = recipient
	_ = sinceTime

	response := gin.H{
		"messages": []types.MessageStatus{},
		"total":    0,
		"limit":    limit,
		"offset":   offset,
	}

	c.JSON(http.StatusOK, response)
}

// handleGetCapabilities handles GET /v1/capabilities/:domain
func (s *Server) handleGetCapabilities(c *gin.Context) {
	domain := c.Param("domain")

	if domain == "" {
		s.respondWithError(c, http.StatusBadRequest, "DOMAIN_REQUIRED",
			"Domain parameter is required", nil)
		return
	}

	// Discover capabilities for the domain
	capabilities, err := s.discovery.DiscoverCapabilities(c.Request.Context(), domain)
	if err != nil {
		s.respondWithError(c, http.StatusNotFound, "CAPABILITIES_NOT_FOUND",
			"AMTP capabilities not found for domain", map[string]interface{}{
				"domain": domain,
				"error":  err.Error(),
			})
		return
	}

	// If this is our own domain, return schemas supported by registered agents
	if domain == s.config.Server.Domain {
		capabilities.Schemas = s.agentRegistry.GetSupportedSchemas()
	}

	c.JSON(http.StatusOK, capabilities)
}

// Schema Management Handlers

// handleRegisterSchema handles POST /v1/admin/schemas
func (s *Server) handleRegisterSchema(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	var req struct {
		ID         string          `json:"id" binding:"required"`
		Definition json.RawMessage `json:"definition" binding:"required"`
		Force      bool            `json:"force,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_REQUEST_FORMAT",
			"Invalid request format", map[string]interface{}{
				"parse_error": err.Error(),
			})
		return
	}

	// Parse schema identifier
	schemaID, err := schema.ParseSchemaIdentifier(req.ID)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_SCHEMA_ID",
			"Invalid schema identifier", map[string]interface{}{
				"schema_id": req.ID,
				"error":     err.Error(),
			})
		return
	}

	// Create schema
	newSchema := &schema.Schema{
		ID:          *schemaID,
		Definition:  req.Definition,
		PublishedAt: time.Now().UTC(),
	}

	// Register schema
	var regErr error
	if req.Force {
		regErr = s.schemaManager.GetRegistry().RegisterOrUpdateSchema(c.Request.Context(), newSchema, nil)
	} else {
		regErr = s.schemaManager.GetRegistry().RegisterSchema(c.Request.Context(), newSchema, nil)
	}

	if regErr != nil {
		if !req.Force && regErr.Error() == "schema already exists" {
			s.respondWithError(c, http.StatusConflict, "SCHEMA_ALREADY_EXISTS",
				"Schema already exists", map[string]interface{}{
					"schema_id": req.ID,
					"hint":      "Use force=true to overwrite existing schema",
				})
		} else {
			s.respondWithError(c, http.StatusInternalServerError, "SCHEMA_REGISTRATION_FAILED",
				"Failed to register schema", map[string]interface{}{
					"schema_id": req.ID,
					"error":     regErr.Error(),
				})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":   "Schema registered successfully",
		"schema_id": req.ID,
		"timestamp": time.Now().UTC(),
	})
}

// handleListSchemas handles GET /v1/admin/schemas
func (s *Server) handleListSchemas(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	pattern := c.Query("pattern")
	schemas, err := s.schemaManager.GetRegistry().ListSchemas(c.Request.Context(), pattern)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "SCHEMA_LIST_FAILED",
			"Failed to list schemas", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"schemas":   schemas,
		"count":     len(schemas),
		"timestamp": time.Now().UTC(),
	})
}

// handleGetSchema handles GET /v1/admin/schemas/:id
func (s *Server) handleGetSchema(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	schemaIDStr := c.Param("id")
	schemaID, err := schema.ParseSchemaIdentifier(schemaIDStr)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_SCHEMA_ID",
			"Invalid schema identifier", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	schemaObj, err := s.schemaManager.GetRegistry().GetSchema(c.Request.Context(), *schemaID)
	if err != nil {
		s.respondWithError(c, http.StatusNotFound, "SCHEMA_NOT_FOUND",
			"Schema not found", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"schema":    schemaObj,
		"timestamp": time.Now().UTC(),
	})
}

// handleUpdateSchema handles PUT /v1/admin/schemas/:id
func (s *Server) handleUpdateSchema(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	schemaIDStr := c.Param("id")
	schemaID, err := schema.ParseSchemaIdentifier(schemaIDStr)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_SCHEMA_ID",
			"Invalid schema identifier", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	var req struct {
		Definition json.RawMessage `json:"definition" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_REQUEST_FORMAT",
			"Invalid request format", map[string]interface{}{
				"parse_error": err.Error(),
			})
		return
	}

	// Create updated schema
	updatedSchema := &schema.Schema{
		ID:          *schemaID,
		Definition:  req.Definition,
		PublishedAt: time.Now().UTC(),
	}

	// Update schema
	err = s.schemaManager.GetRegistry().RegisterOrUpdateSchema(c.Request.Context(), updatedSchema, nil)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "SCHEMA_UPDATE_FAILED",
			"Failed to update schema", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Schema updated successfully",
		"schema_id": schemaIDStr,
		"timestamp": time.Now().UTC(),
	})
}

// handleDeleteSchema handles DELETE /v1/admin/schemas/:id
func (s *Server) handleDeleteSchema(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	schemaIDStr := c.Param("id")
	schemaID, err := schema.ParseSchemaIdentifier(schemaIDStr)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_SCHEMA_ID",
			"Invalid schema identifier", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	// Delete schema (assuming we add this method to the registry interface)
	err = s.schemaManager.GetRegistry().DeleteSchema(c.Request.Context(), *schemaID)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "SCHEMA_DELETE_FAILED",
			"Failed to delete schema", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Schema deleted successfully",
		"schema_id": schemaIDStr,
		"timestamp": time.Now().UTC(),
	})
}

// handleValidateSchema handles POST /v1/admin/schemas/:id/validate
func (s *Server) handleValidateSchema(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	schemaIDStr := c.Param("id")
	_, err := schema.ParseSchemaIdentifier(schemaIDStr)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_SCHEMA_ID",
			"Invalid schema identifier", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	var req struct {
		Payload json.RawMessage `json:"payload" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_REQUEST_FORMAT",
			"Invalid request format", map[string]interface{}{
				"parse_error": err.Error(),
			})
		return
	}

	// Create a temporary message for validation
	message := &types.Message{
		Schema:  schemaIDStr,
		Payload: req.Payload,
	}

	// Validate payload against schema
	report, err := s.schemaManager.ValidateMessage(c.Request.Context(), message)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "VALIDATION_FAILED",
			"Schema validation failed", map[string]interface{}{
				"schema_id": schemaIDStr,
				"error":     err.Error(),
			})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":     report.IsValid(),
		"errors":    report.Errors,
		"warnings":  report.Warnings,
		"timestamp": time.Now().UTC(),
	})
}

// handleSchemaStats handles GET /v1/admin/schemas/stats
func (s *Server) handleSchemaStats(c *gin.Context) {
	if s.schemaManager == nil {
		s.respondWithError(c, http.StatusServiceUnavailable, "SCHEMA_MANAGER_UNAVAILABLE",
			"Schema management is not configured", nil)
		return
	}

	// Get schema registry statistics
	stats := s.schemaManager.GetRegistry().GetStats()

	c.JSON(http.StatusOK, gin.H{
		"stats":     stats,
		"timestamp": time.Now().UTC(),
	})
}

// handleRegisterAgent handles POST /v1/admin/agents
func (s *Server) handleRegisterAgent(c *gin.Context) {
	var agent agents.LocalAgent

	if err := c.ShouldBindJSON(&agent); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_REQUEST_FORMAT",
			"Invalid agent registration format", map[string]interface{}{
				"parse_error": err.Error(),
			})
		return
	}

	// Use the agent registry directly
	if err := s.agentRegistry.RegisterAgent(&agent); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "AGENT_REGISTRATION_FAILED",
			"Failed to register agent", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	s.respondWithSuccess(c, http.StatusCreated, gin.H{
		"message": "Agent registered successfully",
		"agent":   agent,
	})
}

// handleUnregisterAgent handles DELETE /v1/admin/agents/:address
func (s *Server) handleUnregisterAgent(c *gin.Context) {
	agentName := c.Param("address") // Keep param name for backward compatibility

	// Use the agent registry directly
	if err := s.agentRegistry.UnregisterAgent(agentName); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "AGENT_UNREGISTRATION_FAILED",
			"Failed to unregister agent", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"message": "Agent unregistered successfully",
		"name":    agentName,
	})
}

// handleListAgents handles GET /v1/admin/agents
func (s *Server) handleListAgents(c *gin.Context) {
	// Use the agent registry directly
	agents := s.agentRegistry.GetAllAgents()

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"agents": agents,
		"count":  len(agents),
	})
}

// handleGetInbox handles GET /v1/inbox/:recipient
func (s *Server) handleGetInbox(c *gin.Context) {
	recipient := c.Param("recipient")

	// Verify agent authorization for inbox access
	if !s.verifyAgentAccess(c, recipient) {
		return // verifyAgentAccess handles the error response
	}

	// Get inbox messages from unified storage and update last access
	messages, err := s.storage.GetInboxMessages(c.Request.Context(), recipient)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "INBOX_ACCESS_FAILED",
			"Failed to retrieve inbox messages", nil)
		return
	}
	s.agentRegistry.UpdateLastAccess(recipient)

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"recipient": recipient,
		"messages":  messages,
		"count":     len(messages),
	})
}

// handleAcknowledgeMessage handles DELETE /v1/inbox/:recipient/:messageId
func (s *Server) handleAcknowledgeMessage(c *gin.Context) {
	recipient := c.Param("recipient")
	messageID := c.Param("messageId")

	// Verify agent authorization for inbox access
	if !s.verifyAgentAccess(c, recipient) {
		return // verifyAgentAccess handles the error response
	}

	// Acknowledge the message using unified storage and update last access
	if err := s.storage.AcknowledgeMessage(c.Request.Context(), recipient, messageID); err != nil {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message not found or already acknowledged", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	// Update last access timestamp
	s.agentRegistry.UpdateLastAccess(recipient)

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"message":    "Message acknowledged successfully",
		"recipient":  recipient,
		"message_id": messageID,
	})
}

// verifyAgentAccess checks if the requester can access the specified agent's inbox
func (s *Server) verifyAgentAccess(c *gin.Context, agentAddress string) bool {
	// Extract API key from Authorization header
	authHeader := c.GetHeader("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		s.respondWithError(c, http.StatusUnauthorized, "MISSING_AUTHORIZATION",
			"Agent API key required for inbox access", map[string]interface{}{
				"required_header": "Authorization: Bearer <api-key>",
				"agent":           agentAddress,
			})
		return false
	}

	apiKey := strings.TrimPrefix(authHeader, "Bearer ")
	if apiKey == "" {
		s.respondWithError(c, http.StatusUnauthorized, "EMPTY_API_KEY",
			"API key cannot be empty", map[string]interface{}{
				"agent": agentAddress,
			})
		return false
	}

	// Verify agent access
	if !s.agentRegistry.VerifyAPIKey(agentAddress, apiKey) {
		s.respondWithError(c, http.StatusForbidden, "ACCESS_DENIED",
			"Invalid API key for agent", map[string]interface{}{
				"agent": agentAddress,
			})
		return false
	}

	return true
}

// handleDiscoverAgents handles GET /v1/discovery/agents
// Returns all agents registered on this gateway
func (s *Server) handleDiscoverAgents(c *gin.Context) {
	// Get query parameters for filtering
	deliveryMode := c.Query("delivery_mode")       // filter by "push" or "pull"
	activeOnly := c.Query("active_only") == "true" // only show recently active agents

	agents := make([]gin.H, 0)

	// Get agents from the agent registry
	localAgents := s.agentRegistry.GetAllAgents()

	// Build agent list for discovery (without sensitive information)
	for address, agent := range localAgents {
		// Apply delivery mode filter if specified
		if deliveryMode != "" && agent.DeliveryMode != deliveryMode {
			continue
		}

		// Apply active filter if specified
		if activeOnly && time.Since(agent.LastAccess) > 30*24*time.Hour {
			continue
		}

		agentInfo := gin.H{
			"address":       address,
			"delivery_mode": agent.DeliveryMode,
			"created_at":    agent.CreatedAt,
		}

		// Include supported schemas if any
		if len(agent.SupportedSchemas) > 0 {
			agentInfo["supported_schemas"] = agent.SupportedSchemas
		}

		// Include last_active if it's recent (within 30 days) for activity indication
		if time.Since(agent.LastAccess) < 30*24*time.Hour {
			agentInfo["last_active"] = agent.LastAccess
		}

		agents = append(agents, agentInfo)
	}

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"agents":      agents,
		"agent_count": len(agents),
		"domain":      s.config.Server.Domain,
		"timestamp":   time.Now().UTC(),
	})
}

// handleDiscoverAgentsByDomain handles GET /v1/discovery/agents/:domain
// Returns agents for a specific domain (useful for multi-domain setups)
func (s *Server) handleDiscoverAgentsByDomain(c *gin.Context) {
	domain := c.Param("domain")

	// For now, we only serve agents for our own domain
	// In a multi-domain setup, this could be extended to handle multiple domains
	if domain != s.config.Server.Domain {
		s.respondWithError(c, http.StatusNotFound, "DOMAIN_NOT_FOUND",
			"Domain not served by this gateway", map[string]interface{}{
				"requested_domain": domain,
				"served_domain":    s.config.Server.Domain,
			})
		return
	}

	// Delegate to the main agent discovery handler
	s.handleDiscoverAgents(c)
}
