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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/logging"
	"github.com/amtp-protocol/agentry/internal/metrics"
	"github.com/amtp-protocol/agentry/internal/processing"
	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/internal/validation"
	"github.com/amtp-protocol/agentry/pkg/uuid"
)

// MockMessageProcessor for testing
type MockMessageProcessor struct {
	processResult *processing.ProcessingResult
	processError  error
	messages      map[string]*types.Message
	statuses      map[string]*types.MessageStatus
}

type MockStorage struct {
	messages map[string]*types.Message
	statuses map[string]*types.MessageStatus
	agents   map[string]*agents.LocalAgent
	// listFilters records every filter passed to ListMessages so handler
	// tests can assert the shape of the underlying storage query.
	listFilters []storage.MessageFilter
	// countFilters records every filter passed to CountMessages.
	countFilters []storage.MessageFilter
	// countError, when set, makes CountMessages fail so handler tests can
	// verify count failures surface as errors instead of being swallowed.
	countError error
	// statusBatches records every message ID slice passed to GetStatuses so
	// handler tests can assert statuses are fetched in one batch call.
	statusBatches [][]string
	// statusesError, when set, makes GetStatuses fail so handler tests can
	// verify status failures surface as errors.
	statusesError error
}

func NewMockMessageProcessor() *MockMessageProcessor {
	return &MockMessageProcessor{
		messages: make(map[string]*types.Message),
		statuses: make(map[string]*types.MessageStatus),
	}
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		messages: make(map[string]*types.Message),
		statuses: make(map[string]*types.MessageStatus),
		agents:   make(map[string]*agents.LocalAgent),
	}
}

// Implement storage.Storage interface
func (m *MockStorage) StoreMessage(ctx context.Context, message *types.Message) error {
	m.messages[message.MessageID] = message
	return nil
}

func (m *MockStorage) GetMessage(ctx context.Context, messageID string) (*types.Message, error) {
	if message, exists := m.messages[messageID]; exists {
		return message, nil
	}
	return nil, fmt.Errorf("message not found: %s", messageID)
}

func (m *MockStorage) DeleteMessage(ctx context.Context, messageID string) error {
	delete(m.messages, messageID)
	return nil
}

func (m *MockStorage) ListMessages(ctx context.Context, filter storage.MessageFilter) ([]*types.Message, error) {
	m.listFilters = append(m.listFilters, filter)
	var messages []*types.Message
	for _, msg := range m.messages {
		messages = append(messages, msg)
	}
	return messages, nil
}

func (m *MockStorage) CountMessages(ctx context.Context, filter storage.MessageFilter) (int64, error) {
	m.countFilters = append(m.countFilters, filter)
	if m.countError != nil {
		return 0, m.countError
	}
	return int64(len(m.messages)), nil
}

func (m *MockStorage) StoreStatus(ctx context.Context, messageID string, status *types.MessageStatus) error {
	m.statuses[messageID] = status
	return nil
}

func (m *MockStorage) GetStatus(ctx context.Context, messageID string) (*types.MessageStatus, error) {
	if status, exists := m.statuses[messageID]; exists {
		return status, nil
	}
	return nil, fmt.Errorf("message status not found: %s", messageID)
}

func (m *MockStorage) GetStatuses(ctx context.Context, messageIDs []string) (map[string]*types.MessageStatus, error) {
	m.statusBatches = append(m.statusBatches, messageIDs)
	if m.statusesError != nil {
		return nil, m.statusesError
	}
	result := make(map[string]*types.MessageStatus)
	for _, id := range messageIDs {
		if status, exists := m.statuses[id]; exists {
			result[id] = status
		}
	}
	return result, nil
}

func (m *MockStorage) UpdateStatus(ctx context.Context, messageID string, updater storage.StatusUpdater) error {
	if status, exists := m.statuses[messageID]; exists {
		return updater(status)
	}
	return fmt.Errorf("message status not found: %s", messageID)
}

func (m *MockStorage) DeleteStatus(ctx context.Context, messageID string) error {
	delete(m.statuses, messageID)
	return nil
}

func (m *MockStorage) GetInboxMessages(ctx context.Context, recipient string) ([]*types.Message, error) {
	var messages []*types.Message
	for _, msg := range m.messages {
		for _, r := range msg.Recipients {
			if r == recipient {
				messages = append(messages, msg)
				break
			}
		}
	}
	return messages, nil
}

func (m *MockStorage) CreateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	agentCopy := *agent
	m.agents[agent.Address] = &agentCopy
	return nil
}

func (m *MockStorage) GetAgent(ctx context.Context, agentAddress string) (*agents.LocalAgent, error) {
	agent, exists := m.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}

	agentCopy := *agent
	return &agentCopy, nil
}

func (m *MockStorage) UpdateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}
	if _, exists := m.agents[agent.Address]; !exists {
		return fmt.Errorf("agent not found: %s", agent.Address)
	}

	agentCopy := *agent
	m.agents[agent.Address] = &agentCopy
	return nil
}

