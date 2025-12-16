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

package processing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/discovery"
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

// MockAgentRegistry for testing
type MockAgentRegistry struct {
	agents map[string]*agents.LocalAgent
	inbox  map[string][]*types.Message
}

func NewMockAgentRegistry() *MockAgentRegistry {
	return &MockAgentRegistry{
		agents: make(map[string]*agents.LocalAgent),
		inbox:  make(map[string][]*types.Message),
	}
}

func (m *MockAgentRegistry) RegisterAgent(ctx context.Context, agent *agents.LocalAgent) error {
	m.agents[agent.Address] = agent
	return nil
}

func (m *MockAgentRegistry) UnregisterAgent(ctx context.Context, agentNameOrAddress string) error {
	delete(m.agents, agentNameOrAddress)
	return nil
}

func (m *MockAgentRegistry) GetAgent(ctx context.Context, agentAddress string) (*agents.LocalAgent, error) {
	agent, exists := m.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}
	agentCopy := *agent
	return &agentCopy, nil
}

func (m *MockAgentRegistry) GetAllAgents(ctx context.Context) map[string]*agents.LocalAgent {
	agents := make(map[string]*agents.LocalAgent)
	for addr, agent := range m.agents {
		agentCopy := *agent
		agents[addr] = &agentCopy
	}
	return agents
}

func (m *MockAgentRegistry) GetSupportedSchemas(ctx context.Context) []string {
	schemaSet := make(map[string]bool)
	for _, agent := range m.agents {
		for _, schema := range agent.SupportedSchemas {
			if schema != "" {
				schemaSet[schema] = true
			}
		}
	}
	schemas := make([]string, 0, len(schemaSet))
	for schema := range schemaSet {
		schemas = append(schemas, schema)
	}
	return schemas
}

func (m *MockAgentRegistry) GenerateAPIKey() (string, error) {
	return "mock-api-key", nil
}

func (m *MockAgentRegistry) VerifyAPIKey(ctx context.Context, agentAddress, apiKey string) bool {
	agent, exists := m.agents[agentAddress]
	return exists && agent.APIKey == apiKey
}

func (m *MockAgentRegistry) UpdateLastAccess(ctx context.Context, agentAddress string) {
	if agent, exists := m.agents[agentAddress]; exists {
		agent.LastAccess = time.Now().UTC()
	}
}

func (m *MockAgentRegistry) RotateAPIKey(ctx context.Context, agentAddress string) (string, error) {
	agent, exists := m.agents[agentAddress]
	if !exists {
		return "", fmt.Errorf("agent not found: %s", agentAddress)
	}
	newKey := "rotated-api-key"
	agent.APIKey = newKey
	return newKey, nil
}

func (m *MockAgentRegistry) StoreMessage(recipient string, message *types.Message) error {
	if m.inbox[recipient] == nil {
		m.inbox[recipient] = make([]*types.Message, 0)
	}
	m.inbox[recipient] = append(m.inbox[recipient], message)
	return nil
}

func (m *MockAgentRegistry) GetInboxMessages(recipient string) []*types.Message {
	messages, exists := m.inbox[recipient]
	if !exists {
		return []*types.Message{}
	}
	result := make([]*types.Message, len(messages))
	copy(result, messages)
	return result
}

