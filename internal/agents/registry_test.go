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
	"fmt"
	"strings"
	"sync"
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

type inMemoryAgentStore struct {
	mu sync.Mutex
	// agents maps the full address to the stored agent.
	agents map[string]*LocalAgent
	// fullUpdates counts full-record UpdateAgent calls; fieldUpdates counts
	// field-level UpdateAgentFields calls. The registry must never perform a
	// full-record update, which could clobber a concurrent key rotation.
	fullUpdates  int
	fieldUpdates int
	// fieldUpdateError, when set, makes UpdateAgentFields fail so tests can
	// verify the registry propagates the underlying storage error.
	fieldUpdateError error
}

func newInMemoryAgentStore() *inMemoryAgentStore {
	return &inMemoryAgentStore{
		agents: make(map[string]*LocalAgent),
	}
}

func (s *inMemoryAgentStore) CreateAgent(ctx context.Context, agent *LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[agent.Address]; exists {
		return fmt.Errorf("agent already exists: %s", agent.Address)
	}

	agentCopy := *agent
	s.agents[agent.Address] = &agentCopy
	return nil
}

func (s *inMemoryAgentStore) DeleteAgent(ctx context.Context, agentAddress string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[agentAddress]; !exists {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}
	delete(s.agents, agentAddress)
	return nil
}

func (s *inMemoryAgentStore) GetAgent(ctx context.Context, agentAddress string) (*LocalAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, exists := s.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}
	agentCopy := *agent
	return &agentCopy, nil
}

func (s *inMemoryAgentStore) UpdateAgent(ctx context.Context, agent *LocalAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fullUpdates++
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}
	if _, exists := s.agents[agent.Address]; !exists {
		return fmt.Errorf("agent not found: %s", agent.Address)
	}

	agentCopy := *agent
	s.agents[agent.Address] = &agentCopy
	return nil
}

func (s *inMemoryAgentStore) UpdateAgentFields(ctx context.Context, agentAddress string, fields AgentFields) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fieldUpdates++
	agent, exists := s.agents[agentAddress]
	if !exists {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}

	if s.fieldUpdateError != nil {
		return s.fieldUpdateError
	}

	if fields.DeliveryMode != nil || fields.PushTarget != nil {
		mergedMode := agent.DeliveryMode
		if fields.DeliveryMode != nil {
			mergedMode = *fields.DeliveryMode
		}
		mergedTarget := agent.PushTarget
		if fields.PushTarget != nil {
			mergedTarget = *fields.PushTarget
		}
		if err := ValidateDeliveryConfig(mergedMode, mergedTarget); err != nil {
			return err
		}
	}

	if fields.DeliveryMode != nil {
		agent.DeliveryMode = *fields.DeliveryMode
	}
	if fields.PushTarget != nil {
		agent.PushTarget = *fields.PushTarget
	}
	if fields.PushHeaders != nil {
		agent.Headers = make(map[string]string, len(fields.PushHeaders))
		for k, v := range fields.PushHeaders {
			agent.Headers[k] = v
		}
	}
	if fields.SupportedSchemas != nil {
		agent.SupportedSchemas = append([]string(nil), fields.SupportedSchemas...)
		agent.RequiresSchema = len(fields.SupportedSchemas) > 0
	}
	if fields.LastAccess != nil {
		agent.LastAccess = *fields.LastAccess
	}
	if fields.APIKey != nil {
		agent.APIKey = *fields.APIKey
	}
	return nil
}

func (s *inMemoryAgentStore) ListAgents(ctx context.Context) ([]*LocalAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*LocalAgent
	for _, agent := range s.agents {
		agentCopy := *agent
		list = append(list, &agentCopy)
	}
	return list, nil
}

func (s *inMemoryAgentStore) GetSupportedSchemas(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	schemaSet := make(map[string]struct{})
	for _, agent := range s.agents {
		for _, schemaID := range agent.SupportedSchemas {
			if schemaID == "" {
				continue
			}
			schemaSet[schemaID] = struct{}{}
		}
	}

	var schemas []string
	for schemaID := range schemaSet {
		schemas = append(schemas, schemaID)
	}
	return schemas, nil
}

