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

package validation

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/pkg/uuid"
)

// LocalAgent represents a local agent for validation purposes
type LocalAgent struct {
	Address          string   `json:"address"`
	SupportedSchemas []string `json:"supported_schemas"`
	RequiresSchema   bool     `json:"requires_schema"` // whether this agent requires schema validation
}

// AgentManager interface for agent-related operations
type AgentManager interface {
	GetLocalAgents() map[string]*LocalAgent
}

// Validator provides message validation functionality
type Validator struct {
	maxMessageSize int64
	schemaManager  *schema.Manager
	agentManager   AgentManager
}

// New creates a new validator with the given configuration
func New(maxMessageSize int64) *Validator {
	return &Validator{
		maxMessageSize: maxMessageSize,
	}
}

// NewWithSchemaManager creates a new validator with schema management capabilities
func NewWithSchemaManager(maxMessageSize int64, schemaManager *schema.Manager) *Validator {
	return &Validator{
		maxMessageSize: maxMessageSize,
		schemaManager:  schemaManager,
	}
}

// NewWithAgentManager creates a new validator with agent management capabilities
func NewWithAgentManager(maxMessageSize int64, schemaManager *schema.Manager, agentManager AgentManager) *Validator {
	return &Validator{
		maxMessageSize: maxMessageSize,
		schemaManager:  schemaManager,
		agentManager:   agentManager,
	}
}

// ValidateMessage validates an AMTP message according to the protocol specification
func (v *Validator) ValidateMessage(msg *types.Message) error {
	return v.ValidateMessageWithContext(context.Background(), msg)
}

// ValidateMessageWithContext validates an AMTP message with context support
func (v *Validator) ValidateMessageWithContext(ctx context.Context, msg *types.Message) error {
	// Check message size
	if msg.Size() > v.maxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum allowed size %d", msg.Size(), v.maxMessageSize)
	}

	// Validate required fields
	if err := v.validateRequiredFields(msg); err != nil {
		return fmt.Errorf("required field validation failed: %w", err)
	}

	// Validate field formats
	if err := v.validateFieldFormats(msg); err != nil {
		return fmt.Errorf("field format validation failed: %w", err)
	}

	// Validate coordination if present
	if msg.Coordination != nil {
		if err := v.validateCoordination(msg.Coordination); err != nil {
			return fmt.Errorf("coordination validation failed: %w", err)
		}
	}

	// Validate attachments if present
	if len(msg.Attachments) > 0 {
		if err := v.validateAttachments(msg.Attachments); err != nil {
			return fmt.Errorf("attachment validation failed: %w", err)
		}
	}

	// Validate that target agents support the message schema (or lack thereof)
	if v.agentManager != nil {
		if err := v.validateAgentSchemaSupport(msg); err != nil {
			return fmt.Errorf("agent schema validation failed: %w", err)
		}
	}

	// Perform schema validation if schema manager is available and message has schema
	if v.schemaManager != nil && msg.Schema != "" {
		if err := v.validateWithSchemaManager(ctx, msg); err != nil {
			return fmt.Errorf("schema validation failed: %w", err)
		}
	}

	return nil
}

// validateWithSchemaManager performs schema validation using the schema manager
func (v *Validator) validateWithSchemaManager(ctx context.Context, msg *types.Message) error {
	report, err := v.schemaManager.ValidateMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("schema manager validation failed: %w", err)
	}

	if !report.IsValid() {
		// Convert detailed errors to simple error message
		if len(report.Errors) > 0 {
			return fmt.Errorf("schema validation failed: %s", report.Errors[0].Message)
		}
		return fmt.Errorf("schema validation failed")
	}

	return nil
}

// validateAgentSchemaSupport validates that at least one target agent supports the message schema
func (v *Validator) validateAgentSchemaSupport(msg *types.Message) error {
	if msg == nil {
		return fmt.Errorf("message cannot be nil")
	}

	if len(msg.Recipients) == 0 {
		return nil // No recipients to validate
	}

	localAgents := v.agentManager.GetLocalAgents()

	// Check each recipient to see if any local agent supports the schema
	for _, recipient := range msg.Recipients {
		agent, exists := localAgents[recipient]
		if !exists {
			// Agent not registered locally - assume it can handle the schema
			// (external agents or unregistered agents get benefit of doubt)
			continue
		}

		// Check if this agent supports the message schema
		if v.agentSupportsSchema(agent, msg.Schema) {
			return nil // At least one agent supports it
		}
	}

	// Check if any recipients are local agents that don't support the schema
	unsupportedAgents := make([]string, 0)
	for _, recipient := range msg.Recipients {
		if agent, exists := localAgents[recipient]; exists {
			if !v.agentSupportsSchema(agent, msg.Schema) {
				unsupportedAgents = append(unsupportedAgents, recipient)
			}
		}
	}

	if len(unsupportedAgents) > 0 {
		return fmt.Errorf("agents do not support schema '%s': %v", msg.Schema, unsupportedAgents)
	}

	return nil
}

