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
	"encoding/json"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestValidateMessage(t *testing.T) {
	validator := New(10 * 1024 * 1024) // 10MB limit

	// Valid message
	validMessage := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	err := validator.ValidateMessage(validMessage)
	if err != nil {
		t.Errorf("Valid message should pass validation: %v", err)
	}

	// Test missing required fields
	invalidMessage := &types.Message{}
	err = validator.ValidateMessage(invalidMessage)
	if err == nil {
		t.Error("Message with missing required fields should fail validation")
	}

	// Test invalid version
	invalidVersion := *validMessage
	invalidVersion.Version = "2.0"
	err = validator.ValidateMessage(&invalidVersion)
	if err == nil {
		t.Error("Message with invalid version should fail validation")
	}

	// Test invalid email format
	invalidEmail := *validMessage
	invalidEmail.Sender = "invalid-email"
	err = validator.ValidateMessage(&invalidEmail)
	if err == nil {
		t.Error("Message with invalid sender email should fail validation")
	}
}

func TestValidateSendRequest(t *testing.T) {
	validator := New(10 * 1024 * 1024)

	// Valid request
	validRequest := &types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@example.com"},
		Subject:    "Test Message",
		Payload:    json.RawMessage(`{"test": "data"}`),
	}

	err := validator.ValidateSendRequest(validRequest)
	if err != nil {
		t.Errorf("Valid request should pass validation: %v", err)
	}

	// Test missing sender
	invalidRequest := &types.SendMessageRequest{
		Recipients: []string{"recipient@example.com"},
	}
	err = validator.ValidateSendRequest(invalidRequest)
	if err == nil {
		t.Error("Request with missing sender should fail validation")
	}

	// Test empty recipients
	emptyRecipients := *validRequest
	emptyRecipients.Recipients = []string{}
	err = validator.ValidateSendRequest(&emptyRecipients)
	if err == nil {
		t.Error("Request with empty recipients should fail validation")
	}
}

func TestValidateCoordination(t *testing.T) {
	validator := New(10 * 1024 * 1024)

	// Valid parallel coordination
	parallelCoord := &types.CoordinationConfig{
		Type:    "parallel",
		Timeout: 3600,
	}
	err := validator.validateCoordination(parallelCoord)
	if err != nil {
		t.Errorf("Valid parallel coordination should pass: %v", err)
	}

	// Valid sequential coordination
	sequentialCoord := &types.CoordinationConfig{
		Type:     "sequential",
		Timeout:  3600,
		Sequence: []string{"first@example.com", "second@example.com"},
	}
	err = validator.validateCoordination(sequentialCoord)
	if err != nil {
		t.Errorf("Valid sequential coordination should pass: %v", err)
	}

	// Invalid coordination type
	invalidType := &types.CoordinationConfig{
		Type:    "invalid",
		Timeout: 3600,
	}
	err = validator.validateCoordination(invalidType)
	if err == nil {
		t.Error("Invalid coordination type should fail validation")
	}

	// Sequential without sequence
	sequentialNoSeq := &types.CoordinationConfig{
		Type:    "sequential",
		Timeout: 3600,
	}
	err = validator.validateCoordination(sequentialNoSeq)
	if err == nil {
		t.Error("Sequential coordination without sequence should fail validation")
	}
}

func TestValidateAttachments(t *testing.T) {
	validator := New(10 * 1024 * 1024)

	// Valid attachment
	validAttachments := []types.Attachment{
		{
			Filename:    "test.pdf",
			ContentType: "application/pdf",
			Size:        1024,
			Hash:        "sha256:abcdef1234567890",
			URL:         "https://example.com/files/test.pdf",
		},
	}
	err := validator.validateAttachments(validAttachments)
	if err != nil {
		t.Errorf("Valid attachments should pass: %v", err)
	}

	// Invalid URL
	invalidURL := []types.Attachment{
		{
			Filename:    "test.pdf",
			ContentType: "application/pdf",
			Size:        1024,
			Hash:        "sha256:abcdef1234567890",
			URL:         "invalid-url",
		},
	}
	err = validator.validateAttachments(invalidURL)
	if err == nil {
		t.Error("Invalid URL should fail validation")
	}

	// Invalid hash format
	invalidHash := []types.Attachment{
		{
			Filename:    "test.pdf",
			ContentType: "application/pdf",
			Size:        1024,
			Hash:        "invalid-hash",
			URL:         "https://example.com/files/test.pdf",
		},
	}
	err = validator.validateAttachments(invalidHash)
	if err == nil {
		t.Error("Invalid hash format should fail validation")
	}
}