func createTestRegistry() *Registry {
	config := RegistryConfig{
		LocalDomain:   "localhost",
		SchemaManager: NewMockSchemaManager(),
		APIKeySalt:    "test-salt",
	}
	return NewRegistry(config, newInMemoryAgentStore())
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

// TestValidateDeliveryConfig verifies the delivery-mode invariants shared by
// registration and field-level updates.
func TestValidateDeliveryConfig(t *testing.T) {
	// Valid combinations.
	if err := ValidateDeliveryConfig("push", "http://localhost:8080/hook"); err != nil {
		t.Errorf("push with target should be valid: %v", err)
	}
	if err := ValidateDeliveryConfig("pull", ""); err != nil {
		t.Errorf("pull without target should be valid: %v", err)
	}
	if err := ValidateDeliveryConfig("pull", "http://localhost:8080/hook"); err != nil {
		t.Errorf("pull with target should be valid: %v", err)
	}

	// Invalid combinations.
	if err := ValidateDeliveryConfig("push", ""); err == nil ||
		!strings.Contains(err.Error(), "push target URL is required") {
		t.Errorf("push without target should be rejected, got: %v", err)
	}
	if err := ValidateDeliveryConfig("smtp", "http://localhost:8080/hook"); err == nil ||
		!strings.Contains(err.Error(), "delivery mode must be 'push' or 'pull'") {
		t.Errorf("invalid mode should be rejected, got: %v", err)
	}
}

// Test agent API key verification
func TestVerifyAPIKey(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background() // Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// The agent object passed to RegisterAgent should have the plain text API key
	validKey := agent.APIKey
	if validKey == "" {
		t.Fatal("Agent API key is empty after registration")
	}

	// Get the registered agent - API key should be redacted
	registeredAgent, err := registry.GetAgent(ctx, agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}

	if registeredAgent.APIKey != "" {
		t.Error("GetAgent should redact API key")
	}

	// Test valid key verification
	if !registry.VerifyAPIKey(ctx, agent.Address, validKey) {
		t.Error("Valid API key verification failed")
	}

	// Test invalid key verification
	if registry.VerifyAPIKey(ctx, agent.Address, "invalid-key") {
		t.Error("Invalid API key verification should fail")
	}

	// Test non-existent agent
	if registry.VerifyAPIKey(ctx, "nonexistent@localhost", validKey) {
		t.Error("API key verification for non-existent agent should fail")
	}

	// Test empty key
	if registry.VerifyAPIKey(ctx, agent.Address, "") {
		t.Error("Empty API key verification should fail")
	}

	// Test similar but different key (timing attack protection)
	similarKey := validKey[:len(validKey)-1] + "X" // Change last character
	if registry.VerifyAPIKey(ctx, agent.Address, similarKey) {
		t.Error("Similar but different API key verification should fail")
	}
}

// Test agent API key rotation
func TestRotateAPIKey(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	// Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Get original API key from the agent object
	originalKey := agent.APIKey
	if originalKey == "" {
		t.Fatal("Original API key is empty")
	}

	// Rotate the API key
	newKey, err := registry.RotateAPIKey(ctx, agent.Address)
	if err != nil {
		t.Fatalf("Failed to rotate API key: %v", err)
	}

	// Verify new key is different
	if newKey == originalKey {
		t.Error("Rotated API key should be different from original")
	}

	// Verify old key no longer works
	if registry.VerifyAPIKey(ctx, agent.Address, originalKey) {
		t.Error("Original API key should no longer work after rotation")
	}

	// Verify new key works
	if !registry.VerifyAPIKey(ctx, agent.Address, newKey) {
		t.Error("New API key should work after rotation")
	}

	// Test rotation for non-existent agent
	_, err = registry.RotateAPIKey(ctx, "nonexistent@localhost")
	if err == nil {
		t.Error("Rotating API key for non-existent agent should fail")
	}
	// The storage layer reports the missing agent; the error must be
	// propagated, not masked as a generic failure.
	if !strings.Contains(err.Error(), "agent not found") {
		t.Errorf("Expected agent not found error, got: %v", err)
	}
}

// TestRotateAPIKey_PropagatesStorageError verifies that a storage failure
// during key rotation is propagated to the caller instead of being masked as
// "agent not found" (a transient storage outage would previously be
// indistinguishable from a missing agent).
func TestRotateAPIKey_PropagatesStorageError(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	agent := &LocalAgent{Address: "test", DeliveryMode: "pull"}
	if err := registry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	store := registry.storage.(*inMemoryAgentStore)
	store.fieldUpdateError = fmt.Errorf("storage outage: connection refused")

	_, err := registry.RotateAPIKey(ctx, agent.Address)
	if err == nil {
		t.Fatal("expected rotation to fail during storage outage")
	}
	if !strings.Contains(err.Error(), "storage outage: connection refused") {
		t.Errorf("expected underlying storage error to be propagated, got: %v", err)
	}
}

// Test agent last access update
func TestUpdateLastAccess(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	if err := registry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	initialAgent, err := registry.GetAgent(ctx, agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}
	initialLastAccess := initialAgent.LastAccess

	time.Sleep(10 * time.Millisecond)

	registry.UpdateLastAccess(ctx, agent.Address)

	updatedAgent, err := registry.GetAgent(ctx, agent.Address)
	if err != nil {
		t.Fatalf("Failed to get updated agent: %v", err)
	}

	if !updatedAgent.LastAccess.After(initialLastAccess) {
		t.Error("Last access time should be updated")
	}

	registry.UpdateLastAccess(ctx, "nonexistent@localhost")

	if _, err := registry.GetAgent(ctx, "nonexistent@localhost"); err == nil {
		t.Error("Non-existent agent should not be created during last access update")
	}
}

func strPtr(s string) *string { return &s }

// TestUpdateAgent_UsesFieldLevelUpdate verifies that the registry updates an
// agent with a field-level storage write (leaving the API key untouched)
// instead of a full-record read-modify-write that could clobber a concurrent
// key rotation.
func TestUpdateAgent_UsesFieldLevelUpdate(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}
	if err := registry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}
	store := registry.storage.(*inMemoryAgentStore)
	// The stored API key is the hash, not the plaintext returned to callers.
	storedKey := store.agents[agent.Address].APIKey

	updated, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{
		DeliveryMode: strPtr("push"),
		PushTarget:   strPtr("http://localhost:8080/hook"),
	})
	if err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	// The update must go through the field-level path exclusively.
	if store.fieldUpdates != 1 || store.fullUpdates != 0 {
		t.Errorf("Expected 1 field-level update and 0 full-record updates, got field=%d full=%d",
			store.fieldUpdates, store.fullUpdates)
	}

	got, err := registry.GetAgent(ctx, agent.Address)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if got.DeliveryMode != "push" || got.PushTarget != "http://localhost:8080/hook" {
		t.Errorf("Expected updated delivery fields, got mode=%q target=%q", got.DeliveryMode, got.PushTarget)
	}
	// The API key hash in storage must be unchanged.
	if store.agents[agent.Address].APIKey != storedKey {
		t.Error("UpdateAgent must not modify the stored API key hash")
	}
	// The returned record redacts the key.
	if updated.APIKey != "" {
		t.Errorf("Expected redacted API key in update response, got %q", updated.APIKey)
	}
}

