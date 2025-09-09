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

package agents

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
)

// MockSchemaManager for testing
type MockSchemaManager struct{}

func (m *MockSchemaManager) GetSchema(ctx context.Context, id schema.SchemaIdentifier) (*schema.Schema, error) {
	// For testing, assume all schemas exist
	return &schema.Schema{
		ID: id,
	}, nil
}

func (m *MockSchemaManager) ListSchemas(ctx context.Context, pattern string) ([]schema.SchemaIdentifier, error) {
	return []schema.SchemaIdentifier{}, nil
}

func NewMockSchemaManager() *MockSchemaManager {
	return &MockSchemaManager{}
}

func createTestRegistry() *Registry {
	config := RegistryConfig{
		LocalDomain:   "localhost",
		SchemaManager: NewMockSchemaManager(),
	}
	return NewRegistry(config)
}

// Test agent API key generation
func TestGenerateAPIKey(t *testing.T) {
	registry := createTestRegistry()

	// Generate multiple keys to ensure uniqueness
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key, err := registry.GenerateAPIKey()
		if err != nil {
			t.Fatalf("Failed to generate API key: %v", err)
		}

		// Check key format (should be base64 URL-safe)
		if len(key) == 0 {
			t.Error("Generated key is empty")
		}

		// Check uniqueness
		if keys[key] {
			t.Errorf("Generated duplicate key: %s", key)
		}
		keys[key] = true

		// Verify it's valid base64
		if _, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(key); err != nil {
			t.Errorf("Generated key is not valid base64: %s, error: %v", key, err)
		}
	}
}

// Test agent API key verification
func TestVerifyAPIKey(t *testing.T) {
	registry := createTestRegistry()

	// Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Get the registered agent to get the generated API key
	registeredAgent, err := registry.GetAgent(agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}

	validKey := registeredAgent.APIKey
	if validKey == "" {
		t.Fatal("Agent API key is empty")
	}

	// Test valid key verification
	if !registry.VerifyAPIKey(agent.Address, validKey) {
		t.Error("Valid API key verification failed")
	}

	// Test invalid key verification
	if registry.VerifyAPIKey(agent.Address, "invalid-key") {
		t.Error("Invalid API key verification should fail")
	}

	// Test non-existent agent
	if registry.VerifyAPIKey("nonexistent@localhost", validKey) {
		t.Error("API key verification for non-existent agent should fail")
	}

	// Test empty key
	if registry.VerifyAPIKey(agent.Address, "") {
		t.Error("Empty API key verification should fail")
	}

	// Test similar but different key (timing attack protection)
	similarKey := validKey[:len(validKey)-1] + "X" // Change last character
	if registry.VerifyAPIKey(agent.Address, similarKey) {
		t.Error("Similar but different API key verification should fail")
	}
}

// Test agent API key rotation
func TestRotateAPIKey(t *testing.T) {
	registry := createTestRegistry()

	// Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Get original API key
	originalAgent, err := registry.GetAgent(agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}
	originalKey := originalAgent.APIKey

	// Rotate the API key
	newKey, err := registry.RotateAPIKey(agent.Address)
	if err != nil {
		t.Fatalf("Failed to rotate API key: %v", err)
	}

	// Verify new key is different
	if newKey == originalKey {
		t.Error("Rotated API key should be different from original")
	}

	// Verify old key no longer works
	if registry.VerifyAPIKey(agent.Address, originalKey) {
		t.Error("Original API key should no longer work after rotation")
	}

	// Verify new key works
	if !registry.VerifyAPIKey(agent.Address, newKey) {
		t.Error("New API key should work after rotation")
	}

	// Test rotation for non-existent agent
	_, err = registry.RotateAPIKey("nonexistent@localhost")
	if err == nil {
		t.Error("Rotating API key for non-existent agent should fail")
	}
}

// Test agent last access update
func TestUpdateLastAccess(t *testing.T) {
	registry := createTestRegistry()

	// Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Get initial last access time
	initialAgent, err := registry.GetAgent(agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}
	initialLastAccess := initialAgent.LastAccess

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Update last access
	registry.UpdateLastAccess(agent.Address)

	// Verify last access was updated
	updatedAgent, err := registry.GetAgent(agent.Address)
	if err != nil {
		t.Fatalf("Failed to get updated agent: %v", err)
	}

	if !updatedAgent.LastAccess.After(initialLastAccess) {
		t.Error("Last access time should be updated")
	}

	// Test updating non-existent agent (should not panic)
	registry.UpdateLastAccess("nonexistent@localhost")
}