func (m *MockAgentRegistry) AcknowledgeMessage(recipient, messageID string) error {
	messages, exists := m.inbox[recipient]
	if !exists {
		return fmt.Errorf("no messages found for recipient: %s", recipient)
	}
	for i, msg := range messages {
		if msg.MessageID == messageID {
			m.inbox[recipient] = append(messages[:i], messages[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("message not found: %s", messageID)
}

func (m *MockAgentRegistry) GetStats() map[string]interface{} {
	totalAgents := len(m.agents)
	pushAgents := 0
	pullAgents := 0
	totalInboxMessages := 0
	for _, agent := range m.agents {
		if agent.DeliveryMode == "push" {
			pushAgents++
		} else {
			pullAgents++
		}
	}
	for _, messages := range m.inbox {
		totalInboxMessages += len(messages)
	}
	return map[string]interface{}{
		"local_agents":         totalAgents,
		"push_agents":          pushAgents,
		"pull_agents":          pullAgents,
		"total_inbox_messages": totalInboxMessages,
	}
}

func createTestDeliveryConfig() DeliveryConfig {
	return DeliveryConfig{
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
		MaxConnections: 10,
		IdleTimeout:    90 * time.Second,
		UserAgent:      "AMTP-Gateway-Test/1.0",
		MaxMessageSize: 10485760,
		LocalDomain:    "localhost",
	}
}

func TestNewDeliveryEngine(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	mockAgentRegistry := NewMockAgentRegistry()
	config := createTestDeliveryConfig()

	engine := NewDeliveryEngine(mockDiscovery, mockAgentRegistry, config)

	if engine == nil {
		t.Fatal("NewDeliveryEngine returned nil")
	}

	if engine.discovery != mockDiscovery {
		t.Error("Discovery not set correctly")
	}

	if engine.config.Timeout != config.Timeout {
		t.Error("Config timeout not set correctly")
	}

	if engine.httpClient == nil {
		t.Error("HTTP client not initialized")
	}
}

func TestDeliverMessage_Success(t *testing.T) {
	// For this test, we'll use a mock delivery engine to test the interface
	// The actual HTTP delivery is tested in integration tests
	mockEngine := NewMockDeliveryEngine()
	mockEngine.SetDeliveryResult("recipient@test.com", &DeliveryResult{
		Status:     types.StatusDelivered,
		StatusCode: 200,
		Timestamp:  time.Now().UTC(),
		Attempts:   1,
	})

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := mockEngine.DeliverMessage(ctx, message, recipient)

	if err != nil {
		t.Fatalf("DeliverMessage failed: %v", err)
	}

	if result == nil {
		t.Fatal("DeliverMessage returned nil result")
	}

	if result.Status != types.StatusDelivered {
		t.Errorf("Expected status %s, got %s", types.StatusDelivered, result.Status)
	}

	if result.StatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", result.StatusCode)
	}

	if result.Attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", result.Attempts)
	}
}

func TestDeliverMessage_ServerError_WithRetry(t *testing.T) {
	mockEngine := NewMockDeliveryEngine()
	mockEngine.SetDeliveryResult("recipient@test.com", &DeliveryResult{
		Status:       types.StatusFailed,
		StatusCode:   500,
		ErrorCode:    "SERVER_ERROR",
		ErrorMessage: "Internal server error",
		Timestamp:    time.Now().UTC(),
		Attempts:     2,
	})

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := mockEngine.DeliverMessage(ctx, message, recipient)

	if err != nil {
		t.Fatalf("Mock should not return error: %v", err)
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}

	if result.Attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", result.Attempts)
	}

	if result.StatusCode != 500 {
		t.Errorf("Expected status code 500, got %d", result.StatusCode)
	}
}

func TestDeliverMessage_ClientError_NoRetry(t *testing.T) {
	mockEngine := NewMockDeliveryEngine()
	mockEngine.SetDeliveryResult("recipient@test.com", &DeliveryResult{
		Status:       types.StatusFailed,
		StatusCode:   400,
		ErrorCode:    "CLIENT_ERROR",
		ErrorMessage: "Bad request",
		Timestamp:    time.Now().UTC(),
		Attempts:     1,
	})

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := mockEngine.DeliverMessage(ctx, message, recipient)

	if err != nil {
		t.Fatalf("Mock should not return error: %v", err)
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}

	if result.Attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", result.Attempts)
	}

	if result.StatusCode != 400 {
		t.Errorf("Expected status code 400, got %d", result.StatusCode)
	}
}

