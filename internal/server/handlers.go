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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/middleware"
	"github.com/amtp-protocol/agentry/internal/processing"
	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/pkg/uuid"
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
		ResponseType string                    `json:"response_type"`
		InReplyTo    string                    `json:"in_reply_to"`
		Attachments  []types.Attachment        `json:"attachments"`
	}{
		Sender:       req.Sender,
		Recipients:   req.Recipients,
		Subject:      req.Subject,
		Schema:       req.Schema,
		Coordination: req.Coordination,
		Headers:      req.Headers,
		Payload:      req.Payload,
		ResponseType: req.ResponseType,
		InReplyTo:    req.InReplyTo,
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

// isWorkflowResponseType reports whether a reply with this response_type
// participates in the workflow state machine. Error responses must be routed
// too, otherwise participants can never be marked failed and stop_on_failure
// never fires.
func isWorkflowResponseType(responseType string) bool {
	switch responseType {
	case "workflow_response", "workflow_error", "error":
		return true
	}
	return false
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
	messageID := req.MessageID
	if messageID == "" {
		var err error
		messageID, err = uuid.GenerateV7()
		if err != nil {
			s.respondWithError(c, http.StatusInternalServerError, "ID_GENERATION_FAILED",
				"Failed to generate message ID", nil)
			return
		}
	}

	// Generate deterministic idempotency key based on request content
	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = generateIdempotencyKey(&req)
	}

	timestamp := time.Now().UTC()
	if req.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
			timestamp = parsed.UTC()
		}
	}

	// Create AMTP message
	message := &types.Message{
		Version:        "1.0",
		MessageID:      messageID,
		IdempotencyKey: idempotencyKey,
		Timestamp:      timestamp,
		Sender:         req.Sender,
		Recipients:     req.Recipients,
		Subject:        req.Subject,
		Schema:         req.Schema,
		Coordination:   req.Coordination,
		Headers:        req.Headers,
		Payload:        req.Payload,
		ResponseType:   req.ResponseType,
		InReplyTo:      req.InReplyTo,
		WorkflowID:     req.WorkflowID,
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

	// Intercept workflow responses.
	//
	// If this gateway created the workflow (shared-DB deployment) or is the sole
	// process (MemoryStorage), ProcessResponse updates the state machine in-place.
	//
	// If the workflow is not found:
	//   - Shared-DB multi-replica: each replica shares the same storage, so
	//     ErrWorkflowNotFound does NOT fire — every replica can see and process
	//     the workflow concurrently (the optimistic-lock protocol in the workflow
	//     manager serializes conflicting writes).
	//   - MemoryStorage single-process: this is the only process, so
	//     ErrWorkflowNotFound means the workflow genuinely does not exist.
	//
	// CAUTION: In a multi-replica deployment with per-replica MemoryStorage (or
	// non-shared DB), a workflow created on replica A is invisible to replica B.
	// There is currently NO cross-replica forwarding of workflow responses; the
	// owning replica must directly receive the response, e.g. via a frontend
	// load-balancer that routes requests by workflow_id affinity.
	// See docs/DEPLOYMENT.md for deployment topology guidance.
	// Correlation key: prefer the dedicated workflow_id field; fall back to
	// in_reply_to for clients that predate it.
	workflowRef := message.WorkflowID
	if workflowRef == "" {
		workflowRef = message.InReplyTo
	}
	if isWorkflowResponseType(message.ResponseType) && workflowRef != "" {
		if s.workflow != nil {
			err := s.workflow.ProcessResponse(c.Request.Context(), workflowRef, message)
			if err != nil {
				if errors.Is(err, storage.ErrWorkflowNotFound) {
					// Workflow not found in this storage. Fall through to normal
					// delivery so the sender can at least receive the response.
					// In shared-DB deployments this branch is typically unreachable
					// (all replicas share the same `workflows` table).
				} else {
					s.respondWithError(c, http.StatusInternalServerError, "WORKFLOW_UPDATE_FAILED",
						"Failed to process workflow response", map[string]interface{}{
							"error": err.Error(),
						})
					return
				}
			}
		}
	}

	// Is sender a local user?
	senderDomain := ""
	parts := strings.Split(message.Sender, "@")
	if len(parts) == 2 {
		senderDomain = parts[1]
	}
	isSenderLocal := strings.EqualFold(senderDomain, s.config.Server.Domain)

	// Process message using the message processor
	processingOptions := processing.ProcessingOptions{
		ImmediatePath: message.Coordination == nil || !isSenderLocal,
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
		WorkflowID: result.WorkflowID,
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
// Requires an Agent API key or the gateway admin key; agents may only read
// messages they sent or received, while the admin may read any message.
func (s *Server) handleGetMessage(c *gin.Context) {
	messageID := c.Param("id")

	// Validate message ID format
	if !uuid.IsValidV7(messageID) {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_MESSAGE_ID",
			"Invalid message ID format", nil)
		return
	}

	// Authenticate the caller as an agent or the gateway admin.
	agentAddr, isAdmin, ok := s.authenticateAgent(c)
	if !ok {
		return
	}

	// Retrieve message from storage
	message, err := s.storage.GetMessage(c.Request.Context(), messageID)
	if err != nil {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message not found", nil)
		return
	}

	// Ownership check: agents must be the sender or a recipient; the admin
	// may inspect any message (including ones submitted by unregistered
	// senders, which no agent key can reach).
	if !isAdmin && !s.messageBelongsToAgent(message, agentAddr) {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message not found", nil)
		return
	}

	s.respondWithSuccess(c, http.StatusOK, message)
}

// handleGetMessageStatus handles GET /v1/messages/:id/status
// Requires an Agent API key or the gateway admin key; agents may only read
// statuses for messages they sent or received, while the admin may read any.
func (s *Server) handleGetMessageStatus(c *gin.Context) {
	messageID := c.Param("id")

	// Validate message ID format
	if !uuid.IsValidV7(messageID) {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_MESSAGE_ID",
			"Invalid message ID format", nil)
		return
	}

	// Authenticate the caller as an agent or the gateway admin.
	agentAddr, isAdmin, ok := s.authenticateAgent(c)
	if !ok {
		return
	}

	// Verify the caller has access to the message before returning status.
	message, err := s.storage.GetMessage(c.Request.Context(), messageID)
	if err != nil {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message not found", nil)
		return
	}
	if !isAdmin && !s.messageBelongsToAgent(message, agentAddr) {
		s.respondWithError(c, http.StatusNotFound, "MESSAGE_NOT_FOUND",
			"Message not found", nil)
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

// buildListMessagesFilter returns the storage filter for the message list
// endpoint, scoped to the authenticated agent. The storage layer applies AND
// semantics between Sender and Recipients by default; the "all traffic" case
// (no direction) uses OR semantics via MessageFilter.Or so a single query
// covers sent + received and pagination applies to the merged, newest-first
// result set. An empty agentAddr (the admin caller) leaves the result
// unrestricted when no direction filter is given.
func buildListMessagesFilter(status, sender, recipient, agentAddr string, since *time.Time, limit, offset int) storage.MessageFilter {
	filter := storage.MessageFilter{
		Status: types.DeliveryStatus(status),
		Limit:  limit,
		Offset: offset,
	}
	if since != nil {
		unix := since.Unix()
		filter.Since = &unix
	}

	switch {
	case sender != "" && recipient != "":
		// Explicit direction: a specific sender AND recipient conversation.
		filter.Sender = sender
		filter.Recipients = []string{recipient}
	case sender != "":
		filter.Sender = sender
	case recipient != "":
		filter.Recipients = []string{recipient}
	default:
		// No direction: everything the agent sent or received. A single
		// OR-mode query covers both directions so Limit/Offset are applied
		// to the merged result set. (Previously two queries were paginated
		// independently and merged, which could return up to 2*limit
		// messages, overlap or drop rows across offsets, and left the merged
		// list unsorted.) Admin callers have no agent address, so no
		// direction constraint is applied and the full set is returned.
		if agentAddr != "" {
			filter.Sender = agentAddr
			filter.Recipients = []string{agentAddr}
			filter.Or = true
		}
	}
	return filter
}

// normalizeParticipantFilter normalizes a sender/recipient filter parameter
// on the message list endpoint. Bare agent names are resolved to their full
// local address so "?sender=viewer" works like "?sender=viewer@localhost";
// full local addresses are normalized too. Full addresses with a foreign
// domain pass through unchanged: they can only be counterpart references —
// no local agent key can authenticate as a foreign address — and the
// conversation scoping in handleListMessages pins them to the authenticated
// agent. An empty value is returned unchanged, meaning the filter is absent.
func (s *Server) normalizeParticipantFilter(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, "@") {
		parts := strings.SplitN(value, "@", 2)
		if parts[1] != s.config.Server.Domain {
			return value, nil
		}
	}
	return s.agentRegistry.ResolveAgentAddress(value)
}

// resolveConversationFilters scopes the sender/recipient filters of a
// non-admin caller to the authenticated agent's own traffic while allowing
// the other side of a conversation to reference any counterpart — local or
// foreign. Rules:
//   - A single-sided filter naming a counterpart pins the agent as the other
//     side: "?sender=bob" means "messages bob sent to me" and
//     "?recipient=bob" means "messages I sent to bob".
//   - When both sides are given, the authenticated agent must be one of
//     them; the other side may be any counterpart (or the agent itself for
//     self-to-self traffic).
//   - Both sides naming other agents (neither is the caller) is rejected:
//     the caller cannot inspect traffic it is not a participant in.
func resolveConversationFilters(agentAddr, sender, recipient string) (string, string, error) {
	switch {
	case sender == agentAddr || recipient == agentAddr:
		// The agent is already pinned as one side; the other side may be
		// empty ("messages I sent/received") or any counterpart.
		return sender, recipient, nil
	case sender != "" && recipient != "":
		// Both sides specified but neither references the agent.
		return "", "", fmt.Errorf("neither sender nor recipient references the authenticated agent")
	case sender != "":
		// "?sender=X" scoped to the agent: the agent is the recipient side.
		return sender, agentAddr, nil
	case recipient != "":
		// "?recipient=X" scoped to the agent: the agent is the sender side.
		return agentAddr, recipient, nil
	}
	return sender, recipient, nil
}

// handleListMessages handles GET /v1/messages
// Requires an Agent API key or the gateway admin key. Results are scoped to
// the authenticated agent's own traffic. Sender/recipient filters may
// reference the agent itself or a counterpart on the other side of a
// conversation: "?recipient=bob@remote.com" lists messages the agent sent to
// bob, and "?sender=bob@remote.com" lists messages bob sent to the agent.
// The admin may list any message and filter by any participant.
func (s *Server) handleListMessages(c *gin.Context) {
	// Authenticate the caller as an agent or the gateway admin.
	agentAddr, isAdmin, ok := s.authenticateAgent(c)
	if !ok {
		return
	}

	// Parse query parameters
	status := c.Query("status")
	sender := c.Query("sender")
	recipient := c.Query("recipient")
	since := c.Query("since")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	// Validate the status filter against the known delivery statuses before
	// it reaches storage.
	if status != "" && !types.DeliveryStatus(status).Valid() {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_STATUS",
			"Status must be one of: pending, queued, delivering, delivered, failed, retrying", nil)
		return
	}

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

	// A caller may only query their own agent's traffic. Normalize bare-name
	// filters to full addresses so "?sender=viewer" works like
	// "?sender=viewer@localhost"; foreign-domain addresses pass through as
	// counterpart references. Admin callers may filter by any participant.
	// Non-admin callers may reference a counterpart on one side of a
	// conversation as long as the authenticated agent is pinned on the other
	// side, so "?recipient=bob@remote.com" lists messages the agent sent to
	// bob instead of returning 403. A filter that cannot be resolved to a
	// valid agent name is rejected.
	sender, err = s.normalizeParticipantFilter(sender)
	if err != nil {
		s.respondWithError(c, http.StatusForbidden, "ACCESS_DENIED",
			"Sender filter must reference a valid agent", nil)
		return
	}
	recipient, err = s.normalizeParticipantFilter(recipient)
	if err != nil {
		s.respondWithError(c, http.StatusForbidden, "ACCESS_DENIED",
			"Recipient filter must reference a valid agent", nil)
		return
	}
	if !isAdmin {
		sender, recipient, err = resolveConversationFilters(agentAddr, sender, recipient)
		if err != nil {
			s.respondWithError(c, http.StatusForbidden, "ACCESS_DENIED",
				"Sender and recipient filters must reference the authenticated agent", nil)
			return
		}
	}

	// Build the storage filter scoped to the authenticated agent. The
	// storage layer applies AND semantics between Sender and Recipients by
	// default; the "all traffic" case (no direction) uses OR semantics via
	// MessageFilter.Or so a single query covers sent + received and
	// pagination applies to the merged, newest-first result set.
	filter := buildListMessagesFilter(status, sender, recipient, agentAddr, sinceTime, limit, offset)

	// List the page. De-duplication is defensive: the filter yields at most
	// one row per message.
	seen := make(map[string]struct{})
	messages := []*types.Message{}
	page, err := s.storage.ListMessages(c.Request.Context(), filter)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "MESSAGE_LIST_FAILED",
			"Failed to list messages", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}
	for _, msg := range page {
		if _, dup := seen[msg.MessageID]; dup {
			continue
		}
		seen[msg.MessageID] = struct{}{}
		messages = append(messages, msg)
	}

	// Attach delivery status to each message so callers get a complete view.
	// Collect the page's message IDs first, then fetch all statuses in a
	// single batch to avoid an N+1 GetStatus call per message.
	response := make([]gin.H, 0, len(messages))
	statusIDs := make([]string, 0, len(messages))
	for _, msg := range messages {
		// Post-filter for safety: the storage filter may be an OR match, so
		// only include messages the authenticated agent sent or received.
		// Admin callers see the full set, including messages whose
		// participants are all foreign.
		if !isAdmin && !s.messageBelongsToAgent(msg, agentAddr) {
			continue
		}
		item := gin.H{
			"message_id":      msg.MessageID,
			"idempotency_key": msg.IdempotencyKey,
			"timestamp":       msg.Timestamp,
			"sender":          msg.Sender,
			"recipients":      msg.Recipients,
			"subject":         msg.Subject,
			"schema":          msg.Schema,
			"in_reply_to":     msg.InReplyTo,
			"response_type":   msg.ResponseType,
			"workflow_id":     msg.WorkflowID,
		}
		if msg.Payload != nil {
			item["payload"] = msg.Payload
		}
		response = append(response, item)
		statusIDs = append(statusIDs, msg.MessageID)
	}

	statuses, err := s.storage.GetStatuses(c.Request.Context(), statusIDs)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "MESSAGE_LIST_FAILED",
			"Failed to get message statuses", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}
	for i, item := range response {
		if st, ok := statuses[statusIDs[i]]; ok && st != nil {
			item["status"] = st.Status
			item["delivery"] = st
		}
	}

	// total is the number of messages in the filtered set before pagination.
	// CountMessages covers the full filtered set without materializing rows:
	// Limit/Offset are ignored by the storage backends, and a count failure
	// surfaces as an error instead of silently undercounting.
	total, err := s.storage.CountMessages(c.Request.Context(), filter)
	if err != nil {
		s.respondWithError(c, http.StatusInternalServerError, "MESSAGE_LIST_FAILED",
			"Failed to count messages", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"messages": response,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
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
		capabilities.Schemas = s.agentRegistry.GetSupportedSchemas(c.Request.Context())
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
	if err := s.agentRegistry.RegisterAgent(c.Request.Context(), &agent); err != nil {
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
	if err := s.agentRegistry.UnregisterAgent(c.Request.Context(), agentName); err != nil {
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
	agents := s.agentRegistry.GetAllAgents(c.Request.Context())

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"agents": agents,
		"count":  len(agents),
	})
}

// handleRotateAgentKey handles POST /v1/admin/agents/:address/rotate-key
// Generates a new API key for the agent and returns it in plaintext so the
// caller can store it securely. The old key becomes invalid immediately.
func (s *Server) handleRotateAgentKey(c *gin.Context) {
	agentAddress := c.Param("address")

	// Normalize a bare agent name or local address to its full address before
	// consulting the registry, matching the sibling PATCH/DELETE endpoints.
	// Foreign-domain addresses are rejected here with an explicit
	// domain-mismatch error instead of reaching the registry unchecked.
	fullAddress, err := s.agentRegistry.ResolveAgentAddress(agentAddress)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "AGENT_KEY_ROTATION_FAILED",
			"Failed to rotate agent API key", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	newKey, err := s.agentRegistry.RotateAPIKey(c.Request.Context(), fullAddress)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "AGENT_KEY_ROTATION_FAILED",
			"Failed to rotate agent API key", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"message":   "Agent API key rotated successfully",
		"address":   fullAddress,
		"api_key":   newKey,
		"timestamp": time.Now().UTC(),
	})
}

