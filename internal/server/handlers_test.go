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

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/logging"
	"github.com/amtp-protocol/agentry/internal/metrics"
	"github.com/amtp-protocol/agentry/internal/processing"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/internal/validation"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// MockMessageProcessor for testing
type MockMessageProcessor struct {
	processResult *processing.ProcessingResult
	processError  error
	messages      map[string]*types.Message
	statuses      map[string]*types.MessageStatus
}

func NewMockMessageProcessor() *MockMessageProcessor {
	return &MockMessageProcessor{
		messages: make(map[string]*types.Message),
		statuses: make(map[string]*types.MessageStatus),
	}
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

// GetInboxMessages returns messages for a specific recipient (mock implementation)
func (m *MockMessageProcessor) GetInboxMessages(recipient string) []*types.Message {
	var inboxMessages []*types.Message

	// For testing, return messages that match the recipient
	for _, message := range m.messages {
		for _, msgRecipient := range message.Recipients {
			if msgRecipient == recipient {
				inboxMessages = append(inboxMessages, message)
				break
			}
		}
	}

	return inboxMessages
}

// AcknowledgeMessage marks a message as acknowledged (mock implementation)
func (m *MockMessageProcessor) AcknowledgeMessage(recipient, messageID string) error {
	// For testing, just check if the message exists
	if _, exists := m.messages[messageID]; !exists {
		return fmt.Errorf("message not found: %s", messageID)
	}

	// In a real implementation, this would update the recipient status
	// For testing, we'll just return success
	return nil
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
	logger := logging.NewLogger(cfg.Logging).WithComponent("server")

	// Create agent registry for testing
	agentRegistryConfig := agents.RegistryConfig{
		LocalDomain:   cfg.Server.Domain,
		SchemaManager: nil, // No schema manager in basic test
	}
	agentRegistry := agents.NewRegistry(agentRegistryConfig)

	// Create a new metrics registry for each test to avoid conflicts
	testMetrics := &metrics.Metrics{
		HTTPRequestsTotal:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_http_requests_total", Help: "Test HTTP requests"}, []string{"method", "path", "status_code"}),
		HTTPRequestDuration:       prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_http_request_duration", Help: "Test HTTP duration"}, []string{"method", "path", "status_code"}),
		HTTPRequestsInFlight:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_http_requests_in_flight", Help: "Test HTTP requests in flight"}),
		MessagesTotal:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_messages_total", Help: "Test messages"}, []string{"status", "coordination_type"}),
		MessageProcessingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_processing_duration", Help: "Test processing duration"}, []string{"status", "coordination_type"}),
		MessagesInFlight:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_messages_in_flight", Help: "Test messages in flight"}),
		MessageSizeBytes:          prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_message_size", Help: "Test message size"}, []string{"schema"}),
		DeliveriesTotal:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_deliveries_total", Help: "Test deliveries"}, []string{"status"}),
		DeliveryDuration:          prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_delivery_duration", Help: "Test delivery duration"}, []string{"status"}),
		DeliveryAttempts:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_delivery_attempts", Help: "Test delivery attempts"}, []string{"status"}),
		DeliveryRetries:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_delivery_retries", Help: "Test delivery retries"}, []string{"reason"}),
		DiscoveryTotal:            prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_discovery_total", Help: "Test discovery"}, []string{"status"}),
		DiscoveryDuration:         prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_discovery_duration", Help: "Test discovery duration"}, []string{"domain", "method", "status"}),
		DiscoveryCacheHits:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_discovery_cache", Help: "Test discovery cache"}, []string{"domain"}),
		ConnectionsActive:         prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_connections_active", Help: "Test connections"}),
		MemoryUsageBytes:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_memory_usage", Help: "Test memory"}),
		GoroutinesActive:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_goroutines", Help: "Test goroutines"}),
		ErrorsTotal:               prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_errors_total", Help: "Test errors"}, []string{"component", "error_code", "error_type"}),
	}

	router := gin.New()

	server := &Server{
		config:        cfg,
		router:        router,
		discovery:     discoveryService,
		validator:     validator,
		processor:     processor,
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
	mockProcessor := server.processor.(*MockMessageProcessor)

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
	mockProcessor.messages[message.MessageID] = message

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
	mockProcessor := server.processor.(*MockMessageProcessor)

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
	mockProcessor.statuses[messageID] = status

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

	// Create agent registry
	agentRegistryConfig := agents.RegistryConfig{
		LocalDomain:   cfg.Server.Domain,
		SchemaManager: nil, // No schema manager in tests
	}
	agentRegistry := agents.NewRegistry(agentRegistryConfig)

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
	processor := processing.NewMessageProcessor(discoveryService, deliveryEngine)

	logger := logging.NewLogger(cfg.Logging).WithComponent("server")

	// Create a new metrics registry for each test to avoid conflicts
	testMetrics := &metrics.Metrics{
		HTTPRequestsTotal:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_http_requests_total", Help: "Test HTTP requests"}, []string{"method", "path", "status_code"}),
		HTTPRequestDuration:       prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_http_request_duration", Help: "Test HTTP duration"}, []string{"method", "path", "status_code"}),
		HTTPRequestsInFlight:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_http_requests_in_flight", Help: "Test HTTP requests in flight"}),
		MessagesTotal:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_messages_total", Help: "Test messages"}, []string{"status", "coordination_type"}),
		MessageProcessingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_processing_duration", Help: "Test processing duration"}, []string{"status", "coordination_type"}),
		MessagesInFlight:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_messages_in_flight", Help: "Test messages in flight"}),
		DeliveryAttempts:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_delivery_attempts", Help: "Test delivery attempts"}, []string{"status", "domain"}),
		DeliveryDuration:          prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_delivery_duration", Help: "Test delivery duration"}, []string{"status", "domain"}),
	}

	server := &Server{
		config:        cfg,
		discovery:     discoveryService,
		validator:     validator,
		processor:     processor,
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

	err := server.agentRegistry.RegisterAgent(agent1)
	if err != nil {
		t.Fatalf("Failed to register agent1: %v", err)
	}

	err = server.agentRegistry.RegisterAgent(agent2)
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