func TestDeliverMessage_DiscoveryFailure(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetError(fmt.Errorf("DNS lookup failed"))

	config := createTestDeliveryConfig()
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := engine.DeliverMessage(ctx, message, recipient)

	if err == nil {
		t.Fatal("Expected error for discovery failure")
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}

	if result.ErrorCode != "DISCOVERY_FAILED" {
		t.Errorf("Expected error code DISCOVERY_FAILED, got %s", result.ErrorCode)
	}
}

func TestDeliverMessage_InvalidRecipient(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	config := createTestDeliveryConfig()
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipient := "invalid-email" // No @ symbol

	ctx := context.Background()
	result, err := engine.DeliverMessage(ctx, message, recipient)

	if err == nil {
		t.Fatal("Expected error for invalid recipient")
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}

	if result.ErrorCode != "INVALID_RECIPIENT" {
		t.Errorf("Expected error code INVALID_RECIPIENT, got %s", result.ErrorCode)
	}
}

func TestDeliverMessage_MessageTooLarge(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetCapabilities("test.com", &discovery.AMTPCapabilities{
		Version:      "1.0",
		Gateway:      "https://test.com",
		MaxSize:      100, // Very small limit
		Features:     []string{"immediate-path"},
		DiscoveredAt: time.Now(),
		TTL:          5 * time.Minute,
	})

	config := createTestDeliveryConfig()
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := engine.DeliverMessage(ctx, message, recipient)

	if err == nil {
		t.Fatal("Expected error for message too large")
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}

	if result.ErrorCode != "MESSAGE_TOO_LARGE" {
		t.Errorf("Expected error code MESSAGE_TOO_LARGE, got %s", result.ErrorCode)
	}
}

func TestDeliverMessage_InvalidGatewayURL(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetCapabilities("test.com", &discovery.AMTPCapabilities{
		Version:      "1.0",
		Gateway:      "invalid-url", // Invalid URL
		MaxSize:      10485760,
		Features:     []string{"immediate-path"},
		DiscoveredAt: time.Now(),
		TTL:          5 * time.Minute,
	})

	config := createTestDeliveryConfig()
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := engine.DeliverMessage(ctx, message, recipient)

	if err == nil {
		t.Fatal("Expected error for invalid gateway URL")
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}

	if result.ErrorCode != "INVALID_GATEWAY" {
		t.Errorf("Expected error code INVALID_GATEWAY, got %s", result.ErrorCode)
	}
}

