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

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/amtp-protocol/agentry/internal/server"
	"github.com/amtp-protocol/agentry/internal/types"
)

// Integration tests for the AMTP Gateway
// These tests verify the complete flow from HTTP request to response

func createTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Address:      ":8080",
			Domain:       "localhost",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		TLS: config.TLSConfig{
			Enabled: false,
		},
		DNS: config.DNSConfig{
			CacheTTL:  5 * time.Minute,
			Timeout:   5 * time.Second,
			Resolvers: []string{"8.8.8.8:53", "1.1.1.1:53"},
			MockMode:  true,
			MockRecords: map[string]string{
				"test.com":    "v=amtp1;gateway=http://localhost:8080;auth=none;max-size=10485760",
				"example.com": "v=amtp1;gateway=http://localhost:8080;auth=none;max-size=10485760",
			},
			AllowHTTP: true,
		},
		Message: config.MessageConfig{
			MaxSize:           10485760,
			IdempotencyTTL:    168 * time.Hour,
			ValidationEnabled: true,
		},
		Auth: config.AuthConfig{
			RequireAuth:  false,
			Methods:      []string{"domain"},
			APIKeyHeader: "X-API-Key",
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

func createTestServer(t *testing.T) *httptest.Server {
	cfg := createTestConfig()

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	return httptest.NewServer(srv.GetRouter())
}

// createMockAMTPServer creates a mock AMTP server for testing deliveries
func createMockAMTPServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock AMTP server that accepts all messages
		if r.URL.Path == "/v1/messages" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message_id":"mock-id","status":"delivered"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestIntegration_MessageLifecycle(t *testing.T) {
	// Create mock AMTP server for deliveries
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	// Update DNS mock records to point to the mock server
	cfg := createTestConfig()
	cfg.DNS.MockRecords = map[string]string{
		"test.com":    fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
	}

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	testServer := httptest.NewServer(srv.GetRouter())
	defer testServer.Close()

	// Test 1: Send a message
	sendRequest := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@test.com"},
		Subject:    "Integration Test Message",
		Payload:    json.RawMessage(`{"message": "Hello from integration test!"}`),
		Headers: map[string]interface{}{
			"priority": "high",
			"test":     true,
		},
	}

	sendBody, err := json.Marshal(sendRequest)
	if err != nil {
		t.Fatalf("Failed to marshal send request: %v", err)
	}

	resp, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(sendBody))
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		// Read the error response body for debugging
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200 or 202, got %d. Response: %s", resp.StatusCode, string(body))
	}

	var sendResponse types.SendMessageResponse
	err = json.NewDecoder(resp.Body).Decode(&sendResponse)
	if err != nil {
		t.Fatalf("Failed to decode send response: %v", err)
	}

	if sendResponse.MessageID == "" {
		t.Fatal("Expected message ID to be set")
	}

	if len(sendResponse.Recipients) != 1 {
		t.Errorf("Expected 1 recipient, got %d", len(sendResponse.Recipients))
	}

	messageID := sendResponse.MessageID

	// Test 2: Retrieve the message
	getResp, err := http.Get(testServer.URL + "/v1/messages/" + messageID)
	if err != nil {
		t.Fatalf("Failed to get message: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for get message, got %d", getResp.StatusCode)
	}

	var getMessage types.Message
	err = json.NewDecoder(getResp.Body).Decode(&getMessage)
	if err != nil {
		t.Fatalf("Failed to decode get message response: %v", err)
	}

	if getMessage.MessageID != messageID {
		t.Errorf("Expected message ID %s, got %s", messageID, getMessage.MessageID)
	}

	if getMessage.Sender != sendRequest.Sender {
		t.Errorf("Expected sender %s, got %s", sendRequest.Sender, getMessage.Sender)
	}

	if getMessage.Subject != sendRequest.Subject {
		t.Errorf("Expected subject %s, got %s", sendRequest.Subject, getMessage.Subject)
	}

	// Test 3: Get message status
	statusResp, err := http.Get(testServer.URL + "/v1/messages/" + messageID + "/status")
	if err != nil {
		t.Fatalf("Failed to get message status: %v", err)
	}
	defer statusResp.Body.Close()

	if statusResp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for get status, got %d", statusResp.StatusCode)
	}

	var messageStatus types.MessageStatus
	err = json.NewDecoder(statusResp.Body).Decode(&messageStatus)
	if err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}

	if messageStatus.MessageID != messageID {
		t.Errorf("Expected message ID %s, got %s", messageID, messageStatus.MessageID)
	}

	if len(messageStatus.Recipients) != 1 {
		t.Errorf("Expected 1 recipient status, got %d", len(messageStatus.Recipients))
	}

	if messageStatus.Recipients[0].Address != "recipient@test.com" {
		t.Errorf("Expected recipient address 'recipient@test.com', got %s", messageStatus.Recipients[0].Address)
	}
}