func TestValidateSchemaFormat(t *testing.T) {
	validator := New(10 * 1024 * 1024)

	// Valid schema formats
	validSchemas := []string{
		"agntcy:commerce.order.v1",
		"agntcy:finance.payment.v2",
		"agntcy:logistics.shipment.v10",
	}

	for _, schema := range validSchemas {
		err := validator.validateSchemaFormat(schema)
		if err != nil {
			t.Errorf("Valid schema %s should pass validation: %v", schema, err)
		}
	}

	// Invalid schema formats
	invalidSchemas := []string{
		"commerce.order.v1",       // missing agntcy prefix
		"agntcy:commerce.order",   // missing version
		"agntcy:commerce.order.1", // invalid version format
		"agntcy:commerce",         // missing entity and version
		"invalid-schema",          // completely invalid
	}

	for _, schema := range invalidSchemas {
		err := validator.validateSchemaFormat(schema)
		if err == nil {
			t.Errorf("Invalid schema %s should fail validation", schema)
		}
	}
}

func TestIsValidEmail(t *testing.T) {
	validator := New(10 * 1024 * 1024)

	validEmails := []string{
		"test@example.com",
		"user.name@domain.org",
		"agent+tag@company.co.uk",
	}

	for _, email := range validEmails {
		if !validator.isValidEmail(email) {
			t.Errorf("Valid email %s should pass validation", email)
		}
	}

	invalidEmails := []string{
		"invalid-email",
		"@domain.com",
		"user@",
		"",
	}

	for _, email := range invalidEmails {
		if validator.isValidEmail(email) {
			t.Errorf("Invalid email %s should fail validation", email)
		}
	}
}

// MockAgentManager implements AgentManager for testing
type MockAgentManager struct {
	agents map[string]*LocalAgent
}

func NewMockAgentManager() *MockAgentManager {
	return &MockAgentManager{
		agents: make(map[string]*LocalAgent),
	}
}

func (m *MockAgentManager) AddAgent(address string, schemas []string) {
	m.agents[address] = &LocalAgent{
		Address:          address,
		SupportedSchemas: schemas,
		RequiresSchema:   len(schemas) > 0, // Auto-determine based on schemas
	}
}

func (m *MockAgentManager) GetLocalAgents() map[string]*LocalAgent {
	return m.agents
}

func TestNewWithAgentManager(t *testing.T) {
	agentManager := NewMockAgentManager()
	validator := NewWithAgentManager(10*1024*1024, nil, agentManager)

	if validator.agentManager != agentManager {
		t.Error("Validator should have the provided agent manager")
	}
}

func TestValidateMessageWithContext_NoAgentManager(t *testing.T) {
	// Test with validator that has no agent manager
	validator := New(10 * 1024 * 1024)

	message := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Schema:         "agntcy:commerce.order.v1",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	err := validator.ValidateMessageWithContext(context.Background(), message)
	if err != nil {
		t.Errorf("Message should pass validation when no agent manager is present: %v", err)
	}
}

func TestValidateMessageWithContext_AgentSchemaSupport(t *testing.T) {
	agentManager := NewMockAgentManager()
	validator := NewWithAgentManager(10*1024*1024, nil, agentManager)

	// Add agents with different schema support
	agentManager.AddAgent("agent1@example.com", []string{"agntcy:commerce.order.v1", "agntcy:finance.payment.v1"})
	agentManager.AddAgent("agent2@example.com", []string{"agntcy:commerce.*"})
	agentManager.AddAgent("agent3@example.com", []string{}) // No schemas specified (supports all)

	tests := []struct {
		name        string
		schema      string
		recipients  []string
		shouldPass  bool
		description string
	}{
		{
			name:        "exact_schema_match",
			schema:      "agntcy:commerce.order.v1",
			recipients:  []string{"agent1@example.com"},
			shouldPass:  true,
			description: "Agent explicitly supports the schema",
		},
		{
			name:        "wildcard_schema_match",
			schema:      "agntcy:commerce.order.v2",
			recipients:  []string{"agent2@example.com"},
			shouldPass:  true,
			description: "Agent supports schema via wildcard",
		},
		{
			name:        "no_schemas_specified_supports_all",
			schema:      "agntcy:logistics.shipment.v1",
			recipients:  []string{"agent3@example.com"},
			shouldPass:  true,
			description: "Agent with no schemas specified supports all",
		},
		{
			name:        "multiple_recipients_one_supports",
			schema:      "agntcy:commerce.order.v1",
			recipients:  []string{"external@other.com", "agent1@example.com"},
			shouldPass:  true,
			description: "At least one local agent supports the schema",
		},
		{
			name:        "no_local_agents_support_schema",
			schema:      "agntcy:logistics.shipment.v1",
			recipients:  []string{"agent1@example.com", "agent2@example.com"},
			shouldPass:  false,
			description: "No local agents support the schema",
		},
		{
			name:        "only_external_recipients",
			schema:      "agntcy:logistics.shipment.v1",
			recipients:  []string{"external1@other.com", "external2@other.com"},
			shouldPass:  true,
			description: "Only external recipients, no local validation needed",
		},
		{
			name:        "no_schema_specified_to_schema_optional_agent",
			schema:      "",
			recipients:  []string{"agent3@example.com"}, // agent3 has no schemas, so accepts unstructured
			shouldPass:  true,
			description: "No schema specified should pass for agents that don't require schemas",
		},
		{
			name:        "no_schema_specified_to_schema_required_agent",
			schema:      "",
			recipients:  []string{"agent1@example.com"}, // agent1 has schemas, so requires them
			shouldPass:  false,
			description: "No schema specified should fail for agents that require schemas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := &types.Message{
				Version:        "1.0",
				MessageID:      "01234567-89ab-7def-8123-456789abcdef",
				IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
				Timestamp:      time.Now(),
				Sender:         "test@example.com",
				Recipients:     tt.recipients,
				Subject:        "Test Message",
				Schema:         tt.schema,
				Payload:        json.RawMessage(`{"test": "data"}`),
			}

			err := validator.ValidateMessageWithContext(context.Background(), message)

			if tt.shouldPass && err != nil {
				t.Errorf("Test %s: Expected validation to pass but got error: %v", tt.description, err)
			}

			if !tt.shouldPass && err == nil {
				t.Errorf("Test %s: Expected validation to fail but it passed", tt.description)
			}
		})
	}
}