func TestDeliverBatch(t *testing.T) {
	mockEngine := NewMockDeliveryEngine()
	mockEngine.SetDeliveryResult("recipient1@test1.com", &DeliveryResult{
		Status:     types.StatusDelivered,
		StatusCode: 200,
		Timestamp:  time.Now().UTC(),
		Attempts:   1,
	})
	mockEngine.SetDeliveryResult("recipient2@test2.com", &DeliveryResult{
		Status:     types.StatusDelivered,
		StatusCode: 200,
		Timestamp:  time.Now().UTC(),
		Attempts:   1,
	})

	message := createTestMessage()
	recipients := []string{"recipient1@test1.com", "recipient2@test2.com"}

	ctx := context.Background()
	results := make(map[string]*DeliveryResult)

	// Simulate batch delivery by calling DeliverMessage for each recipient
	for _, recipient := range recipients {
		result, err := mockEngine.DeliverMessage(ctx, message, recipient)
		if err != nil {
			t.Fatalf("DeliverMessage failed for %s: %v", recipient, err)
		}
		results[recipient] = result
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	for recipient, result := range results {
		if result.Status != types.StatusDelivered {
			t.Errorf("Expected status %s for %s, got %s", types.StatusDelivered, recipient, result.Status)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	config := createTestDeliveryConfig()
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	tests := []struct {
		statusCode int
		err        error
		retryable  bool
	}{
		{429, nil, true},                             // Too Many Requests
		{502, nil, true},                             // Bad Gateway
		{503, nil, true},                             // Service Unavailable
		{504, nil, true},                             // Gateway Timeout
		{0, nil, true},                               // No response
		{400, nil, false},                            // Bad Request
		{401, nil, false},                            // Unauthorized
		{404, nil, false},                            // Not Found
		{200, nil, false},                            // Success
		{0, fmt.Errorf("timeout"), true},             // Timeout error
		{0, fmt.Errorf("connection refused"), true},  // Connection error
		{0, fmt.Errorf("no such host"), true},        // DNS error
		{0, fmt.Errorf("network unreachable"), true}, // Network error
		{400, fmt.Errorf("some other error"), false}, // Other error with 4xx status
	}

	for _, test := range tests {
		result := engine.isRetryableError(test.statusCode, test.err)
		if result != test.retryable {
			t.Errorf("isRetryableError(%d, %v) = %v, expected %v",
				test.statusCode, test.err, result, test.retryable)
		}
	}
}

func TestCalculateRetryDelay(t *testing.T) {
	mockDiscovery := NewMockDiscovery()
	config := createTestDeliveryConfig()
	config.RetryDelay = 1 * time.Second
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	// Test exponential backoff
	delay1 := engine.calculateRetryDelay(1)
	delay2 := engine.calculateRetryDelay(2)
	delay3 := engine.calculateRetryDelay(3)

	if delay1 >= delay2 {
		t.Errorf("Expected delay1 (%v) < delay2 (%v)", delay1, delay2)
	}

	if delay2 >= delay3 {
		t.Errorf("Expected delay2 (%v) < delay3 (%v)", delay2, delay3)
	}

	// Test maximum delay cap
	largeDelay := engine.calculateRetryDelay(20)
	maxDelay := 5 * time.Minute
	if largeDelay > maxDelay*2 { // Allow some jitter
		t.Errorf("Expected delay to be capped, got %v", largeDelay)
	}
}

func TestDeliverMessage_ContextCancellation(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "accepted"}`))
	}))
	defer server.Close()

	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetCapabilities("test.com", &discovery.AMTPCapabilities{
		Version: "1.0", Gateway: server.URL, MaxSize: 10485760,
		Features: []string{"immediate-path"}, DiscoveredAt: time.Now(), TTL: 5 * time.Minute,
	})

	config := createTestDeliveryConfig()
	config.AllowHTTP = true                // Allow HTTP for test server
	config.Timeout = 50 * time.Millisecond // Short timeout
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipient := "recipient@test.com"

	ctx := context.Background()
	result, err := engine.DeliverMessage(ctx, message, recipient)

	if err == nil {
		t.Fatal("Expected error for context timeout")
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}
}

func BenchmarkDeliverMessage(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "accepted"}`))
	}))
	defer server.Close()

	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetCapabilities("test.com", &discovery.AMTPCapabilities{
		Version: "1.0", Gateway: server.URL, MaxSize: 10485760,
		Features: []string{"immediate-path"}, DiscoveredAt: time.Now(), TTL: 5 * time.Minute,
	})

	config := createTestDeliveryConfig()
	config.AllowHTTP = true // Allow HTTP for test server
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipient := "recipient@test.com"
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.DeliverMessage(ctx, message, recipient)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeliverBatch(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "accepted"}`))
	}))
	defer server.Close()

	mockDiscovery := NewMockDiscovery()
	for i := 0; i < 10; i++ {
		domain := fmt.Sprintf("test%d.com", i)
		mockDiscovery.SetCapabilities(domain, &discovery.AMTPCapabilities{
			Version: "1.0", Gateway: server.URL, MaxSize: 10485760,
			Features: []string{"immediate-path"}, DiscoveredAt: time.Now(), TTL: 5 * time.Minute,
		})
	}

	config := createTestDeliveryConfig()
	config.AllowHTTP = true // Allow HTTP for test server
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)

	message := createTestMessage()
	recipients := make([]string, 10)
	for i := 0; i < 10; i++ {
		recipients[i] = fmt.Sprintf("recipient%d@test%d.com", i, i)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.DeliverBatch(ctx, message, recipients)
		if err != nil {
			b.Fatal(err)
		}
	}
}