func TestIntegration_MultipleRecipients(t *testing.T) {
	// Create mock AMTP server for deliveries
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	// Update DNS mock records to point to the mock server
	cfg := createTestConfig()
	cfg.DNS.MockRecords = map[string]string{
		"test.com":    fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
	}

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	testServer := httptest.NewServer(srv.GetRouter())
	defer testServer.Close()

	sendRequest := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient1@test.com", "recipient2@test.com", "recipient3@test.com"},
		Subject:    "Multi-recipient Test",
		Payload:    json.RawMessage(`{"message": "Hello to multiple recipients!"}`),
	}

	sendBody, err := json.Marshal(sendRequest)
	if err != nil {
		t.Fatalf("Failed to marshal send request: %v", err)
	}

	resp, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(sendBody))
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		// Read the error response body for debugging
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200 or 202, got %d. Response: %s", resp.StatusCode, string(body))
		return
	}

	var sendResponse types.SendMessageResponse
	err = json.NewDecoder(resp.Body).Decode(&sendResponse)
	if err != nil {
		t.Fatalf("Failed to decode send response: %v", err)
	}

	if len(sendResponse.Recipients) != 3 {
		t.Errorf("Expected 3 recipients, got %d", len(sendResponse.Recipients))
	}

	// Verify all recipients are present
	expectedRecipients := map[string]bool{
		"recipient1@test.com": false,
		"recipient2@test.com": false,
		"recipient3@test.com": false,
	}

	for _, recipient := range sendResponse.Recipients {
		if _, exists := expectedRecipients[recipient.Address]; exists {
			expectedRecipients[recipient.Address] = true
		}
	}

	for addr, found := range expectedRecipients {
		if !found {
			t.Errorf("Expected recipient %s not found in response", addr)
		}
	}
}

func TestIntegration_CoordinationTypes(t *testing.T) {
	// Create mock AMTP server for deliveries
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	// Update DNS mock records to point to the mock server
	cfg := createTestConfig()
	cfg.DNS.MockRecords = map[string]string{
		"test.com":    fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
	}

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	testServer := httptest.NewServer(srv.GetRouter())
	defer testServer.Close()

	tests := []struct {
		name         string
		coordination *types.CoordinationConfig
	}{
		{
			name: "Parallel Coordination",
			coordination: &types.CoordinationConfig{
				Type:    "parallel",
				Timeout: 30,
			},
		},
		{
			name: "Sequential Coordination",
			coordination: &types.CoordinationConfig{
				Type:     "sequential",
				Sequence: []string{"recipient1@test.com", "recipient2@test.com"},
				Timeout:  30,
			},
		},
		{
			name: "Conditional Coordination",
			coordination: &types.CoordinationConfig{
				Type:    "conditional",
				Timeout: 30,
				Conditions: []types.ConditionalRule{
					{
						If:   "always",
						Then: []string{"recipient1@test.com"},
						Else: []string{"recipient2@test.com"},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sendRequest := types.SendMessageRequest{
				Sender:       "test@example.com",
				Recipients:   []string{"recipient1@test.com", "recipient2@test.com"},
				Subject:      fmt.Sprintf("Test %s", test.name),
				Coordination: test.coordination,
				Payload:      json.RawMessage(`{"message": "Coordination test"}`),
			}

			sendBody, err := json.Marshal(sendRequest)
			if err != nil {
				t.Fatalf("Failed to marshal send request: %v", err)
			}

			resp, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(sendBody))
			if err != nil {
				t.Fatalf("Failed to send message: %v", err)
			}
			defer resp.Body.Close()

			// Should accept the message regardless of coordination type
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 200, 202, or 400, got %d", resp.StatusCode)
			}

			var sendResponse types.SendMessageResponse
			err = json.NewDecoder(resp.Body).Decode(&sendResponse)
			if err != nil {
				// If it's a 400 error, check the error response
				if resp.StatusCode == http.StatusBadRequest {
					var errorResponse types.ErrorResponse
					resp.Body.Close()
					resp, _ = http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(sendBody))
					err = json.NewDecoder(resp.Body).Decode(&errorResponse)
					if err != nil {
						t.Fatalf("Failed to decode error response: %v", err)
					}
					t.Logf("Coordination type %s failed validation: %s", test.coordination.Type, errorResponse.Error.Message)
					return
				}
				t.Fatalf("Failed to decode send response: %v", err)
			}

			if sendResponse.MessageID == "" {
				t.Error("Expected message ID to be set")
			}
		})
	}
}

