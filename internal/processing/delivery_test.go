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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/signing"
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

func (m *MockAgentRegistry) AuthenticateAgent(ctx context.Context, apiKey string) (string, bool) {
	for addr, agent := range m.agents {
		if agent != nil && agent.APIKey == apiKey {
			return addr, true
		}
	}
	return "", false
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

func (m *MockAgentRegistry) UpdateAgent(ctx context.Context, agentNameOrAddress string, updates *agents.AgentUpdate) (*agents.LocalAgent, error) {
	agent, exists := m.agents[agentNameOrAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentNameOrAddress)
	}
	if updates.DeliveryMode != nil {
		agent.DeliveryMode = *updates.DeliveryMode
	}
	if updates.PushTarget != nil {
		agent.PushTarget = *updates.PushTarget
	}
	if updates.PushHeaders != nil {
		agent.Headers = updates.PushHeaders
	}
	if updates.SupportedSchemas != nil {
		agent.SupportedSchemas = updates.SupportedSchemas
	}
	agentCopy := *agent
	agentCopy.APIKey = ""
	return &agentCopy, nil
}

func (m *MockAgentRegistry) ResolveAgentAddress(nameOrAddress string) (string, error) {
	for i := 0; i < len(nameOrAddress); i++ {
		if nameOrAddress[i] == '@' {
			return nameOrAddress, nil
		}
	}
	return nameOrAddress + "@localhost", nil
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

// newTestSigner generates a fresh ES256 signer for local-domain tests.
func newTestSigner(t *testing.T, keyID string) *signing.Signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	signer, err := signing.LoadSigner(pemBytes, signing.AlgES256, keyID)
	if err != nil {
		t.Fatalf("load signer: %v", err)
	}
	return signer
}

// signerKeyRecord formats the signer's public key as a DNS TXT record.
func signerKeyRecord(t *testing.T, s *signing.Signer) string {
	t.Helper()
	rec, err := s.PublicKeyRecord()
	if err != nil {
		t.Fatalf("public key record: %v", err)
	}
	txt, err := signing.FormatKeyRecord(rec)
	if err != nil {
		t.Fatalf("format key record: %v", err)
	}
	return txt
}

// captureServer records the raw body of every request it receives.
type captureServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	bodies   [][]byte
	failOnce int // number of initial requests to fail with 500
	handled  int
}

func newCaptureServer(failOnce int) *captureServer {
	cs := &captureServer{failOnce: failOnce}
	cs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cs.mu.Lock()
		cs.bodies = append(cs.bodies, body)
		n := cs.handled
		cs.handled++
		cs.mu.Unlock()
		if n < cs.failOnce {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"try again"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	return cs
}

func (cs *captureServer) close() { cs.server.Close() }

func (cs *captureServer) bodyCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.bodies)
}

func (cs *captureServer) body(i int) []byte {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.bodies[i]
}

// setupSignedDelivery wires a delivery engine against a capture server for a
// remote recipient domain, with the given signer (nil = unsigned).
func setupSignedDelivery(t *testing.T, signer *signing.Signer, failOnce int) (*DeliveryEngine, *captureServer) {
	t.Helper()
	cs := newCaptureServer(failOnce)
	t.Cleanup(cs.close)

	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetCapabilities("remote.example", &discovery.AMTPCapabilities{
		Version: "1.0", Gateway: cs.server.URL, MaxSize: 10485760,
		Features: []string{"immediate-path"}, DiscoveredAt: time.Now(), TTL: 5 * time.Minute,
	})

	config := createTestDeliveryConfig()
	config.AllowHTTP = true
	config.RetryDelay = 10 * time.Millisecond
	config.LocalDomain = "local.example"
	engine := NewDeliveryEngine(mockDiscovery, NewMockAgentRegistry(), config)
	engine.SetSigner(signer)
	return engine, cs
}

// TestDeliverMessage_SignedWireBytes verifies that a local-domain sender's
// delivery carries a verifiable signature over the exact wire bytes.
func TestDeliverMessage_SignedWireBytes(t *testing.T) {
	signer := newTestSigner(t, "k1")
	engine, cs := setupSignedDelivery(t, signer, 0)

	message := createTestMessage()
	message.Sender = "agent@local.example"
	message.WorkflowID = "01937a58-0000-7000-8000-000000000001"

	result, err := engine.DeliverMessage(context.Background(), message, "recipient@remote.example")
	if err != nil {
		t.Fatalf("DeliverMessage: %v", err)
	}
	if result.Status != types.StatusDelivered {
		t.Fatalf("status = %s, want delivered", result.Status)
	}

	raw := cs.body(0)

	// The wire body must include workflow_id (previously omitted).
	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal delivered body: %v", err)
	}
	if top["workflow_id"] != message.WorkflowID {
		t.Errorf("workflow_id = %v, want %s", top["workflow_id"], message.WorkflowID)
	}
	if top["version"] != "1.0" {
		t.Errorf("version = %v, want 1.0", top["version"])
	}
	recips, ok := top["recipients"].([]interface{})
	if !ok || len(recips) != 1 || recips[0] != "recipient@remote.example" {
		t.Errorf("recipients = %v, want single remote recipient", top["recipients"])
	}

	// The signature must verify against the signer's public key record.
	resolver := &fixedKeyResolver{txts: []string{signerKeyRecord(t, signer)}}
	verifier := signing.NewVerifier(resolver)
	verification, err := verifier.VerifyBody(context.Background(), raw)
	if err != nil {
		t.Fatalf("VerifyBody: %v", err)
	}
	if verification.Result != signing.ResultVerified {
		t.Errorf("verification result = %s, want verified", verification.Result)
	}
}

