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
	var messages []*types.Message
	for _, msg := range m.messages {
		messages = append(messages, msg)
	}
	return messages, nil
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
	m.agents[agent.Address] = agent
	return nil
}

func (m *MockStorage) GetAgent(ctx context.Context, agentAddress string) (*agents.LocalAgent, error) {
	agent, exists := m.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}

	return agent, nil
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

	// First, send a message to store it
	message := &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@test.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"message": "Hello, World!"}`),
	}
	mockStorage.messages[message.MessageID] = message

	req, err := http.NewRequest("GET", "/v1/messages/"+message.MessageID, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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

	req, err := http.NewRequest("GET", "/v1/messages/invalid-id", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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

	req, err := http.NewRequest("GET", "/v1/messages/01234567-89ab-7def-8123-456789abcdef", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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

func TestHandleGetMessageStatus_Success(t *testing.T) {
	server := createTestServer()
	mockStorage := server.storage.(*MockStorage)

	messageID := "01234567-89ab-7def-8123-456789abcdef"
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

	req, err := http.NewRequest("GET", "/v1/messages/invalid-id/status", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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

	req, err := http.NewRequest("GET", "/v1/messages/01234567-89ab-7def-8123-456789abcdef/status", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

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

	req := httptest.NewRequest("GET", "/v1/messages", nil)
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

func TestHandleListMessages_WithParameters(t *testing.T) {
	server := createTestServer()

	req := httptest.NewRequest("GET", "/v1/messages?limit=50&offset=10&status=delivered&sender=test@example.com&recipient=user@example.com&since=2023-01-01T00:00:00Z", nil)
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

func TestHandleListMessages_InvalidLimit(t *testing.T) {
	server := createTestServer()

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

	req := httptest.NewRequest("GET", "/v1/messages?since=invalid-date", nil)
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