// agentSupportsSchema checks if an agent supports a specific schema
func (v *Validator) agentSupportsSchema(agent *LocalAgent, messageSchema string) bool {
	// If agent doesn't require schema validation, it accepts all messages (including unstructured)
	if !agent.RequiresSchema {
		return true
	}

	// If agent requires schema but message has no schema, reject
	if messageSchema == "" {
		return false
	}

	// Check for exact match or wildcard match
	for _, supportedSchema := range agent.SupportedSchemas {
		if supportedSchema == messageSchema {
			return true
		}

		// Check wildcard patterns (e.g., "agntcy:commerce.*")
		if strings.HasSuffix(supportedSchema, "*") {
			prefix := strings.TrimSuffix(supportedSchema, "*")
			if strings.HasPrefix(messageSchema, prefix) {
				return true
			}
		}
	}
	return false
}

// ValidateSendRequest validates a send message request
func (v *Validator) ValidateSendRequest(req *types.SendMessageRequest) error {
	if req.Sender == "" {
		return fmt.Errorf("sender is required")
	}

	if !v.isValidEmail(req.Sender) {
		return fmt.Errorf("invalid sender email format: %s", req.Sender)
	}

	if len(req.Recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	for _, recipient := range req.Recipients {
		if !v.isValidEmail(recipient) {
			return fmt.Errorf("invalid recipient email format: %s", recipient)
		}
	}

	// Validate coordination if present
	if req.Coordination != nil {
		if err := v.validateCoordination(req.Coordination); err != nil {
			return fmt.Errorf("coordination validation failed: %w", err)
		}
	}

	// Validate attachments if present
	if len(req.Attachments) > 0 {
		if err := v.validateAttachments(req.Attachments); err != nil {
			return fmt.Errorf("attachment validation failed: %w", err)
		}
	}

	return nil
}

// validateRequiredFields validates that all required fields are present
func (v *Validator) validateRequiredFields(msg *types.Message) error {
	if msg.Version == "" {
		return fmt.Errorf("version is required")
	}

	if msg.MessageID == "" {
		return fmt.Errorf("message_id is required")
	}

	if msg.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key is required")
	}

	if msg.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}

	if msg.Sender == "" {
		return fmt.Errorf("sender is required")
	}

	if len(msg.Recipients) == 0 {
		return fmt.Errorf("recipients are required")
	}

	return nil
}

// validateFieldFormats validates the format of various fields
func (v *Validator) validateFieldFormats(msg *types.Message) error {
	// Validate version
	if msg.Version != "1.0" {
		return fmt.Errorf("unsupported protocol version: %s", msg.Version)
	}

	// Validate message ID (should be UUIDv7)
	if !uuid.IsValidV7(msg.MessageID) {
		return fmt.Errorf("invalid message_id format, must be UUIDv7: %s", msg.MessageID)
	}

	// Validate idempotency key (should be UUIDv4)
	if !uuid.IsValidV4(msg.IdempotencyKey) {
		return fmt.Errorf("invalid idempotency_key format, must be UUIDv4: %s", msg.IdempotencyKey)
	}

	// Validate sender email
	if !v.isValidEmail(msg.Sender) {
		return fmt.Errorf("invalid sender email format: %s", msg.Sender)
	}

	// Validate recipient emails
	for _, recipient := range msg.Recipients {
		if !v.isValidEmail(recipient) {
			return fmt.Errorf("invalid recipient email format: %s", recipient)
		}
	}

	// Validate in_reply_to if present
	if msg.InReplyTo != "" && !uuid.IsValidV7(msg.InReplyTo) {
		return fmt.Errorf("invalid in_reply_to format, must be UUIDv7: %s", msg.InReplyTo)
	}

	// Validate schema format if present
	if msg.Schema != "" {
		if err := v.validateSchemaFormat(msg.Schema); err != nil {
			return fmt.Errorf("invalid schema format: %w", err)
		}
	}

	return nil
}