// TestUpdateAgent_SupportedSchemasDerivesRequiresSchema verifies that setting
// supported schemas also updates the derived RequiresSchema flag.
func TestUpdateAgent_SupportedSchemasDerivesRequiresSchema(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	agent := &LocalAgent{Address: "test", DeliveryMode: "pull"}
	if err := registry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	if _, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{
		SupportedSchemas: []string{"agntcy:test.hello.v1"},
	}); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	store := registry.storage.(*inMemoryAgentStore)
	stored := store.agents[agent.Address]
	if !stored.RequiresSchema {
		t.Error("Expected RequiresSchema to be derived true when schemas are set")
	}
	if len(stored.SupportedSchemas) != 1 || stored.SupportedSchemas[0] != "agntcy:test.hello.v1" {
		t.Errorf("Expected updated supported schemas, got %v", stored.SupportedSchemas)
	}
}

// TestUpdateAgent_InvalidDeliveryCombination verifies the delivery invariant
// on the merged state: switching to push without a target, and clearing the
// target while in push mode, are both rejected with the stored record
// unchanged.
func TestUpdateAgent_InvalidDeliveryCombination(t *testing.T) {
	ctx := context.Background()

	t.Run("switch to push without target", func(t *testing.T) {
		registry := createTestRegistry()
		agent := &LocalAgent{Address: "test", DeliveryMode: "pull"}
		if err := registry.RegisterAgent(ctx, agent); err != nil {
			t.Fatalf("register: %v", err)
		}

		if _, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{
			DeliveryMode: strPtr("push"),
		}); err == nil || !strings.Contains(err.Error(), "push target URL is required") {
			t.Fatalf("expected push target required error, got: %v", err)
		}

		got, err := registry.GetAgent(ctx, agent.Address)
		if err != nil {
			t.Fatalf("get agent: %v", err)
		}
		if got.DeliveryMode != "pull" || got.PushTarget != "" {
			t.Errorf("agent mutated by rejected update: mode=%q target=%q", got.DeliveryMode, got.PushTarget)
		}
	})

	t.Run("clear target in push mode", func(t *testing.T) {
		registry := createTestRegistry()
		agent := &LocalAgent{Address: "test", DeliveryMode: "push", PushTarget: "http://localhost:8080/hook"}
		if err := registry.RegisterAgent(ctx, agent); err != nil {
			t.Fatalf("register: %v", err)
		}

		if _, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{
			PushTarget: strPtr(""),
		}); err == nil || !strings.Contains(err.Error(), "push target URL is required") {
			t.Fatalf("expected push target required error, got: %v", err)
		}

		got, err := registry.GetAgent(ctx, agent.Address)
		if err != nil {
			t.Fatalf("get agent: %v", err)
		}
		if got.DeliveryMode != "push" || got.PushTarget != "http://localhost:8080/hook" {
			t.Errorf("agent mutated by rejected update: mode=%q target=%q", got.DeliveryMode, got.PushTarget)
		}
	})

	t.Run("invalid mode", func(t *testing.T) {
		registry := createTestRegistry()
		agent := &LocalAgent{Address: "test", DeliveryMode: "pull"}
		if err := registry.RegisterAgent(ctx, agent); err != nil {
			t.Fatalf("register: %v", err)
		}

		if _, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{
			DeliveryMode: strPtr("smtp"),
		}); err == nil || !strings.Contains(err.Error(), "delivery mode must be 'push' or 'pull'") {
			t.Fatalf("expected delivery mode error, got: %v", err)
		}
	})
}