func TestAgentSupportsSchema(t *testing.T) {
	tests := []struct {
		name             string
		supportedSchemas []string
		messageSchema    string
		shouldSupport    bool
	}{
		{
			name:             "exact_match",
			supportedSchemas: []string{"agntcy:commerce.order.v1"},
			messageSchema:    "agntcy:commerce.order.v1",
			shouldSupport:    true,
		},
		{
			name:             "wildcard_match_domain",
			supportedSchemas: []string{"agntcy:commerce.*"},
			messageSchema:    "agntcy:commerce.order.v1",
			shouldSupport:    true,
		},
		{
			name:             "wildcard_match_domain_entity",
			supportedSchemas: []string{"agntcy:commerce.order.*"},
			messageSchema:    "agntcy:commerce.order.v2",
			shouldSupport:    true,
		},
		{
			name:             "no_match",
			supportedSchemas: []string{"agntcy:finance.payment.v1"},
			messageSchema:    "agntcy:commerce.order.v1",
			shouldSupport:    false,
		},
		{
			name:             "partial_wildcard_no_match",
			supportedSchemas: []string{"agntcy:finance.*"},
			messageSchema:    "agntcy:commerce.order.v1",
			shouldSupport:    false,
		},
		{
			name:             "empty_schemas_no_schema_required_supports_all",
			supportedSchemas: []string{},
			messageSchema:    "agntcy:commerce.order.v1",
			shouldSupport:    true,
		},
		{
			name:             "empty_schemas_no_schema_required_supports_unstructured",
			supportedSchemas: []string{},
			messageSchema:    "",
			shouldSupport:    true,
		},
		{
			name:             "multiple_schemas_one_matches",
			supportedSchemas: []string{"agntcy:finance.*", "agntcy:commerce.order.v1", "agntcy:logistics.*"},
			messageSchema:    "agntcy:commerce.order.v1",
			shouldSupport:    true,
		},
		{
			name:             "schema_required_agent_rejects_unstructured",
			supportedSchemas: []string{"agntcy:commerce.order.v1"},
			messageSchema:    "",
			shouldSupport:    false,
		},
	}

	agentManager := NewMockAgentManager()
	validator := NewWithAgentManager(10*1024*1024, nil, agentManager)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &LocalAgent{
				Address:          "test@example.com",
				SupportedSchemas: tt.supportedSchemas,
				RequiresSchema:   len(tt.supportedSchemas) > 0, // Auto-determine based on schemas
			}

			result := validator.agentSupportsSchema(agent, tt.messageSchema)

			if result != tt.shouldSupport {
				t.Errorf("Expected agentSupportsSchema to return %v, got %v", tt.shouldSupport, result)
			}
		})
	}
}

func TestValidateAgentSchemaSupport_EdgeCases(t *testing.T) {
	agentManager := NewMockAgentManager()
	validator := NewWithAgentManager(10*1024*1024, nil, agentManager)

	// Test with nil message
	err := validator.validateAgentSchemaSupport(nil)
	if err == nil {
		t.Error("validateAgentSchemaSupport should return error for nil message")
	}

	// Test with message with no recipients
	message := &types.Message{
		Version:   "1.0",
		MessageID: "01234567-89ab-7def-8123-456789abcdef",
		Schema:    "agntcy:commerce.order.v1",
	}

	err = validator.validateAgentSchemaSupport(message)
	if err != nil {
		t.Errorf("validateAgentSchemaSupport should pass for message with no recipients: %v", err)
	}
}

func BenchmarkValidateMessage(b *testing.B) {
	validator := New(10 * 1024 * 1024)
	message := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := validator.ValidateMessage(message)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateMessageWithContext(b *testing.B) {
	agentManager := NewMockAgentManager()
	agentManager.AddAgent("agent@example.com", []string{"agntcy:commerce.*"})

	validator := NewWithAgentManager(10*1024*1024, nil, agentManager)
	message := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"agent@example.com"},
		Subject:        "Test Message",
		Schema:         "agntcy:commerce.order.v1",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := validator.ValidateMessageWithContext(context.Background(), message)
		if err != nil {
			b.Fatal(err)
		}
	}
}