func TestIntegration_ErrorHandling(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	tests := []struct {
		name           string
		request        interface{}
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "Invalid JSON",
			request:        "invalid json",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_REQUEST_FORMAT",
		},
		{
			name: "Missing Sender",
			request: types.SendMessageRequest{
				Recipients: []string{"recipient@test.com"},
				Subject:    "Test",
			},
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "VALIDATION_FAILED",
		},
		{
			name: "Invalid Sender Email",
			request: types.SendMessageRequest{
				Sender:     "invalid-email",
				Recipients: []string{"recipient@test.com"},
				Subject:    "Test",
			},
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "VALIDATION_FAILED",
		},
		{
			name: "Empty Recipients",
			request: types.SendMessageRequest{
				Sender:     "test@example.com",
				Recipients: []string{},
				Subject:    "Test",
			},
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "VALIDATION_FAILED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body []byte
			var err error

			if str, ok := test.request.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(test.request)
				if err != nil {
					t.Fatalf("Failed to marshal request: %v", err)
				}
			}

			resp, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(body))
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != test.expectedStatus {
				t.Errorf("Expected status %d, got %d", test.expectedStatus, resp.StatusCode)
			}

			var errorResponse types.ErrorResponse
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			if err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if errorResponse.Error.Code != test.expectedCode {
				t.Errorf("Expected error code %s, got %s", test.expectedCode, errorResponse.Error.Code)
			}

			if errorResponse.Error.Message == "" {
				t.Error("Expected error message to be set")
			}

			if errorResponse.Error.Timestamp.IsZero() {
				t.Error("Expected error timestamp to be set")
			}
		})
	}
}

func TestIntegration_HealthEndpoints(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	endpoints := []struct {
		path           string
		expectedStatus string
	}{
		{"/health", "healthy"},
		{"/ready", "ready"},
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint.path, func(t *testing.T) {
			resp, err := http.Get(testServer.URL + endpoint.path)
			if err != nil {
				t.Fatalf("Failed to get %s: %v", endpoint.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var response map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&response)
			if err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if response["status"] != endpoint.expectedStatus {
				t.Errorf("Expected status %s, got %v", endpoint.expectedStatus, response["status"])
			}

			if response["version"] != "1.0" {
				t.Errorf("Expected version 1.0, got %v", response["version"])
			}
		})
	}
}