// fixedKeyResolver returns the same TXT strings for any owner.
type fixedKeyResolver struct {
	txts []string
}

func (r *fixedKeyResolver) ResolveKeyTXT(_ context.Context, _, _ string) ([]string, error) {
	return r.txts, nil
}

// TestDeliverMessage_UnsignedWithoutSigner verifies deliveries stay unsigned
// when no signer is configured.
func TestDeliverMessage_UnsignedWithoutSigner(t *testing.T) {
	engine, cs := setupSignedDelivery(t, nil, 0)

	message := createTestMessage()
	message.Sender = "agent@local.example"

	result, err := engine.DeliverMessage(context.Background(), message, "recipient@remote.example")
	if err != nil {
		t.Fatalf("DeliverMessage: %v", err)
	}
	if result.Status != types.StatusDelivered {
		t.Fatalf("status = %s, want delivered", result.Status)
	}

	var top map[string]interface{}
	if err := json.Unmarshal(cs.body(0), &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := top["signature"]; exists {
		t.Error("delivery carried a signature although no signer is configured")
	}
}

// TestDeliverMessage_RetriesReuseBytes verifies every retry sends the exact
// same bytes (same signature, no re-signing drift).
func TestDeliverMessage_RetriesReuseBytes(t *testing.T) {
	signer := newTestSigner(t, "k1")
	engine, cs := setupSignedDelivery(t, signer, 2) // first two attempts fail

	message := createTestMessage()
	message.Sender = "agent@local.example"

	result, err := engine.DeliverMessage(context.Background(), message, "recipient@remote.example")
	if err != nil {
		t.Fatalf("DeliverMessage: %v", err)
	}
	if result.Status != types.StatusDelivered {
		t.Fatalf("status = %s, want delivered", result.Status)
	}
	if result.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", result.Attempts)
	}
	if cs.bodyCount() != 3 {
		t.Fatalf("server saw %d requests, want 3", cs.bodyCount())
	}
	for i := 1; i < 3; i++ {
		if string(cs.body(i)) != string(cs.body(0)) {
			t.Errorf("retry %d sent different bytes than attempt 0", i)
		}
	}
}

// TestDeliverMessage_RefusesRelayResigning verifies a message that already
// carries a remote-origin signature is refused rather than re-signed.
func TestDeliverMessage_RefusesRelayResigning(t *testing.T) {
	signer := newTestSigner(t, "k1")
	engine, _ := setupSignedDelivery(t, signer, 0)

	message := createTestMessage()
	message.Sender = "agent@other.example" // not our domain
	message.Signature = &types.MessageSignature{
		Algorithm: "ES256",
		KeyID:     "k9",
		Value:     "bm90LXZhbGlk",
	}

	result, err := engine.DeliverMessage(context.Background(), message, "recipient@remote.example")
	if err == nil {
		t.Fatal("expected relay refusal error")
	}
	if result == nil || result.Status != types.StatusFailed {
		t.Fatalf("result = %+v, want failed status", result)
	}
	if result.ErrorCode != "RELAY_UNSUPPORTED" {
		t.Errorf("error code = %s, want RELAY_UNSUPPORTED", result.ErrorCode)
	}
}

// TestDeliverMessage_DoesNotSignRemoteSender verifies the gateway does not
// sign for a sender domain it does not own, even with a signer configured.
func TestDeliverMessage_DoesNotSignRemoteSender(t *testing.T) {
	signer := newTestSigner(t, "k1")
	engine, cs := setupSignedDelivery(t, signer, 0)

	message := createTestMessage()
	message.Sender = "agent@other.example" // remote sender, no signature

	result, err := engine.DeliverMessage(context.Background(), message, "recipient@remote.example")
	if err != nil {
		t.Fatalf("DeliverMessage: %v", err)
	}
	if result.Status != types.StatusDelivered {
		t.Fatalf("status = %s, want delivered", result.Status)
	}

	var top map[string]interface{}
	if err := json.Unmarshal(cs.body(0), &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := top["signature"]; exists {
		t.Error("gateway signed a message for a sender domain it does not own")
	}
}

// TestDeliverMessage_WireParityWithVector guards against field drift: the
// delivered body must round-trip through the signing package's wire builder
// byte-for-byte.
func TestDeliverMessage_WireParityWithVector(t *testing.T) {
	engine, cs := setupSignedDelivery(t, nil, 0)

	message := createTestMessage()
	message.Sender = "agent@local.example"
	message.InReplyTo = "01234567-89ab-7def-8123-456789abcdee"
	message.ResponseType = "final"

	if _, err := engine.DeliverMessage(context.Background(), message, "recipient@remote.example"); err != nil {
		t.Fatalf("DeliverMessage: %v", err)
	}

	want, err := signing.MarshalWireBody(mustBuildWire(t, message, "recipient@remote.example"))
	if err != nil {
		t.Fatalf("marshal wire body: %v", err)
	}
	if string(cs.body(0)) != string(want) {
		t.Errorf("delivered bytes differ from wire builder output:\n got: %s\nwant: %s", cs.body(0), want)
	}
}

func mustBuildWire(t *testing.T, message *types.Message, recipient string) map[string]interface{} {
	t.Helper()
	body, err := signing.BuildWireBody(message, recipient)
	if err != nil {
		t.Fatalf("BuildWireBody: %v", err)
	}
	return body
}