// TestUpdateAgent_ConcurrentDeliveryUpdatesInvariantHeld verifies the race
// fixed by enforcing the delivery invariant atomically in storage: two
// concurrent UpdateAgent calls — one clearing push_target, the other
// switching to push — each validate against the state committed by the other,
// so exactly one fails and the final state is never push mode with an empty
// push target.
func TestUpdateAgent_ConcurrentDeliveryUpdatesInvariantHeld(t *testing.T) {
	for i := 0; i < 5; i++ {
		registry := createTestRegistry()
		ctx := context.Background()

		agent := &LocalAgent{
			Address:      "race",
			DeliveryMode: "pull",
			PushTarget:   "http://localhost:8080/hook",
		}
		if err := registry.RegisterAgent(ctx, agent); err != nil {
			t.Fatalf("register: %v", err)
		}

		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{PushTarget: strPtr("")})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := registry.UpdateAgent(ctx, agent.Address, &AgentUpdate{DeliveryMode: strPtr("push")})
			errs <- err
		}()
		close(start)
		wg.Wait()
		close(errs)

		okCount, errCount := 0, 0
		for err := range errs {
			if err == nil {
				okCount++
			} else {
				errCount++
				if !strings.Contains(err.Error(), "push target URL is required") {
					t.Errorf("unexpected error: %v", err)
				}
			}
		}
		if okCount != 1 || errCount != 1 {
			t.Fatalf("iteration %d: expected exactly one success and one failure, got %d success and %d failures",
				i, okCount, errCount)
		}

		got, err := registry.GetAgent(ctx, agent.Address)
		if err != nil {
			t.Fatalf("get agent: %v", err)
		}
		if got.DeliveryMode == "push" && got.PushTarget == "" {
			t.Errorf("iteration %d: concurrent updates stored invalid state: push mode with empty push target", i)
		}
	}
}

// TestUpdateLastAccess_UsesFieldLevelUpdate verifies that UpdateLastAccess
// performs a field-level write so it cannot clobber a concurrent rotation.
func TestUpdateLastAccess_UsesFieldLevelUpdate(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	agent := &LocalAgent{Address: "test", DeliveryMode: "pull"}
	if err := registry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}
	store := registry.storage.(*inMemoryAgentStore)
	storedKey := store.agents[agent.Address].APIKey

	registry.UpdateLastAccess(ctx, agent.Address)

	if store.fieldUpdates != 1 || store.fullUpdates != 0 {
		t.Errorf("Expected 1 field-level update and 0 full-record updates, got field=%d full=%d",
			store.fieldUpdates, store.fullUpdates)
	}
	if store.agents[agent.Address].APIKey != storedKey {
		t.Error("UpdateLastAccess must not modify the stored API key hash")
	}
}