func (m *MockStorage) UpdateAgentFields(ctx context.Context, agentAddress string, fields agents.AgentFields) error {
	if _, exists := m.agents[agentAddress]; !exists {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}
	agent := m.agents[agentAddress]
	if fields.DeliveryMode != nil {
		agent.DeliveryMode = *fields.DeliveryMode
	}
	if fields.PushTarget != nil {
		agent.PushTarget = *fields.PushTarget
	}
	if fields.PushHeaders != nil {
		agent.Headers = fields.PushHeaders
	}
	if fields.SupportedSchemas != nil {
		agent.SupportedSchemas = fields.SupportedSchemas
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

func (m *MockStorage) DeleteAgent(ctx context.Context, agentAddress string) error {
	if _, exists := m.agents[agentAddress]; !exists {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}

	delete(m.agents, agentAddress)
	return nil
}

func (m *MockStorage) ListAgents(ctx context.Context) ([]*agents.LocalAgent, error) {
	var list []*agents.LocalAgent
	for _, agent := range m.agents {
		agentCopy := *agent
		list = append(list, &agentCopy)
	}
	return list, nil
}

func (m *MockStorage) GetSupportedSchemas(ctx context.Context) ([]string, error) {
	schemaSet := make(map[string]struct{})
	for _, agent := range m.agents {
		for _, schemaID := range agent.SupportedSchemas {
			schemaSet[schemaID] = struct{}{}
		}
	}

	var schemas []string
	for schemaID := range schemaSet {
		schemas = append(schemas, schemaID)
	}

	return schemas, nil
}

func (m *MockStorage) AcknowledgeMessage(ctx context.Context, recipient, messageID string) error {
	// Check if message exists
	if _, exists := m.messages[messageID]; !exists {
		return fmt.Errorf("message not found: %s", messageID)
	}
	return nil
}

func (m *MockStorage) Close() error {
	return nil
}

func (m *MockStorage) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *MockStorage) GetStats(ctx context.Context) (storage.StorageStats, error) {
	return storage.StorageStats{}, nil
}

func (m *MockMessageProcessor) ProcessMessage(ctx context.Context, message *types.Message, options processing.ProcessingOptions) (*processing.ProcessingResult, error) {
	if m.processError != nil {
		return nil, m.processError
	}

	if m.processResult != nil {
		// Store the message
		m.messages[message.MessageID] = message

		// Create status
		status := &types.MessageStatus{
			MessageID:  message.MessageID,
			Status:     m.processResult.Status,
			Recipients: m.processResult.Recipients,
			Attempts:   m.processResult.Recipients[0].Attempts,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		m.statuses[message.MessageID] = status

		return m.processResult, nil
	}

	// Default successful processing
	result := &processing.ProcessingResult{
		MessageID: message.MessageID,
		Status:    types.StatusDelivered,
		Recipients: []types.RecipientStatus{
			{
				Address:   message.Recipients[0],
				Status:    types.StatusDelivered,
				Timestamp: time.Now().UTC(),
				Attempts:  1,
			},
		},
		ProcessedAt: time.Now().UTC(),
	}

	// Store the message and status
	m.messages[message.MessageID] = message
	status := &types.MessageStatus{
		MessageID:  message.MessageID,
		Status:     result.Status,
		Recipients: result.Recipients,
		Attempts:   1,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	m.statuses[message.MessageID] = status

	return result, nil
}

func (m *MockMessageProcessor) GetMessage(messageID string) (*types.Message, error) {
	if message, exists := m.messages[messageID]; exists {
		return message, nil
	}
	return nil, fmt.Errorf("message not found: %s", messageID)
}

func (m *MockMessageProcessor) GetMessageStatus(messageID string) (*types.MessageStatus, error) {
	if status, exists := m.statuses[messageID]; exists {
		return status, nil
	}
	return nil, fmt.Errorf("message status not found: %s", messageID)
}

func (m *MockMessageProcessor) SetProcessResult(result *processing.ProcessingResult) {
	m.processResult = result
}

func (m *MockMessageProcessor) SetProcessError(err error) {
	m.processError = err
}

func createTestServer() *Server {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: ":8080",
			Domain:  "localhost",
		},
		Message: config.MessageConfig{
			MaxSize: 10485760,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	discoveryService := &discovery.Discovery{}
	validator := validation.New(cfg.Message.MaxSize)
	processor := NewMockMessageProcessor()
	mockStorage := NewMockStorage()
	logger := logging.NewLogger(cfg.Logging).WithComponent("server")

	// Create agent registry for testing
	agentRegistryConfig := agents.RegistryConfig{
		LocalDomain:   cfg.Server.Domain,
		SchemaManager: nil, // No schema manager in basic test
		APIKeySalt:    "test-salt",
	}
	agentRegistry := agents.NewRegistry(agentRegistryConfig, mockStorage)

	testMetrics := metrics.NewMetricsProvider()

	router := gin.New()

	server := &Server{
		config:        cfg,
		router:        router,
		discovery:     discoveryService,
		validator:     validator,
		processor:     processor,
		storage:       mockStorage,
		agentRegistry: agentRegistry,
		schemaManager: nil, // No schema manager in basic test
		logger:        logger,
		metrics:       testMetrics,
	}

	server.setupRoutes()
	return server
}

// registerTestAgent registers an agent in the given server and returns its
// plaintext API key for use in Authorization headers.
func registerTestAgent(t *testing.T, server *Server, name string) string {
	t.Helper()
	agent := &agents.LocalAgent{Address: name, DeliveryMode: "pull"}
	if err := server.agentRegistry.RegisterAgent(context.Background(), agent); err != nil {
		t.Fatalf("register test agent %s: %v", name, err)
	}
	return agent.APIKey
}

func TestHandleSendMessage_Success(t *testing.T) {
	server := createTestServer()

	requestBody := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@test.com"},
		Subject:    "Test Message",
		Payload:    json.RawMessage(`{"message": "Hello, World!"}`),
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	var response types.SendMessageResponse
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.MessageID == "" {
		t.Error("Expected message ID to be set")
	}

	if response.Status != "delivered" {
		t.Errorf("Expected status 'delivered', got %s", response.Status)
	}

	if len(response.Recipients) != 1 {
		t.Errorf("Expected 1 recipient, got %d", len(response.Recipients))
	}

	if response.Recipients[0].Address != "recipient@test.com" {
		t.Errorf("Expected recipient 'recipient@test.com', got %s", response.Recipients[0].Address)
	}
}

func TestHandleSendMessage_InvalidJSON(t *testing.T) {
	server := createTestServer()

	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBufferString("invalid json"))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "INVALID_REQUEST_FORMAT" {
		t.Errorf("Expected error code 'INVALID_REQUEST_FORMAT', got %s", errorResponse.Error.Code)
	}
}

func TestHandleSendMessage_ValidationFailed(t *testing.T) {
	server := createTestServer()

	requestBody := types.SendMessageRequest{
		Sender:     "invalid-email", // Invalid email format
		Recipients: []string{"recipient@test.com"},
		Subject:    "Test Message",
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "VALIDATION_FAILED" {
		t.Errorf("Expected error code 'VALIDATION_FAILED', got %s", errorResponse.Error.Code)
	}
}

func TestHandleSendMessage_ProcessingFailed(t *testing.T) {
	server := createTestServer()
	mockProcessor := server.processor.(*MockMessageProcessor)
	mockProcessor.SetProcessError(fmt.Errorf("processing failed"))

	requestBody := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@test.com"},
		Subject:    "Test Message",
		Payload:    json.RawMessage(`{"message": "Hello, World!"}`),
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "PROCESSING_FAILED" {
		t.Errorf("Expected error code 'PROCESSING_FAILED', got %s", errorResponse.Error.Code)
	}
}

func TestHandleGetMessage_Success(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "sender")

	// First, send a message to store it
	message := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC(),
		Sender:         "sender@localhost",
		Recipients:     []string{"recipient@test.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"message": "Hello, World!"}`),
	}
	mockStorage.messages[message.MessageID] = message

	req, err := http.NewRequest("GET", "/v1/messages/"+message.MessageID, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	var response types.Message
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.MessageID != message.MessageID {
		t.Errorf("Expected message ID %s, got %s", message.MessageID, response.MessageID)
	}

	if response.Sender != message.Sender {
		t.Errorf("Expected sender %s, got %s", message.Sender, response.Sender)
	}
}

func TestHandleGetMessage_InvalidID(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "sender")

	req, err := http.NewRequest("GET", "/v1/messages/invalid-id", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "INVALID_MESSAGE_ID" {
		t.Errorf("Expected error code 'INVALID_MESSAGE_ID', got %s", errorResponse.Error.Code)
	}
}

func TestHandleGetMessage_NotFound(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "sender")

	req, err := http.NewRequest("GET", "/v1/messages/01234567-89ab-7def-8123-456789abcdef", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "MESSAGE_NOT_FOUND" {
		t.Errorf("Expected error code 'MESSAGE_NOT_FOUND', got %s", errorResponse.Error.Code)
	}
}

func TestHandleGetMessage_ForbiddenOtherAgent(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	// Two agents: the message belongs to agent-a, caller authenticates as agent-b.
	registerTestAgent(t, server, "agent-a")
	keyB := registerTestAgent(t, server, "agent-b")

	message := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC(),
		Sender:         "agent-a@localhost",
		Recipients:     []string{"other@test.com"},
		Subject:        "Private",
	}
	mockStorage.messages[message.MessageID] = message

	req, err := http.NewRequest("GET", "/v1/messages/"+message.MessageID, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+keyB)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for other agent's message, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestHandleGetMessage_NoAuth(t *testing.T) {
	server := createTestServer()

	req, err := http.NewRequest("GET", "/v1/messages/01234567-89ab-7def-8123-456789abcdef", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d without auth, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestHandleGetMessageStatus_Success(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "sender")

	messageID := "01234567-89ab-7def-8123-456789abcdef"
	// Message must exist and belong to the caller.
	mockStorage.messages[messageID] = &types.Message{
		MessageID:  messageID,
		Sender:     "sender@localhost",
		Recipients: []string{"recipient@test.com"},
	}
	status := &types.MessageStatus{
		MessageID: messageID,
		Status:    types.StatusDelivered,
		Recipients: []types.RecipientStatus{
			{
				Address:   "recipient@test.com",
				Status:    types.StatusDelivered,
				Timestamp: time.Now().UTC(),
				Attempts:  1,
			},
		},
		Attempts:  1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	mockStorage.statuses[messageID] = status

	req, err := http.NewRequest("GET", "/v1/messages/"+messageID+"/status", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	var response types.MessageStatus
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.MessageID != messageID {
		t.Errorf("Expected message ID %s, got %s", messageID, response.MessageID)
	}

	if response.Status != types.StatusDelivered {
		t.Errorf("Expected status %s, got %s", types.StatusDelivered, response.Status)
	}

	if len(response.Recipients) != 1 {
		t.Errorf("Expected 1 recipient, got %d", len(response.Recipients))
	}
}

func TestHandleGetMessageStatus_InvalidID(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "sender")

	req, err := http.NewRequest("GET", "/v1/messages/invalid-id/status", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "INVALID_MESSAGE_ID" {
		t.Errorf("Expected error code 'INVALID_MESSAGE_ID', got %s", errorResponse.Error.Code)
	}
}

func TestHandleGetMessageStatus_NotFound(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "sender")

	req, err := http.NewRequest("GET", "/v1/messages/01234567-89ab-7def-8123-456789abcdef/status", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, rr.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(rr.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "MESSAGE_NOT_FOUND" {
		t.Errorf("Expected error code 'MESSAGE_NOT_FOUND', got %s", errorResponse.Error.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	server := createTestServer()

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	var response HealthStatus
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", response.Status)
	}

	if !response.Healthy {
		t.Errorf("Expected healthy to be true, got %v", response.Healthy)
	}

	if response.Version != "1.0" {
		t.Errorf("Expected version '1.0', got %v", response.Version)
	}

	// Check that all components are reported
	expectedComponents := []string{"router", "message_processor", "agent_registry", "discovery_service", "schema_manager"}
	for _, component := range expectedComponents {
		if status, exists := response.Components[component]; !exists {
			t.Errorf("Expected component '%s' to be present in health check", component)
		} else if component != "schema_manager" && status != "healthy" {
			// Schema manager might be "not_configured" which is acceptable
			t.Errorf("Expected component '%s' to be healthy, got '%s'", component, status)
		}
	}
}

func TestHandleReady(t *testing.T) {
	server := createTestServer()

	req, err := http.NewRequest("GET", "/ready", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	var response ReadinessStatus
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "ready" {
		t.Errorf("Expected status 'ready', got %v", response.Status)
	}

	if !response.Ready {
		t.Errorf("Expected ready to be true, got %v", response.Ready)
	}

	if response.Version != "1.0" {
		t.Errorf("Expected version '1.0', got %v", response.Version)
	}

	// Check that all dependencies are reported
	expectedDependencies := []string{"agent_registry", "schema_manager", "discovery_service", "message_processor", "validator"}
	for _, dependency := range expectedDependencies {
		if status, exists := response.Dependencies[dependency]; !exists {
			t.Errorf("Expected dependency '%s' to be present in readiness check", dependency)
		} else if dependency != "schema_manager" && status != "ready" {
			// Schema manager might be "not_configured" which is acceptable
			t.Errorf("Expected dependency '%s' to be ready, got '%s'", dependency, status)
		}
	}
}

func TestHandleHealth_UnhealthyComponents(t *testing.T) {
	// Create server with nil components to test unhealthy state
	server := &Server{
		config: &config.Config{},
		router: nil, // This will make it unhealthy
	}

	health := server.checkHealth()

	if health.Healthy {
		t.Errorf("Expected health to be false when router is nil")
	}

	if health.Status != "unhealthy" {
		t.Errorf("Expected status 'unhealthy', got %v", health.Status)
	}

	if health.Components["router"] != "not_initialized" {
		t.Errorf("Expected router component to be 'not_initialized', got %v", health.Components["router"])
	}
}

func TestHandleReady_NotReadyDependencies(t *testing.T) {
	// Create server with nil dependencies to test not ready state
	server := &Server{
		config:        &config.Config{},
		agentRegistry: nil, // This will make it not ready
	}

	readiness := server.checkReadiness()

	if readiness.Ready {
		t.Errorf("Expected readiness to be false when agentRegistry is nil")
	}

	if readiness.Status != "not_ready" {
		t.Errorf("Expected status 'not_ready', got %v", readiness.Status)
	}

	if readiness.Dependencies["agent_registry"] != "not_initialized" {
		t.Errorf("Expected agent_registry dependency to be 'not_initialized', got %v", readiness.Dependencies["agent_registry"])
	}
}

func createTestServerWithRealProcessor() *Server {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: ":8080",
			Domain:  "localhost",
		},
		Message: config.MessageConfig{
			MaxSize: 10485760,
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		DNS: config.DNSConfig{
			MockMode: true,
			MockRecords: map[string]string{
				"localhost": "v=amtp1;gateway=http://localhost:8080",
			},
		},
	}

	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create real components
	discoveryService := discovery.NewMockDiscovery(cfg.DNS.MockRecords, 5*time.Minute)
	validator := validation.New(cfg.Message.MaxSize)

	// Create message storage
	storageConfig := storage.DefaultStorageConfig()
	storage, _ := storage.NewStorage(storageConfig)

	// Create agent registry
	agentRegistryConfig := agents.RegistryConfig{
		LocalDomain:   cfg.Server.Domain,
		SchemaManager: nil, // No schema manager in tests
		APIKeySalt:    "test-salt",
	}
	agentRegistry := agents.NewRegistry(agentRegistryConfig, storage)

	// Create real delivery engine and processor
	deliveryConfig := processing.DeliveryConfig{
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
		MaxConnections: 100,
		IdleTimeout:    90 * time.Second,
		UserAgent:      "AMTP-Gateway/1.0",
		MaxMessageSize: cfg.Message.MaxSize,
		AllowHTTP:      true,
		LocalDomain:    cfg.Server.Domain,
	}
	deliveryEngine := processing.NewDeliveryEngine(discoveryService, agentRegistry, deliveryConfig)

	processor := processing.NewMessageProcessor(discoveryService, deliveryEngine, storage)

	logger := logging.NewLogger(cfg.Logging).WithComponent("server")

	testMetrics := metrics.NewMetricsProvider()

	server := &Server{
		config:        cfg,
		discovery:     discoveryService,
		validator:     validator,
		processor:     processor,
		storage:       storage,
		agentRegistry: agentRegistry,
		logger:        logger,
		metrics:       testMetrics,
	}

	// Create router
	server.router = gin.New()
	server.setupRoutes()
	return server
}

func TestHandleAgentDiscovery(t *testing.T) {
	// Test the agent discovery endpoint (which should still work)
	server := createTestServerWithRealProcessor()

	// Register some test agents first
	agent1 := &agents.LocalAgent{
		Address:      "sales", // Will be normalized to sales@localhost
		DeliveryMode: "pull",
	}

	agent2 := &agents.LocalAgent{
		Address:      "support", // Will be normalized to support@localhost
		DeliveryMode: "push",
		PushTarget:   "https://support.example.com/webhook",
	}

	ctx := context.Background()

	err := server.agentRegistry.RegisterAgent(ctx, agent1)
	if err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}

	err = server.agentRegistry.RegisterAgent(ctx, agent2)
	if err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}

	// Test the agent discovery endpoint
	req, err := http.NewRequest("GET", "/v1/discovery/agents", nil)
	if err != nil {
		t.Fatalf("Failed to create agent discovery request: %v", err)
	}

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d for agent discovery, got %d", http.StatusOK, rr.Code)
	}

	var agentResponse map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &agentResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal agent discovery response: %v", err)
	}

	// Check agents array in the discovery response
	agents, ok := agentResponse["agents"].([]interface{})
	if !ok {
		t.Fatalf("Expected agents array in discovery response, got %v", agentResponse["agents"])
	}

	if len(agents) != 2 {
		t.Errorf("Expected 2 agents in discovery response, got %d", len(agents))
	}

	// Check agent_count in discovery response
	agentCount, ok := agentResponse["agent_count"].(float64)
	if !ok {
		t.Fatalf("Expected agent_count number in discovery response, got %v", agentResponse["agent_count"])
	}

	if int(agentCount) != 2 {
		t.Errorf("Expected agent_count 2 in discovery response, got %d", int(agentCount))
	}

	// Check domain in discovery response
	domain, ok := agentResponse["domain"].(string)
	if !ok {
		t.Fatalf("Expected domain string in discovery response, got %v", agentResponse["domain"])
	}

	if domain != "localhost" {
		t.Errorf("Expected domain 'localhost' in discovery response, got %s", domain)
	}

	// Verify agent information (without sensitive data)
	agentAddresses := make(map[string]bool)
	for _, agentInterface := range agents {
		agent, ok := agentInterface.(map[string]interface{})
		if !ok {
			t.Errorf("Expected agent to be object, got %v", agentInterface)
			continue
		}

		address, ok := agent["address"].(string)
		if !ok {
			t.Errorf("Expected agent address to be string, got %v", agent["address"])
			continue
		}

		agentAddresses[address] = true

		// Verify required fields exist
		if _, ok := agent["delivery_mode"]; !ok {
			t.Errorf("Expected delivery_mode field for agent %s", address)
		}

		if _, ok := agent["created_at"]; !ok {
			t.Errorf("Expected created_at field for agent %s", address)
		}

		// Verify sensitive fields are NOT included
		if _, ok := agent["api_key"]; ok {
			t.Errorf("API key should not be included in discovery response for agent %s", address)
		}

		if _, ok := agent["push_target"]; ok {
			t.Errorf("Push target should not be included in discovery response for agent %s", address)
		}
	}

	// Verify expected agents are present
	expectedAddresses := []string{"sales@localhost", "support@localhost"}
	for _, expectedAddr := range expectedAddresses {
		if !agentAddresses[expectedAddr] {
			t.Errorf("Expected agent %s not found in discovery response", expectedAddr)
		}
	}
}

func BenchmarkHandleSendMessage(b *testing.B) {
	server := createTestServer()

	requestBody := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@test.com"},
		Subject:    "Test Message",
		Payload:    json.RawMessage(`{"message": "Hello, World!"}`),
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		b.Fatalf("Failed to marshal request body: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
		if err != nil {
			b.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		server.router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			b.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
		}
	}
}

func BenchmarkHandleGetMessage(b *testing.B) {
	server := createTestServer()
	mockProcessor := server.processor.(*MockMessageProcessor)

	messageID := "01234567-89ab-7def-8123-456789abcdef"
	message := &types.Message{
		Version:        "1.0",
		MessageID:      messageID,
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@test.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"message": "Hello, World!"}`),
	}
	mockProcessor.messages[messageID] = message

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("GET", "/v1/messages/"+messageID, nil)
		if err != nil {
			b.Fatalf("Failed to create request: %v", err)
		}

		rr := httptest.NewRecorder()
		server.router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			b.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
		}
	}
}

// Test handleListMessages
func TestHandleListMessages_Success(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "viewer")

	req := httptest.NewRequest("GET", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 0 {
		t.Errorf("Expected total 0, got %v", response["total"])
	}

	if response["limit"].(float64) != 100 {
		t.Errorf("Expected limit 100, got %v", response["limit"])
	}

	if response["offset"].(float64) != 0 {
		t.Errorf("Expected offset 0, got %v", response["offset"])
	}
}

// TestHandleListMessages_NoAuth verifies message listing requires a key.
func TestHandleListMessages_NoAuth(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("GET", "/v1/messages", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d without auth, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestHandleListMessages_ReturnsStoredMessages verifies the list endpoint
// surfaces stored messages with their delivery status attached.
func TestHandleListMessages_ReturnsStoredMessages(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "sender")

	// Seed a message and its status directly into storage.
	now := time.Now().UTC()
	msg := &types.Message{
		Version:        "1.0",
		MessageID:      "019fbd30-9f27-75aa-8cd4-de1f14e011ab",
		IdempotencyKey: "11111111-1111-4111-8111-111111111111",
		Timestamp:      now,
		Sender:         "sender@localhost",
		Recipients:     []string{"recipient@localhost"},
		Subject:        "Hello",
		Schema:         "agntcy:test.hello.v1",
		Payload:        json.RawMessage(`{"msg":"hi"}`),
	}
	if err := mockStorage.StoreMessage(context.Background(), msg); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := mockStorage.StoreStatus(context.Background(), msg.MessageID, &types.MessageStatus{
		MessageID: msg.MessageID,
		Status:    types.StatusDelivered,
	}); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/messages?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response struct {
		Messages []map[string]interface{} `json:"messages"`
		Total    int                      `json:"total"`
		Limit    int                      `json:"limit"`
		Offset   int                      `json:"offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if response.Total != 1 {
		t.Errorf("Expected total 1, got %d", response.Total)
	}
	if len(response.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(response.Messages))
	}

	got := response.Messages[0]
	if got["message_id"] != msg.MessageID {
		t.Errorf("Expected message_id %s, got %v", msg.MessageID, got["message_id"])
	}
	if got["subject"] != "Hello" {
		t.Errorf("Expected subject Hello, got %v", got["subject"])
	}
	// Delivery status should be attached.
	delivery, ok := got["delivery"].(map[string]interface{})
	if !ok {
		t.Errorf("Expected delivery status attached, got %v", got["delivery"])
	} else if delivery["status"] != string(types.StatusDelivered) {
		t.Errorf("Expected delivery status %s, got %v", types.StatusDelivered, delivery["status"])
	}
	if got["status"] != string(types.StatusDelivered) {
		t.Errorf("Expected status %s, got %v", types.StatusDelivered, got["status"])
	}
}

func TestHandleListMessages_WithParameters(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "viewer")

	req := httptest.NewRequest("GET", "/v1/messages?limit=50&offset=10&status=delivered&sender=viewer@localhost&recipient=viewer@localhost&since=2023-01-01T00:00:00Z", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["limit"].(float64) != 50 {
		t.Errorf("Expected limit 50, got %v", response["limit"])
	}

	if response["offset"].(float64) != 10 {
		t.Errorf("Expected offset 10, got %v", response["offset"])
	}
}

// TestHandleListMessages_ForbiddenOtherAgent verifies filters referencing
// another agent are rejected.
func TestHandleListMessages_ForbiddenOtherAgent(t *testing.T) {
	server := createTestServer()
	registerTestAgent(t, server, "viewer")
	key := registerTestAgent(t, server, "other")

	req := httptest.NewRequest("GET", "/v1/messages?sender=viewer@localhost", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for other agent filter, got %d", http.StatusForbidden, w.Code)
	}
}

// TestHandleListMessages_BareNameFilters verifies that sender/recipient
// filters accept bare agent names (normalized to full addresses) exactly like
// full addresses, against real storage so the filter predicates are applied.
func TestHandleListMessages_BareNameFilters(t *testing.T) {
	server := createTestServerWithRealProcessor()
	key := registerTestAgent(t, server, "viewer")

	ctx := context.Background()
	now := time.Now().UTC()
	seed := []*types.Message{
		{
			MessageID:  "019fbd30-0001-75aa-8cd4-de1f14e011ab",
			Timestamp:  now,
			Sender:     "viewer@localhost",
			Recipients: []string{"peer@localhost"},
			Subject:    "sent",
		},
		{
			MessageID:  "019fbd30-0002-75aa-8cd4-de1f14e011ab",
			Timestamp:  now.Add(-time.Minute),
			Sender:     "peer@localhost",
			Recipients: []string{"viewer@localhost"},
			Subject:    "received",
		},
		{
			MessageID:  "019fbd30-0003-75aa-8cd4-de1f14e011ab",
			Timestamp:  now.Add(-2 * time.Minute),
			Sender:     "viewer@localhost",
			Recipients: []string{"viewer@localhost"},
			Subject:    "self",
		},
	}
	for _, msg := range seed {
		if err := server.storage.StoreMessage(ctx, msg); err != nil {
			t.Fatalf("seed %s: %v", msg.MessageID, err)
		}
	}

	tests := []struct {
		name    string
		query   string
		wantIDs []string
	}{
		{"bare sender", "?sender=viewer", []string{"019fbd30-0001-75aa-8cd4-de1f14e011ab", "019fbd30-0003-75aa-8cd4-de1f14e011ab"}},
		{"full sender", "?sender=viewer@localhost", []string{"019fbd30-0001-75aa-8cd4-de1f14e011ab", "019fbd30-0003-75aa-8cd4-de1f14e011ab"}},
		{"bare recipient", "?recipient=viewer", []string{"019fbd30-0002-75aa-8cd4-de1f14e011ab", "019fbd30-0003-75aa-8cd4-de1f14e011ab"}},
		{"full recipient", "?recipient=viewer@localhost", []string{"019fbd30-0002-75aa-8cd4-de1f14e011ab", "019fbd30-0003-75aa-8cd4-de1f14e011ab"}},
		// Both filters AND: only the self message was sent by and addressed
		// to the viewer.
		{"bare both", "?sender=viewer&recipient=viewer", []string{"019fbd30-0003-75aa-8cd4-de1f14e011ab"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/messages"+tt.query, nil)
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
			}

			var response struct {
				Messages []map[string]interface{} `json:"messages"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(response.Messages) != len(tt.wantIDs) {
				t.Fatalf("Expected %d messages, got %d", len(tt.wantIDs), len(response.Messages))
			}
			got := make(map[string]bool)
			for _, m := range response.Messages {
				got[m["message_id"].(string)] = true
			}
			for _, id := range tt.wantIDs {
				if !got[id] {
					t.Errorf("Expected message %s in response, got %v", id, got)
				}
			}
		})
	}
}

// TestHandleListMessages_BareNameFilterNormalized verifies that a bare-name
// filter is normalized to the full address before reaching the storage layer.
func TestHandleListMessages_BareNameFilterNormalized(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "viewer")

	req := httptest.NewRequest("GET", "/v1/messages?sender=viewer&recipient=viewer", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if len(mockStorage.listFilters) == 0 {
		t.Fatal("Expected ListMessages to be called")
	}
	filter := mockStorage.listFilters[0]
	if filter.Sender != "viewer@localhost" {
		t.Errorf("Expected normalized sender viewer@localhost in storage filter, got %q", filter.Sender)
	}
	if len(filter.Recipients) != 1 || filter.Recipients[0] != "viewer@localhost" {
		t.Errorf("Expected normalized recipient [viewer@localhost] in storage filter, got %v", filter.Recipients)
	}
}

// TestHandleListMessages_ForbiddenBareOtherAgent verifies that a bare name
// referencing another agent, and a foreign-domain address, are both rejected
// with 403.
func TestHandleListMessages_ForbiddenBareOtherAgent(t *testing.T) {
	server := createTestServer()
	registerTestAgent(t, server, "viewer")
	key := registerTestAgent(t, server, "other")

	tests := []struct {
		name  string
		query string
	}{
		{"bare other sender", "?sender=viewer"},
		{"bare other recipient", "?recipient=viewer"},
		{"foreign domain sender", "?sender=viewer@example.com"},
		{"invalid sender", "?sender=bad%20name%21"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/messages"+tt.query, nil)
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("Expected status %d, got %d: %s", http.StatusForbidden, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleListMessages_InvalidLimit(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "viewer")

	tests := []struct {
		name  string
		limit string
	}{
		{"negative limit", "-1"},
		{"zero limit", "0"},
		{"too large limit", "1001"},
		{"non-numeric limit", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/messages?limit="+tt.limit, nil)
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}

			var errorResponse types.ErrorResponse
			err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
			if err != nil {
				t.Fatalf("Failed to unmarshal error response: %v", err)
			}

			if errorResponse.Error.Code != "INVALID_LIMIT" {
				t.Errorf("Expected error code 'INVALID_LIMIT', got %s", errorResponse.Error.Code)
			}
		})
	}
}

func TestHandleListMessages_InvalidOffset(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "viewer")

	tests := []struct {
		name   string
		offset string
	}{
		{"negative offset", "-1"},
		{"non-numeric offset", "xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/messages?offset="+tt.offset, nil)
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}

			var errorResponse types.ErrorResponse
			err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
			if err != nil {
				t.Fatalf("Failed to unmarshal error response: %v", err)
			}

			if errorResponse.Error.Code != "INVALID_OFFSET" {
				t.Errorf("Expected error code 'INVALID_OFFSET', got %s", errorResponse.Error.Code)
			}
		})
	}
}

func TestHandleListMessages_InvalidSince(t *testing.T) {
	server := createTestServer()
	key := registerTestAgent(t, server, "viewer")

	req := httptest.NewRequest("GET", "/v1/messages?since=invalid-date", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var errorResponse types.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "INVALID_SINCE_FORMAT" {
		t.Errorf("Expected error code 'INVALID_SINCE_FORMAT', got %s", errorResponse.Error.Code)
	}
}

// TestHandleListMessages_MergedQueryUsesOrFilter verifies that the default
// "all traffic" path (no sender/recipient filter) issues exactly one
// OR-mode storage query covering both directions, with limit/offset intact,
// instead of two independently paginated queries.
func TestHandleListMessages_MergedQueryUsesOrFilter(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "viewer")

	now := time.Now().UTC()
	if err := mockStorage.StoreMessage(context.Background(), &types.Message{
		MessageID:  "019fbd30-0001-75aa-8cd4-de1f14e011ab",
		Timestamp:  now,
		Sender:     "viewer@localhost",
		Recipients: []string{"peer@localhost"},
		Subject:    "sent",
	}); err != nil {
		t.Fatalf("seed sent message: %v", err)
	}
	if err := mockStorage.StoreMessage(context.Background(), &types.Message{
		MessageID:  "019fbd30-0002-75aa-8cd4-de1f14e011ab",
		Timestamp:  now.Add(-time.Minute),
		Sender:     "peer@localhost",
		Recipients: []string{"viewer@localhost"},
		Subject:    "received",
	}); err != nil {
		t.Fatalf("seed received message: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/messages?limit=5&offset=2", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// The merged path must issue one OR-mode ListMessages query (limit/offset
	// intact) and one OR-mode CountMessages query for the total, so the count
	// covers the full filtered set without materializing it.
	if len(mockStorage.listFilters) != 1 {
		t.Fatalf("Expected 1 ListMessages query for the merged path, got %d", len(mockStorage.listFilters))
	}
	filter := mockStorage.listFilters[0]
	if !filter.Or {
		t.Error("Expected the merged query to use OR semantics")
	}
	if filter.Sender != "viewer@localhost" {
		t.Errorf("Expected merged query sender viewer@localhost, got %q", filter.Sender)
	}
	if len(filter.Recipients) != 1 || filter.Recipients[0] != "viewer@localhost" {
		t.Errorf("Expected merged query recipients [viewer@localhost], got %v", filter.Recipients)
	}
	if filter.Limit != 5 {
		t.Errorf("Expected limit 5 propagated to storage, got %d", filter.Limit)
	}
	if filter.Offset != 2 {
		t.Errorf("Expected offset 2 propagated to storage, got %d", filter.Offset)
	}

	// The total-count query must keep OR semantics; pagination is ignored by
	// the storage backends for counting.
	if len(mockStorage.countFilters) != 1 {
		t.Fatalf("Expected 1 CountMessages query for the merged path, got %d", len(mockStorage.countFilters))
	}
	countFilter := mockStorage.countFilters[0]
	if !countFilter.Or {
		t.Error("Expected the total-count query to keep OR semantics")
	}
	if countFilter.Sender != "viewer@localhost" {
		t.Errorf("Expected count query sender viewer@localhost, got %q", countFilter.Sender)
	}
	if len(countFilter.Recipients) != 1 || countFilter.Recipients[0] != "viewer@localhost" {
		t.Errorf("Expected count query recipients [viewer@localhost], got %v", countFilter.Recipients)
	}

	// Both directions must be surfaced in the response.
	var response struct {
		Messages []map[string]interface{} `json:"messages"`
		Total    int                      `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if response.Total != 2 {
		t.Errorf("Expected total 2, got %d", response.Total)
	}
	if len(response.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(response.Messages))
	}
	subjects := map[string]bool{}
	for _, m := range response.Messages {
		subjects[m["subject"].(string)] = true
	}
	if !subjects["sent"] || !subjects["received"] {
		t.Errorf("Expected both sent and received messages in the merged result, got %v", subjects)
	}
}

// TestHandleListMessages_MergedQueryPagination runs the merged "all traffic"
// path against real storage and verifies that limit/offset apply to the
// merged, newest-first result set: pages never exceed limit, consecutive
// pages neither overlap nor drop messages, and the ordering is globally
// newest-first.
func TestHandleListMessages_MergedQueryPagination(t *testing.T) {
	server := createTestServerWithRealProcessor()
	key := registerTestAgent(t, server, "viewer")

	ctx := context.Background()
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	const total = 8
	// Newest-first expected order; even indices are sent by the viewer, odd
	// indices are received by it.
	newestFirst := make([]string, 0, total)
	for i := 0; i < total; i++ {
		sender, recipient := "viewer@localhost", "peer@localhost"
		if i%2 == 1 {
			sender, recipient = "peer@localhost", "viewer@localhost"
		}
		msgID := fmt.Sprintf("019fbd30-%04d-75aa-8cd4-de1f14e011ab", i)
		msg := &types.Message{
			Version:        "1.0",
			MessageID:      msgID,
			IdempotencyKey: "11111111-1111-4111-8111-111111111111",
			Timestamp:      base.Add(time.Duration(total-1-i) * time.Minute),
			Sender:         sender,
			Recipients:     []string{recipient},
			Subject:        fmt.Sprintf("merged-%d", i),
			Payload:        json.RawMessage(`{"i":` + fmt.Sprintf("%d", i) + `}`),
		}
		if err := server.storage.StoreMessage(ctx, msg); err != nil {
			t.Fatalf("seed %s: %v", msgID, err)
		}
		newestFirst = append(newestFirst, msgID)
	}

	// Page through the full set with limit=3.
	const limit = 3
	seen := make(map[string]struct{})
	var all []string
	for offset := 0; offset < total; offset += limit {
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/messages?limit=%d&offset=%d", limit, offset), nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("offset=%d: expected status %d, got %d: %s", offset, http.StatusOK, w.Code, w.Body.String())
		}

		var response struct {
			Messages []struct {
				MessageID string `json:"message_id"`
				Timestamp string `json:"timestamp"`
			} `json:"messages"`
			Total  int `json:"total"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("offset=%d: unmarshal: %v", offset, err)
		}

		if response.Total != total {
			t.Errorf("offset=%d: expected total %d, got %d", offset, total, response.Total)
		}
		if len(response.Messages) > limit {
			t.Errorf("offset=%d: expected at most %d messages, got %d", offset, limit, len(response.Messages))
		}

		// Newest-first within the page.
		for j := 1; j < len(response.Messages); j++ {
			prev, err := time.Parse(time.RFC3339, response.Messages[j-1].Timestamp)
			if err != nil {
				t.Fatalf("offset=%d: parse previous timestamp: %v", offset, err)
			}
			cur, err := time.Parse(time.RFC3339, response.Messages[j].Timestamp)
			if err != nil {
				t.Fatalf("offset=%d: parse current timestamp: %v", offset, err)
			}
			if !prev.After(cur) {
				t.Errorf("offset=%d: page not newest-first: %s before %s",
					offset, response.Messages[j-1].MessageID, response.Messages[j].MessageID)
			}
		}

		for _, m := range response.Messages {
			if _, dup := seen[m.MessageID]; dup {
				t.Errorf("offset=%d: duplicate message %s across pages", offset, m.MessageID)
			}
			seen[m.MessageID] = struct{}{}
			all = append(all, m.MessageID)
		}
	}

	if len(all) != total {
		t.Errorf("Expected %d messages across pages, got %d", total, len(all))
	}
	for i, id := range newestFirst {
		if i < len(all) && all[i] != id {
			t.Errorf("Position %d: expected %s, got %s (merged list must be newest-first)", i, id, all[i])
		}
	}
}

// TestHandleListMessages_CountFailure verifies that a CountMessages failure
// surfaces as an error response instead of being silently swallowed (the
// previous total computation ignored storage errors and undercounted).
func TestHandleListMessages_CountFailure(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "viewer")

	mockStorage.countError = fmt.Errorf("count exploded")

	req := httptest.NewRequest("GET", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}

	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errorResponse.Error.Code != "MESSAGE_LIST_FAILED" {
		t.Errorf("Expected error code MESSAGE_LIST_FAILED, got %s", errorResponse.Error.Code)
	}
}

// TestHandleListMessages_StatusesBatch verifies that delivery statuses for the
// whole page are fetched in a single GetStatuses call (not N+1 GetStatus
// calls) and attached to the response items.
func TestHandleListMessages_StatusesBatch(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "viewer")

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		msgID := fmt.Sprintf("019fbd30-%04d-75aa-8cd4-de1f14e011ab", i)
		if err := mockStorage.StoreMessage(context.Background(), &types.Message{
			MessageID:  msgID,
			Timestamp:  now.Add(-time.Duration(i) * time.Minute),
			Sender:     "viewer@localhost",
			Recipients: []string{"peer@localhost"},
			Subject:    fmt.Sprintf("msg-%d", i),
		}); err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
		if err := mockStorage.StoreStatus(context.Background(), msgID, &types.MessageStatus{
			MessageID: msgID,
			Status:    types.StatusDelivered,
		}); err != nil {
			t.Fatalf("seed status %d: %v", i, err)
		}
	}

	req := httptest.NewRequest("GET", "/v1/messages?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Statuses must be fetched in exactly one batch containing every page
	// message ID, not one GetStatus call per message.
	if len(mockStorage.statusBatches) != 1 {
		t.Fatalf("Expected exactly 1 GetStatuses batch, got %d", len(mockStorage.statusBatches))
	}
	batch := mockStorage.statusBatches[0]
	if len(batch) != 3 {
		t.Fatalf("Expected batch of 3 message IDs, got %d", len(batch))
	}
	got := make(map[string]bool)
	for _, id := range batch {
		got[id] = true
	}
	for i := 0; i < 3; i++ {
		msgID := fmt.Sprintf("019fbd30-%04d-75aa-8cd4-de1f14e011ab", i)
		if !got[msgID] {
			t.Errorf("Expected batch to include %s, got %v", msgID, batch)
		}
	}

	// Each response item must carry its delivery status.
	var response struct {
		Messages []map[string]interface{} `json:"messages"`
		Total    int                      `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(response.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(response.Messages))
	}
	for _, m := range response.Messages {
		if m["status"] != string(types.StatusDelivered) {
			t.Errorf("Expected status %s on message %v, got %v", types.StatusDelivered, m["message_id"], m["status"])
		}
		if _, ok := m["delivery"].(map[string]interface{}); !ok {
			t.Errorf("Expected delivery attached to message %v", m["message_id"])
		}
	}
}

// TestHandleListMessages_StatusesFailure verifies that a GetStatuses failure
// surfaces as an error response instead of silently dropping statuses.
func TestHandleListMessages_StatusesFailure(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	key := registerTestAgent(t, server, "viewer")

	if err := mockStorage.StoreMessage(context.Background(), &types.Message{
		MessageID:  "019fbd30-0000-75aa-8cd4-de1f14e011ab",
		Timestamp:  time.Now().UTC(),
		Sender:     "viewer@localhost",
		Recipients: []string{"peer@localhost"},
		Subject:    "hello",
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	mockStorage.statusesError = fmt.Errorf("statuses exploded")

	req := httptest.NewRequest("GET", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}

	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errorResponse.Error.Code != "MESSAGE_LIST_FAILED" {
		t.Errorf("Expected error code MESSAGE_LIST_FAILED, got %s", errorResponse.Error.Code)
	}
}

// Test handleGetCapabilities
func TestHandleGetCapabilities_Success(t *testing.T) {
	server := createTestServerWithRealProcessor()

	req := httptest.NewRequest("GET", "/v1/capabilities/localhost", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// The response should contain capabilities
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should have basic AMTP capabilities
	if response["version"] == nil {
		t.Error("Expected version in capabilities response")
	}
}

func TestHandleGetCapabilities_EmptyDomain(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("GET", "/v1/capabilities/", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleGetCapabilities_OwnDomain(t *testing.T) {
	server := createTestServerWithRealProcessor()
	ctx := context.Background()

	// Register some agents to test schema inclusion
	agent := &agents.LocalAgent{
		Address:          "test",
		DeliveryMode:     "pull",
		SupportedSchemas: []string{"agntcy:example.test.v1", "agntcy:example.order.v1"},
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/capabilities/localhost", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should include schemas from registered agents
	if schemas, ok := response["schemas"].([]interface{}); ok {
		if len(schemas) == 0 {
			t.Error("Expected schemas to be included for own domain")
		}
	}
}

// Test handleDiscoverAgentsByDomain
func TestHandleDiscoverAgentsByDomain_Success(t *testing.T) {
	server := createTestServerWithRealProcessor()
	ctx := context.Background()

	// Register test agent
	agent := &agents.LocalAgent{
		Address:      "test",
		DeliveryMode: "pull",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/discovery/agents/localhost", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["domain"] != "localhost" {
		t.Errorf("Expected domain 'localhost', got %v", response["domain"])
	}
}

func TestHandleDiscoverAgentsByDomain_NotFound(t *testing.T) {
	server := createTestServerWithRealProcessor()

	req := httptest.NewRequest("GET", "/v1/discovery/agents/other.domain.com", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	var errorResponse types.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "DOMAIN_NOT_FOUND" {
		t.Errorf("Expected error code 'DOMAIN_NOT_FOUND', got %s", errorResponse.Error.Code)
	}
}

// Test agent management handlers
func TestHandleRegisterAgent_Success(t *testing.T) {
	server := createTestServer()

	agent := agents.LocalAgent{
		Address:      "newagent",
		DeliveryMode: "pull",
	}

	body, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Failed to marshal agent: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/admin/agents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["message"] != "Agent registered successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}
}

func TestHandleRegisterAgent_InvalidJSON(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("POST", "/v1/admin/agents", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var errorResponse types.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "INVALID_REQUEST_FORMAT" {
		t.Errorf("Expected error code 'INVALID_REQUEST_FORMAT', got %s", errorResponse.Error.Code)
	}
}

func TestHandleUnregisterAgent_Success(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// First register an agent
	agent := &agents.LocalAgent{
		Address:      "testagent",
		DeliveryMode: "pull",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/v1/admin/agents/testagent", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["message"] != "Agent unregistered successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}
}

func TestHandleUnregisterAgent_NotFound(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("DELETE", "/v1/admin/agents/nonexistent@localhost", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var errorResponse types.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "AGENT_UNREGISTRATION_FAILED" {
		t.Errorf("Expected error code 'AGENT_UNREGISTRATION_FAILED', got %s", errorResponse.Error.Code)
	}
}

func TestHandleListAgents_Success(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// Register test agents
	agent1 := &agents.LocalAgent{
		Address:      "agent1",
		DeliveryMode: "pull",
	}
	agent2 := &agents.LocalAgent{
		Address:      "agent2",
		DeliveryMode: "push",
		PushTarget:   "https://example.com/webhook",
	}

	err := server.agentRegistry.RegisterAgent(ctx, agent1)
	if err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}

	err = server.agentRegistry.RegisterAgent(ctx, agent2)
	if err != nil {
		t.Fatalf("Failed to register agent2: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/admin/agents", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["count"].(float64) != 2 {
		t.Errorf("Expected count 2, got %v", response["count"])
	}

	agents, ok := response["agents"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected agents to be a map, got %T", response["agents"])
	}

	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}
}

// TestHandleRotateAgentKey_Success verifies a new key is issued and the
// old key no longer authenticates.
func TestHandleRotateAgentKey_Success(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// Register an agent and capture the original API key.
	agent := &agents.LocalAgent{Address: "rotate-me", DeliveryMode: "pull"}
	if err := server.agentRegistry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	oldKey := agent.APIKey

	req := httptest.NewRequest("POST", "/v1/admin/agents/rotate-me/rotate-key", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response struct {
		Message string `json:"message"`
		Address string `json:"address"`
		APIKey  string `json:"api_key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if response.APIKey == "" {
		t.Fatal("Expected a new api_key in the response")
	}
	if response.APIKey == oldKey {
		t.Error("Expected the new key to differ from the old key")
	}
	if response.Address != "rotate-me@localhost" {
		t.Errorf("Expected normalized address rotate-me@localhost, got %s", response.Address)
	}

	// Old key must no longer verify.
	if server.agentRegistry.VerifyAPIKey(ctx, "rotate-me@localhost", oldKey) {
		t.Error("Old API key should be invalid after rotation")
	}
	// New key must verify.
	if !server.agentRegistry.VerifyAPIKey(ctx, "rotate-me@localhost", response.APIKey) {
		t.Error("New API key should verify after rotation")
	}
}

// TestHandleRotateAgentKey_NotFound verifies rotation fails for unknown agents.
func TestHandleRotateAgentKey_NotFound(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("POST", "/v1/admin/agents/nonexistent/rotate-key", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if errorResponse.Error.Code != "AGENT_KEY_ROTATION_FAILED" {
		t.Errorf("Expected AGENT_KEY_ROTATION_FAILED, got %s", errorResponse.Error.Code)
	}
}

// TestHandleUpdateAgent_Success verifies delivery mode and schemas can be
// updated on an existing agent.
func TestHandleUpdateAgent_Success(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	agent := &agents.LocalAgent{Address: "upd-agent", DeliveryMode: "pull"}
	if err := server.agentRegistry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	body := []byte(`{"delivery_mode":"push","push_target":"https://hooks.example.com/a","supported_schemas":["agntcy:upd.test.v1"]}`)
	req := httptest.NewRequest("PATCH", "/v1/admin/agents/upd-agent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response struct {
		Message string `json:"message"`
		Agent   struct {
			Address          string   `json:"address"`
			DeliveryMode     string   `json:"delivery_mode"`
			PushTarget       string   `json:"push_target"`
			SupportedSchemas []string `json:"supported_schemas"`
			APIKey           string   `json:"api_key"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if response.Agent.DeliveryMode != "push" {
		t.Errorf("Expected delivery_mode push, got %s", response.Agent.DeliveryMode)
	}
	if response.Agent.PushTarget != "https://hooks.example.com/a" {
		t.Errorf("Expected push target updated, got %s", response.Agent.PushTarget)
	}
	if len(response.Agent.SupportedSchemas) != 1 || response.Agent.SupportedSchemas[0] != "agntcy:upd.test.v1" {
		t.Errorf("Expected schemas updated, got %v", response.Agent.SupportedSchemas)
	}
	if response.Agent.APIKey != "" {
		t.Error("API key must be redacted in update response")
	}

	// Verify storage reflects the change.
	stored, err := server.agentRegistry.GetAgent(ctx, "upd-agent@localhost")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if stored.DeliveryMode != "push" {
		t.Errorf("Expected stored delivery_mode push, got %s", stored.DeliveryMode)
	}
}

// TestHandleUpdateAgent_PushWithoutTarget verifies push mode without a
// target URL is rejected.
func TestHandleUpdateAgent_PushWithoutTarget(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	agent := &agents.LocalAgent{Address: "upd-agent2", DeliveryMode: "pull"}
	if err := server.agentRegistry.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	body := []byte(`{"delivery_mode":"push"}`)
	req := httptest.NewRequest("PATCH", "/v1/admin/agents/upd-agent2", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if errorResponse.Error.Code != "AGENT_UPDATE_FAILED" {
		t.Errorf("Expected AGENT_UPDATE_FAILED, got %s", errorResponse.Error.Code)
	}
}

// TestHandleUpdateAgent_NotFound verifies updating an unknown agent fails.
func TestHandleUpdateAgent_NotFound(t *testing.T) {
	server := createTestServer()

	body := []byte(`{"delivery_mode":"pull"}`)
	req := httptest.NewRequest("PATCH", "/v1/admin/agents/ghost", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if errorResponse.Error.Code != "AGENT_UPDATE_FAILED" {
		t.Errorf("Expected AGENT_UPDATE_FAILED, got %s", errorResponse.Error.Code)
	}
}

// Test inbox handlers
func TestHandleGetInbox_Success(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// Register agent with API key
	agent := &agents.LocalAgent{
		Address:      "testuser",
		DeliveryMode: "pull",
		APIKey:       "valid-api-key",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/inbox/testuser@localhost", nil)
	req.Header.Set("Authorization", "Bearer valid-api-key")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["recipient"] != "testuser@localhost" {
		t.Errorf("Expected recipient 'testuser@localhost', got %v", response["recipient"])
	}

	if response["count"].(float64) != 0 {
		t.Errorf("Expected count 0, got %v", response["count"])
	}
}

func TestHandleGetInbox_Unauthorized(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("GET", "/v1/inbox/testuser@localhost", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var errorResponse types.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "MISSING_AUTHORIZATION" {
		t.Errorf("Expected error code 'MISSING_AUTHORIZATION', got %s", errorResponse.Error.Code)
	}
}

func TestHandleGetInbox_InvalidAPIKey(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// Register agent with different API key
	agent := &agents.LocalAgent{
		Address:      "testuser",
		DeliveryMode: "pull",
		APIKey:       "correct-api-key",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/inbox/testuser@localhost", nil)
	req.Header.Set("Authorization", "Bearer wrong-api-key")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, w.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "ACCESS_DENIED" {
		t.Errorf("Expected error code 'ACCESS_DENIED', got %s", errorResponse.Error.Code)
	}
}

func TestHandleAcknowledgeMessage_Success(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)
	ctx := context.Background()

	// Register agent with API key
	agent := &agents.LocalAgent{
		Address:      "testuser",
		DeliveryMode: "pull",
		APIKey:       "valid-api-key",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Add a message to acknowledge
	messageID := "test-message-123"
	message := &types.Message{
		MessageID:  messageID,
		Recipients: []string{"testuser@localhost"},
	}
	mockStorage.messages[messageID] = message

	req := httptest.NewRequest("DELETE", "/v1/inbox/testuser@localhost/"+messageID, nil)
	req.Header.Set("Authorization", "Bearer valid-api-key")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["message"] != "Message acknowledged successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}
}

func TestHandleAcknowledgeMessage_NotFound(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// Register agent with API key
	agent := &agents.LocalAgent{
		Address:      "testuser",
		DeliveryMode: "pull",
		APIKey:       "valid-api-key",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/v1/inbox/testuser@localhost/nonexistent-message", nil)
	req.Header.Set("Authorization", "Bearer valid-api-key")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	var errorResponse types.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "MESSAGE_NOT_FOUND" {
		t.Errorf("Expected error code 'MESSAGE_NOT_FOUND', got %s", errorResponse.Error.Code)
	}
}

// Test verifyAgentAccess function
func TestVerifyAgentAccess_Success(t *testing.T) {
	server := createTestServer()
	ctx := context.Background()

	// Register agent with API key
	agent := &agents.LocalAgent{
		Address:      "testuser",
		DeliveryMode: "pull",
		APIKey:       "valid-api-key",
	}
	err := server.agentRegistry.RegisterAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Create test context
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		if server.verifyAgentAccess(c, "testuser@localhost") {
			c.JSON(http.StatusOK, gin.H{"access": "granted"})
		}
		// If access denied, verifyAgentAccess handles the response
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestVerifyAgentAccess_EmptyAPIKey(t *testing.T) {
	server := createTestServer()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		server.verifyAgentAccess(c, "testuser@localhost")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var errorResponse types.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
	if err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errorResponse.Error.Code != "EMPTY_API_KEY" {
		t.Errorf("Expected error code 'EMPTY_API_KEY', got %s", errorResponse.Error.Code)
	}
}

func (m *MockStorage) StoreWorkflow(ctx context.Context, state *types.Workflow) error {
	return nil
}
func (m *MockStorage) GetWorkflow(ctx context.Context, workflowID string) (*types.Workflow, error) {
	return nil, fmt.Errorf("not found")
}
func (m *MockStorage) UpdateWorkflowStatus(ctx context.Context, workflowID string, status types.WorkflowStatus) error {
	return nil
}
func (m *MockStorage) UpdateWorkflowParticipant(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte) error {
	return nil
}
func (m *MockStorage) UpdateWorkflowParticipantAtomic(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte, expectedVersion int) error {
	return nil
}
func (m *MockStorage) UpdateWorkflowStatusAtomic(ctx context.Context, workflowID string, status types.WorkflowStatus, expectedVersion int) error {
	return nil
}
func (m *MockStorage) ListTimedOutWorkflows(ctx context.Context) ([]*types.Workflow, error) {
	return nil, nil
}

func TestHandleSendMessage_WorkflowResponseRoutedByWorkflowID(t *testing.T) {
	server := createTestServer()

	workflowID, err := uuid.GenerateV7()
	if err != nil {
		t.Fatalf("Failed to generate workflow ID: %v", err)
	}

	var gotWorkflowID string
	var gotSender string
	server.workflow = &processing.MockWorkflowManager{
		ProcessResponseFunc: func(ctx context.Context, wfID string, replyMsg *types.Message) error {
			gotWorkflowID = wfID
			gotSender = replyMsg.Sender
			return nil
		},
	}

	requestBody := types.SendMessageRequest{
		Sender:       "agent1@example.com",
		Recipients:   []string{"initiator@test.com"},
		Subject:      "Workflow step done",
		ResponseType: "workflow_response",
		WorkflowID:   workflowID,
		Payload:      json.RawMessage(`{"status": "completed"}`),
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if gotWorkflowID != workflowID {
		t.Errorf("ProcessResponse called with workflow ID %q, want %q", gotWorkflowID, workflowID)
	}
	if gotSender != "agent1@example.com" {
		t.Errorf("ProcessResponse reply sender = %q, want agent1@example.com", gotSender)
	}
}

func TestHandleSendMessage_ResponseIncludesWorkflowID(t *testing.T) {
	server := createTestServer()

	workflowID, err := uuid.GenerateV7()
	if err != nil {
		t.Fatalf("Failed to generate workflow ID: %v", err)
	}

	mockProcessor := server.processor.(*MockMessageProcessor)
	mockProcessor.processResult = &processing.ProcessingResult{
		Status:     types.StatusQueued,
		WorkflowID: workflowID,
		Recipients: []types.RecipientStatus{
			{Address: "agent1@test.com", Status: types.StatusQueued},
		},
	}

	requestBody := types.SendMessageRequest{
		Sender:     "initiator@example.com",
		Recipients: []string{"agent1@test.com"},
		Subject:    "Coordinated task",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			Timeout:           60,
			RequiredResponses: []string{"agent1@test.com"},
		},
		Payload: json.RawMessage(`{"task": "do it"}`),
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Expected status code %d, got %d: %s", http.StatusAccepted, rr.Code, rr.Body.String())
	}

	var response types.SendMessageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.WorkflowID != workflowID {
		t.Errorf("Response workflow_id = %q, want %q", response.WorkflowID, workflowID)
	}
}

func TestHandleSendMessage_WorkflowErrorResponsesReachEngine(t *testing.T) {
	workflowID, err := uuid.GenerateV7()
	if err != nil {
		t.Fatalf("Failed to generate workflow ID: %v", err)
	}

	cases := []struct {
		name          string
		responseType  string
		workflowID    string
		wantIntercept bool
	}{
		{"workflow_error with workflow_id", "workflow_error", workflowID, true},
		{"plain error with workflow_id", "error", workflowID, true},
		{"plain error without workflow reference", "error", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := createTestServer()

			var gotWorkflowID string
			var gotResponseType string
			called := false
			server.workflow = &processing.MockWorkflowManager{
				ProcessResponseFunc: func(ctx context.Context, wfID string, replyMsg *types.Message) error {
					called = true
					gotWorkflowID = wfID
					gotResponseType = replyMsg.ResponseType
					return nil
				},
			}

			requestBody := types.SendMessageRequest{
				Sender:       "agent1@example.com",
				Recipients:   []string{"initiator@test.com"},
				Subject:      "Workflow step failed",
				ResponseType: tc.responseType,
				WorkflowID:   tc.workflowID,
				Payload:      json.RawMessage(`{"error": "downstream unavailable"}`),
			}

			body, err := json.Marshal(requestBody)
			if err != nil {
				t.Fatalf("Failed to marshal request body: %v", err)
			}

			req, err := http.NewRequest("POST", "/v1/messages", bytes.NewBuffer(body))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status code %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
			}

			if called != tc.wantIntercept {
				t.Fatalf("ProcessResponse called = %v, want %v", called, tc.wantIntercept)
			}
			if tc.wantIntercept {
				if gotWorkflowID != tc.workflowID {
					t.Errorf("ProcessResponse workflow ID = %q, want %q", gotWorkflowID, tc.workflowID)
				}
				if gotResponseType != tc.responseType {
					t.Errorf("ProcessResponse reply response_type = %q, want %q", gotResponseType, tc.responseType)
				}
			}
		})
	}
}