// handleUpdateAgent handles PATCH /v1/admin/agents/:address
// Updates mutable delivery configuration for an existing agent.
func (s *Server) handleUpdateAgent(c *gin.Context) {
	agentAddress := c.Param("address")

	var updates agents.AgentUpdate
	if err := c.ShouldBindJSON(&updates); err != nil {
		s.respondWithError(c, http.StatusBadRequest, "INVALID_REQUEST_FORMAT",
			"Invalid agent update format", map[string]interface{}{
				"parse_error": err.Error(),
			})
		return
	}

	updated, err := s.agentRegistry.UpdateAgent(c.Request.Context(), agentAddress, &updates)
	if err != nil {
		s.respondWithError(c, http.StatusBadRequest, "AGENT_UPDATE_FAILED",
			"Failed to update agent", map[string]interface{}{
				"error": err.Error(),
			})
		return
	}

	s.respondWithSuccess(c, http.StatusOK, gin.H{
		"message": "Agent updated successfully",
		"agent":   updated,
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
	s.agentRegistry.UpdateLastAccess(c.Request.Context(), recipient)

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
	s.agentRegistry.UpdateLastAccess(c.Request.Context(), recipient)

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
	if !s.agentRegistry.VerifyAPIKey(c.Request.Context(), agentAddress, apiKey) {
		s.respondWithError(c, http.StatusForbidden, "ACCESS_DENIED",
			"Invalid API key for agent", map[string]interface{}{
				"agent": agentAddress,
			})
		return false
	}

	return true
}

// authenticateAgent authenticates a request to a message query endpoint
// using an Agent API key, falling back to the gateway admin key. On success
// it returns the caller's identity: a registered agent's full address
// (agentAddr), or the admin identity (isAdmin), plus true. Agents are scoped
// to messages they sent or received; the admin may inspect any message.
//
// The admin fallback lets operators inspect messages submitted by
// unregistered senders (e.g. routed in from a foreign gateway), which no
// agent key can access since neither sender nor recipient is a registered
// local agent. POST /v1/messages is intentionally public — AMTP is a
// federated protocol where remote senders have no local key — so without the
// fallback such messages would be permanently unreadable.
func (s *Server) authenticateAgent(c *gin.Context) (agentAddr string, isAdmin bool, ok bool) {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		if apiKey == "" {
			s.respondWithError(c, http.StatusUnauthorized, "EMPTY_API_KEY",
				"API key cannot be empty", nil)
			return "", false, false
		}

		// Hash the presented key once and match it against a single listing
		// of agents (the stored API key is already the salted hash), instead
		// of verifying per agent — which reads each agent from storage and
		// re-hashes the same key on every iteration.
		address, ok := s.agentRegistry.AuthenticateAgent(c.Request.Context(), apiKey)
		if ok {
			s.agentRegistry.UpdateLastAccess(c.Request.Context(), address)
			return address, false, true
		}
	}

	// Admin fallback: a valid admin key may inspect any message.
	if s.isAdminRequest(c) {
		return "", true, true
	}

	// No valid credentials. Distinguish "no credentials at all" (401) from
	// "invalid credentials" (403) so clients can tell the cases apart.
	if authHeader == "" && c.GetHeader(s.config.Auth.AdminAPIKeyHeader) == "" {
		s.respondWithError(c, http.StatusUnauthorized, "MISSING_AUTHORIZATION",
			"Agent API key or admin key required", map[string]interface{}{
				"required_header": "Authorization: Bearer <api-key> or " + s.config.Auth.AdminAPIKeyHeader,
			})
	} else {
		s.respondWithError(c, http.StatusForbidden, "ACCESS_DENIED",
			"Invalid API key", nil)
	}
	return "", false, false
}

// isAdminRequest reports whether the request carries a valid gateway admin
// key. The fallback only activates when an admin key file is configured;
// without one there is no admin identity to grant, and message queries stay
// restricted to registered agents.
func (s *Server) isAdminRequest(c *gin.Context) bool {
	auth := s.config.Auth
	if auth.AdminKeyFile == "" {
		return false
	}
	adminKey := c.GetHeader(auth.AdminAPIKeyHeader)
	if adminKey == "" {
		return false
	}
	return middleware.ValidateAdminKey(adminKey, auth.AdminKeyFile)
}

// messageBelongsToAgent reports whether the message was sent by or addressed
// to the given agent address.
func (s *Server) messageBelongsToAgent(message *types.Message, agentAddr string) bool {
	if message == nil {
		return false
	}
	if message.Sender == agentAddr {
		return true
	}
	for _, r := range message.Recipients {
		if r == agentAddr {
			return true
		}
	}
	return false
}

// handleDiscoverAgents handles GET /v1/discovery/agents
// Returns all agents registered on this gateway
func (s *Server) handleDiscoverAgents(c *gin.Context) {
	// Get query parameters for filtering
	deliveryMode := c.Query("delivery_mode")       // filter by "push" or "pull"
	activeOnly := c.Query("active_only") == "true" // only show recently active agents

	agents := make([]gin.H, 0)

	// Get agents from the agent registry
	localAgents := s.agentRegistry.GetAllAgents(c.Request.Context())

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