// validateCoordination validates coordination configuration
func (v *Validator) validateCoordination(coord *types.CoordinationConfig) error {
	// Validate coordination type
	switch coord.Type {
	case "parallel", "sequential", "conditional":
		// Valid types
	default:
		return fmt.Errorf("invalid coordination type: %s", coord.Type)
	}

	// Validate timeout
	if coord.Timeout <= 0 {
		return fmt.Errorf("coordination timeout must be positive")
	}

	// Type-specific validation
	switch coord.Type {
	case "sequential":
		if len(coord.Sequence) == 0 {
			return fmt.Errorf("sequence is required for sequential coordination")
		}
		for _, addr := range coord.Sequence {
			if !v.isValidEmail(addr) {
				return fmt.Errorf("invalid email in sequence: %s", addr)
			}
		}

	case "conditional":
		if len(coord.Conditions) == 0 {
			return fmt.Errorf("conditions are required for conditional coordination")
		}
		for i, condition := range coord.Conditions {
			if condition.If == "" {
				return fmt.Errorf("condition %d: 'if' clause is required", i)
			}
			if len(condition.Then) == 0 {
				return fmt.Errorf("condition %d: 'then' clause is required", i)
			}
			for _, addr := range condition.Then {
				if !v.isValidEmail(addr) {
					return fmt.Errorf("condition %d: invalid email in 'then' clause: %s", i, addr)
				}
			}
			for _, addr := range condition.Else {
				if !v.isValidEmail(addr) {
					return fmt.Errorf("condition %d: invalid email in 'else' clause: %s", i, addr)
				}
			}
		}
	}

	// Validate required and optional responses
	for _, addr := range coord.RequiredResponses {
		if !v.isValidEmail(addr) {
			return fmt.Errorf("invalid email in required_responses: %s", addr)
		}
	}

	for _, addr := range coord.OptionalResponses {
		if !v.isValidEmail(addr) {
			return fmt.Errorf("invalid email in optional_responses: %s", addr)
		}
	}

	return nil
}

// validateAttachments validates attachment references
func (v *Validator) validateAttachments(attachments []types.Attachment) error {
	for i, attachment := range attachments {
		if attachment.Filename == "" {
			return fmt.Errorf("attachment %d: filename is required", i)
		}

		if attachment.ContentType == "" {
			return fmt.Errorf("attachment %d: content_type is required", i)
		}

		if attachment.Size < 0 {
			return fmt.Errorf("attachment %d: size cannot be negative", i)
		}

		if attachment.Hash == "" {
			return fmt.Errorf("attachment %d: hash is required", i)
		}

		if attachment.URL == "" {
			return fmt.Errorf("attachment %d: URL is required", i)
		}

		// Validate URL format
		if !v.isValidURL(attachment.URL) {
			return fmt.Errorf("attachment %d: invalid URL format: %s", i, attachment.URL)
		}

		// Validate hash format (should be sha256:...)
		if !v.isValidHashFormat(attachment.Hash) {
			return fmt.Errorf("attachment %d: invalid hash format: %s", i, attachment.Hash)
		}
	}

	return nil
}

// isValidEmail validates email address format
func (v *Validator) isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// isValidURL validates URL format (basic validation)
func (v *Validator) isValidURL(url string) bool {
	// Basic URL validation - starts with http:// or https://
	urlRegex := regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)
	return urlRegex.MatchString(url)
}

// isValidHashFormat validates hash format (algorithm:value)
func (v *Validator) isValidHashFormat(hash string) bool {
	// Should be in format "algorithm:hexvalue"
	hashRegex := regexp.MustCompile(`^(sha256|sha512|md5):[a-fA-F0-9]+$`)
	return hashRegex.MatchString(hash)
}

// validateSchemaFormat validates AGNTCY schema identifier format
func (v *Validator) validateSchemaFormat(schema string) error {
	// AGNTCY schema format: agntcy:domain.entity.version
	schemaRegex := regexp.MustCompile(`^agntcy:[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.v[0-9]+$`)
	if !schemaRegex.MatchString(schema) {
		return fmt.Errorf("schema must be in format 'agntcy:domain.entity.version': %s", schema)
	}

	return nil
}