// Test agent registration with API key generation
func TestRegisterAgentAPIKeyGeneration(t *testing.T) {
	registry := createTestRegistry()

	tests := []struct {
		name        string
		agent       *LocalAgent
		expectError bool
	}{
		{
			name: "valid agent without API key",
			agent: &LocalAgent{
				Address:      "test1",
				DeliveryMode: "pull",
			},
			expectError: false,
		},
		{
			name: "valid agent with API key",
			agent: &LocalAgent{
				Address:      "test2",
				DeliveryMode: "push",
				PushTarget:   "http://example.com/webhook",
				APIKey:       "custom-api-key",
			},
			expectError: false,
		},
		{
			name: "invalid agent - empty address",
			agent: &LocalAgent{
				Address:      "",
				DeliveryMode: "pull",
			},
			expectError: true,
		},
		{
			name: "invalid agent - invalid delivery mode",
			agent: &LocalAgent{
				Address:      "test3",
				DeliveryMode: "invalid",
			},
			expectError: true,
		},
		{
			name: "invalid agent - push mode without target",
			agent: &LocalAgent{
				Address:      "test4",
				DeliveryMode: "push",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.RegisterAgent(tt.agent)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify agent was registered
			registeredAgent, err := registry.GetAgent(tt.agent.Address)
			if err != nil {
				t.Fatalf("Failed to get registered agent: %v", err)
			}

			// Verify API key was generated if not provided
			if tt.agent.APIKey == "" && registeredAgent.APIKey == "" {
				t.Error("API key should be generated if not provided")
			}

			// Verify custom API key was preserved
			if tt.agent.APIKey != "" && registeredAgent.APIKey != tt.agent.APIKey {
				t.Error("Custom API key should be preserved")
			}

			// Verify timestamps were set
			if registeredAgent.CreatedAt.IsZero() {
				t.Error("CreatedAt timestamp should be set")
			}

			if registeredAgent.LastAccess.IsZero() {
				t.Error("LastAccess timestamp should be set")
			}
		})
	}
}

// Test registry statistics
func TestGetStats(t *testing.T) {
	registry := createTestRegistry()

	// Register some test agents to verify stats
	agent1 := &LocalAgent{
		Address:      "test1",
		DeliveryMode: "push",
		PushTarget:   "http://example.com/webhook1",
	}

	agent2 := &LocalAgent{
		Address:      "test2",
		DeliveryMode: "pull",
	}

	agent3 := &LocalAgent{
		Address:      "test3",
		DeliveryMode: "push",
		PushTarget:   "http://example.com/webhook3",
	}

	// Register agents
	if err := registry.RegisterAgent(agent1); err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}
	if err := registry.RegisterAgent(agent2); err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}
	if err := registry.RegisterAgent(agent3); err != nil {
		t.Fatalf("Failed to register agent3: %v", err)
	}

	// Add some messages to inbox for pull agent
	testMessage := &types.Message{
		MessageID: "test-msg-1",
		Sender:    "sender@example.com",
		Subject:   "Test Message",
	}
	// Use the full address that was generated by the registry
	registeredAgent2, err := registry.GetAgent(agent2.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent2: %v", err)
	}
	if err := registry.StoreMessage(registeredAgent2.Address, testMessage); err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Get stats
	stats := registry.GetStats()

	// Verify stats
	if stats["local_agents"] != 3 {
		t.Errorf("Expected 3 local agents, got %v", stats["local_agents"])
	}

	if stats["push_agents"] != 2 {
		t.Errorf("Expected 2 push agents, got %v", stats["push_agents"])
	}

	if stats["pull_agents"] != 1 {
		t.Errorf("Expected 1 pull agent, got %v", stats["pull_agents"])
	}

	// Note: total_inbox_messages is no longer tracked by AgentRegistry
	// since inbox storage is now handled by unified message storage
	if _, exists := stats["total_inbox_messages"]; exists {
		t.Errorf("total_inbox_messages should not be present in stats (handled by unified storage)")
	}
}