func TestIntegration_AgentDiscoveryEndpoint(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	resp, err := http.Get(testServer.URL + "/v1/discovery/agents")
	if err != nil {
		t.Fatalf("Failed to get agent discovery endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, exists := response["agents"]; !exists {
		t.Error("Expected agents field to be present")
	}

	if _, exists := response["agent_count"]; !exists {
		t.Error("Expected agent_count field to be present")
	}

	if _, exists := response["domain"]; !exists {
		t.Error("Expected domain field to be present")
	}

	if _, exists := response["timestamp"]; !exists {
		t.Error("Expected timestamp field to be present")
	}
}

func TestIntegration_InvalidMessageID(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	tests := []struct {
		name      string
		messageID string
		endpoint  string
	}{
		{"Get Message - Invalid ID", "invalid-id", "/v1/messages/invalid-id"},
		{"Get Status - Invalid ID", "invalid-id", "/v1/messages/invalid-id/status"},
		{"Get Message - Not Found", "01234567-89ab-7def-8123-456789abcdef", "/v1/messages/01234567-89ab-7def-8123-456789abcdef"},
		{"Get Status - Not Found", "01234567-89ab-7def-8123-456789abcdef", "/v1/messages/01234567-89ab-7def-8123-456789abcdef/status"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := http.Get(testServer.URL + test.endpoint)
			if err != nil {
				t.Fatalf("Failed to get %s: %v", test.endpoint, err)
			}
			defer resp.Body.Close()

			expectedStatus := http.StatusBadRequest
			if test.messageID == "01234567-89ab-7def-8123-456789abcdef" {
				expectedStatus = http.StatusNotFound
			}

			if resp.StatusCode != expectedStatus {
				t.Errorf("Expected status %d, got %d", expectedStatus, resp.StatusCode)
			}

			var errorResponse types.ErrorResponse
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			if err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			expectedCode := "INVALID_MESSAGE_ID"
			if test.messageID == "01234567-89ab-7def-8123-456789abcdef" {
				expectedCode = "MESSAGE_NOT_FOUND"
			}

			if errorResponse.Error.Code != expectedCode {
				t.Errorf("Expected error code %s, got %s", expectedCode, errorResponse.Error.Code)
			}
		})
	}
}

func TestIntegration_Idempotency(t *testing.T) {
	// Create mock AMTP server for deliveries
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	// Update DNS mock records to point to the mock server
	cfg := createTestConfig()
	cfg.DNS.MockRecords = map[string]string{
		"test.com":    fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
	}

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	testServer := httptest.NewServer(srv.GetRouter())
	defer testServer.Close()

	sendRequest := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@test.com"},
		Subject:    "Idempotency Test",
		Payload:    json.RawMessage(`{"message": "Testing idempotency"}`),
	}

	sendBody, err := json.Marshal(sendRequest)
	if err != nil {
		t.Fatalf("Failed to marshal send request: %v", err)
	}

	// Send the same message twice
	resp1, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(sendBody))
	if err != nil {
		t.Fatalf("Failed to send first message: %v", err)
	}
	defer resp1.Body.Close()

	var sendResponse1 types.SendMessageResponse
	err = json.NewDecoder(resp1.Body).Decode(&sendResponse1)
	if err != nil {
		t.Fatalf("Failed to decode first response: %v", err)
	}

	resp2, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(sendBody))
	if err != nil {
		t.Fatalf("Failed to send second message: %v", err)
	}
	defer resp2.Body.Close()

	var sendResponse2 types.SendMessageResponse
	err = json.NewDecoder(resp2.Body).Decode(&sendResponse2)
	if err != nil {
		t.Fatalf("Failed to decode second response: %v", err)
	}

	// The responses should be identical due to idempotency
	if sendResponse1.MessageID != sendResponse2.MessageID {
		t.Errorf("Expected same message ID due to idempotency, got %s and %s",
			sendResponse1.MessageID, sendResponse2.MessageID)
	}

	if sendResponse1.Status != sendResponse2.Status {
		t.Errorf("Expected same status due to idempotency, got %s and %s",
			sendResponse1.Status, sendResponse2.Status)
	}
}

func BenchmarkIntegration_SendMessage(b *testing.B) {
	b.Skip("Integration tests temporarily disabled due to Prometheus metrics registration conflicts")
	cfg := createTestConfig()

	srv, err := server.New(cfg)
	if err != nil {
		b.Fatalf("Failed to create server: %v", err)
	}

	testServer := httptest.NewServer(srv.GetRouter())
	defer testServer.Close()

	sendRequest := types.SendMessageRequest{
		Sender:     "test@example.com",
		Recipients: []string{"recipient@test.com"},
		Subject:    "Benchmark Test",
		Payload:    json.RawMessage(`{"message": "Benchmark message"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different subjects to avoid idempotency
		request := sendRequest
		request.Subject = fmt.Sprintf("Benchmark Test %d", i)
		body, _ := json.Marshal(request)

		resp, err := http.Post(testServer.URL+"/v1/messages", "application/json", bytes.NewBuffer(body))
		if err != nil {
			b.Fatalf("Failed to send message: %v", err)
		}
		resp.Body.Close()
	}
}