// TestRotateAPIKey_UsesFieldLevelUpdate verifies that key rotation performs a
// field-level write so it cannot clobber a concurrent agent update.
func TestRotateAPIKey_UsesFieldLevelUpdate(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	agent := &LocalAgent{Address: "test", DeliveryMode: "pull"}
	if err := registry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}
	store := registry.storage.(*inMemoryAgentStore)
	oldKey := agent.APIKey

	newKey, err := registry.RotateAPIKey(ctx, agent.Address)
	if err != nil {
		t.Fatalf("RotateAPIKey failed: %v", err)
	}

	if store.fieldUpdates != 1 || store.fullUpdates != 0 {
		t.Errorf("Expected 1 field-level update and 0 full-record updates, got field=%d full=%d",
			store.fieldUpdates, store.fullUpdates)
	}
	if !registry.VerifyAPIKey(ctx, agent.Address, newKey) {
		t.Error("New key should verify after rotation")
	}
	if registry.VerifyAPIKey(ctx, agent.Address, oldKey) {
		t.Error("Old key should no longer verify after rotation")
	}
}

// Test agent registration with API key generation
func TestRegisterAgentAPIKeyGeneration(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

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
			err := registry.RegisterAgent(ctx, tt.agent)

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
			registeredAgent, err := registry.GetAgent(ctx, tt.agent.Address)
			if err != nil {
				t.Fatalf("Failed to get registered agent: %v", err)
			}

			// Verify GetAgent redacts API key
			if registeredAgent.APIKey != "" {
				t.Error("GetAgent should redact API key")
			}

			// Verify API key was generated/preserved in the input struct
			if tt.agent.APIKey == "" {
				t.Error("API key should be present in the input struct after registration")
			}

			// Verify we can verify the key
			if !registry.VerifyAPIKey(ctx, tt.agent.Address, tt.agent.APIKey) {
				t.Error("Should be able to verify the API key")
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
	ctx := context.Background()

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
	if err := registry.RegisterAgent(ctx, agent1); err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}
	if err := registry.RegisterAgent(ctx, agent2); err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}
	if err := registry.RegisterAgent(ctx, agent3); err != nil {
		t.Fatalf("Failed to register agent3: %v", err)
	}

	// Add some messages to inbox for pull agent
	testMessage := &types.Message{
		MessageID: "test-msg-1",
		Sender:    "sender@example.com",
		Subject:   "Test Message",
	}
	// Use the full address that was generated by the registry
	registeredAgent2, err := registry.GetAgent(ctx, agent2.Address)
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
	ctx := context.Background()

	// Register an agent
	agent := &LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}

	err := registry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Verify agent exists
	_, err = registry.GetAgent(ctx, agent.Address)
	if err != nil {
		t.Fatalf("Agent should exist after registration: %v", err)
	}

	// Get the registered agent to get the full address
	registeredAgent, err := registry.GetAgent(ctx, agent.Address)
	if err != nil {
		t.Fatalf("Failed to get registered agent: %v", err)
	}

	// Unregister agent using the agent name (not full address)
	err = registry.UnregisterAgent(ctx, "test") // Use agent name, not full address
	if err != nil {
		t.Fatalf("Failed to unregister agent: %v", err)
	}

	// Verify agent no longer exists
	_, err = registry.GetAgent(ctx, registeredAgent.Address)
	if err == nil {
		t.Error("Agent should not exist after unregistration")
	}

	// Test unregistering non-existent agent
	err = registry.UnregisterAgent(ctx, "non-existent")
	if err == nil {
		t.Error("Expected error when unregistering non-existent agent")
	}
}

// TestResolveAgentAddress verifies that ResolveAgentAddress accepts either a
// bare agent name or a full address matching the local domain and returns the
// normalized full address, and rejects foreign domains and invalid names.
func TestResolveAgentAddress(t *testing.T) {
	registry := createTestRegistry()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"bare name", "viewer", "viewer@localhost", false},
		{"full address matching local domain", "viewer@localhost", "viewer@localhost", false},
		{"foreign domain", "viewer@example.com", "", true},
		{"invalid characters", "bad name!", "", true},
		{"empty", "", "", true},
		{"name with dots", "sales.us", "sales.us@localhost", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registry.ResolveAgentAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error for input %q, got %q", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveAgentAddress(%q) failed: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, got)
			}
		})
	}
}

// Test getting all agents
func TestGetAllAgents(t *testing.T) {
	registry := createTestRegistry()
	ctx := context.Background()

	// Initially should be empty
	agents := registry.GetAllAgents(ctx)
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

	if err := registry.RegisterAgent(ctx, agent1); err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}
	if err := registry.RegisterAgent(ctx, agent2); err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}

	// Get all agents
	agents = registry.GetAllAgents(ctx)
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
	ctx := context.Background()

	// Initially should be empty
	schemas := registry.GetSupportedSchemas(ctx)
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

	if err := registry.RegisterAgent(ctx, agent1); err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}
	if err := registry.RegisterAgent(ctx, agent2); err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}

	// Get supported schemas
	schemas = registry.GetSupportedSchemas(ctx)

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