// Test inbox functionality (deprecated methods - now handled by unified storage)
func TestInboxOperations(t *testing.T) {
	registry := createTestRegistry()

	recipient := "test@localhost"

	// Test getting messages from empty inbox (should return empty since it's deprecated)
	messages := registry.GetInboxMessages(recipient)
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages from deprecated GetInboxMessages, got %d", len(messages))
	}

	// Store a message (should be no-op now)
	testMessage1 := &types.Message{
		MessageID: "test-msg-1",
		Sender:    "sender@example.com",
		Subject:   "Test Message 1",
	}

	err := registry.StoreMessage(recipient, testMessage1)
	if err != nil {
		t.Fatalf("StoreMessage should not fail (it's a no-op): %v", err)
	}

	// Get messages (should still return empty since storage is deprecated)
	messages = registry.GetInboxMessages(recipient)
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages from deprecated GetInboxMessages after store, got %d", len(messages))
	}

	// Acknowledge message (should return error indicating it's deprecated)
	err = registry.AcknowledgeMessage(recipient, "test-msg-1")
	if err == nil {
		t.Error("Expected error from deprecated AcknowledgeMessage method")
	}

	expectedError := "acknowledgment should be handled by unified message storage"
	if err.Error() != expectedError {
		t.Errorf("Expected error message '%s', got '%s'", expectedError, err.Error())
	}
}

// Test agent unregistration
func TestUnregisterAgent(t *testing.T) {
	registry := createTestRegistry()

	// Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Verify agent exists
	_, err = registry.GetAgent(agent.Address)
	if err != nil {
		t.Fatalf("Agent should exist after registration: %v", err)
	}

	// Get the registered agent to get the full address
	registeredAgent, err := registry.GetAgent(agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}

	// Unregister agent using the agent name (not full address)
	err = registry.UnregisterAgent("test") // Use agent name, not full address
	if err != nil {
		t.Fatalf("Failed to unregister agent: %v", err)
	}

	// Verify agent no longer exists
	_, err = registry.GetAgent(registeredAgent.Address)
	if err == nil {
		t.Error("Agent should not exist after unregistration")
	}

	// Test unregistering non-existent agent
	err = registry.UnregisterAgent("non-existent")
	if err == nil {
		t.Error("Expected error when unregistering non-existent agent")
	}
}

// Test getting all agents
func TestGetAllAgents(t *testing.T) {
	registry := createTestRegistry()

	// Initially should be empty
	agents := registry.GetAllAgents()
	if len(agents) != 0 {
		t.Errorf("Expected 0 agents initially, got %d", len(agents))
	}

	// Register some agents
	agent1 := &LocalAgent{
		Address:      "test1",
		DeliveryMode: "pull",
	}
	agent2 := &LocalAgent{
		Address:      "test2",
		DeliveryMode: "push",
		PushTarget:   "http://example.com/webhook",
	}

	if err := registry.RegisterAgent(agent1); err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}
	if err := registry.RegisterAgent(agent2); err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}

	// Get all agents
	agents = registry.GetAllAgents()
	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}

	// Verify agents are returned correctly
	if agents[agent1.Address] == nil {
		t.Error("Agent1 should be in the returned map")
	}
	if agents[agent2.Address] == nil {
		t.Error("Agent2 should be in the returned map")
	}
}

// Test supported schemas functionality
func TestGetSupportedSchemas(t *testing.T) {
	registry := createTestRegistry()

	// Initially should be empty
	schemas := registry.GetSupportedSchemas()
	if len(schemas) != 0 {
		t.Errorf("Expected 0 schemas initially, got %d", len(schemas))
	}

	// Register agents with schemas
	agent1 := &LocalAgent{
		Address:          "test1",
		DeliveryMode:     "pull",
		SupportedSchemas: []string{"agntcy:commerce.order.v1", "agntcy:commerce.product.v1"},
	}
	agent2 := &LocalAgent{
		Address:          "test2",
		DeliveryMode:     "pull",
		SupportedSchemas: []string{"agntcy:commerce.order.v1", "agntcy:auth.user.v1"}, // Overlapping schema
	}

	if err := registry.RegisterAgent(agent1); err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}
	if err := registry.RegisterAgent(agent2); err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}

	// Get supported schemas
	schemas = registry.GetSupportedSchemas()

	// Should have 3 unique schemas
	expectedSchemas := map[string]bool{
		"agntcy:commerce.order.v1":   true,
		"agntcy:commerce.product.v1": true,
		"agntcy:auth.user.v1":        true,
	}

	if len(schemas) != len(expectedSchemas) {
		t.Errorf("Expected %d unique schemas, got %d", len(expectedSchemas), len(schemas))
	}

	for _, schema := range schemas {
		if !expectedSchemas[schema] {
			t.Errorf("Unexpected schema in results: %s", schema)
		}
	}
}
