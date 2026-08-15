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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/config"
	"github.com/amtp-protocol/agentry/internal/server"
	"github.com/amtp-protocol/agentry/internal/types"
)

// Integration tests for the AMTP Gateway
// These tests verify the complete flow from HTTP request to response

// adminKeyValue is the shared admin key used by the integration tests.
const adminKeyValue = "integration-admin-key"

// testConfigTB is the subset of testing.TB used by createTestConfig, so the
// helper works for both tests and benchmarks.
type testConfigTB interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...interface{})
}

// createTestConfig returns a test configuration with a dynamically generated
// admin key file so the tests work without committing a key to the repo
// (agentry's .gitignore excludes *.key).
func createTestConfig(t testConfigTB) *config.Config {
	t.Helper()

	keyFile := filepath.Join(t.TempDir(), "admin.key")
	if err := os.WriteFile(keyFile, []byte(adminKeyValue), 0o600); err != nil {
		t.Fatalf("write admin key file: %v", err)
	}

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
			RequireAuth:       false,
			Methods:           []string{"domain"},
			APIKeyHeader:      "X-API-Key",
			AdminAPIKeyHeader: "X-Admin-Key",
			// Admin key file used by the message lifecycle test to register
			// an agent and authenticate message queries.
			AdminKeyFile: keyFile,
			APIKeySalt:   "integration-test-salt",
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

func createTestServer(t *testing.T) *httptest.Server {
	cfg := createTestConfig(t)

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

// registerLocalAgentWithAddress registers a local agent with the given
// address via the admin API and returns its plaintext API key for
// authenticating message queries.
func registerLocalAgentWithAddress(t *testing.T, baseURL, address string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"address": address, "delivery_mode": "pull"})
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/admin/agents", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("build register request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", adminKeyValue)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("register agent status %d: %s", resp.StatusCode, string(rb))
	}

	var out struct {
		Agent struct {
			APIKey string `json:"api_key"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if out.Agent.APIKey == "" {
		t.Fatal("agent API key is empty")
	}
	return out.Agent.APIKey
}

// registerLocalAgent registers a local agent named "test" via the admin API
// and returns its plaintext API key for authenticating message queries.
func registerLocalAgent(t *testing.T, baseURL string) string {
	t.Helper()
	return registerLocalAgentWithAddress(t, baseURL, "test")
}

// sendTestMessage posts a message via the send endpoint and returns the
// assigned message ID. An explicit timestamp makes the newest-first ordering
// of the message store deterministic for pagination assertions.
func sendTestMessage(t *testing.T, baseURL, sender, recipient, subject, timestamp string) string {
	t.Helper()
	sendRequest := types.SendMessageRequest{
		Sender:     sender,
		Recipients: []string{recipient},
		Subject:    subject,
		Timestamp:  timestamp,
		Payload:    json.RawMessage(`{"hello":"world"}`),
	}
	body, err := json.Marshal(sendRequest)
	if err != nil {
		t.Fatalf("marshal send request: %v", err)
	}
	resp, err := http.Post(baseURL+"/v1/messages", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("send message status %d: %s", resp.StatusCode, string(rb))
	}

	var sendResponse types.SendMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&sendResponse); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if sendResponse.MessageID == "" {
		t.Fatal("send response has empty message ID")
	}
	return sendResponse.MessageID
}

// listMessageItem is the per-message shape returned by GET /v1/messages.
type listMessageItem struct {
	MessageID string `json:"message_id"`
	Timestamp string `json:"timestamp"`
}

// listMessagesResponse is the envelope returned by GET /v1/messages.
type listMessagesResponse struct {
	Messages []listMessageItem `json:"messages"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

// listMessages queries GET /v1/messages as the given agent and returns the
// parsed response.
func listMessages(t *testing.T, baseURL, apiKey string, limit, offset int) listMessagesResponse {
	t.Helper()
	url := fmt.Sprintf("%s/v1/messages?limit=%d&offset=%d", baseURL, limit, offset)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("list messages status %d: %s", resp.StatusCode, string(rb))
	}

	var out listMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return out
}

// TestIntegration_ListMessagesPagination is a functional verification test
// for the merged "all traffic" message query (GET /v1/messages without a
// sender/recipient filter). It seeds a mix of messages the listing agent
// sent and messages it received, then pages through the result and verifies:
//   - each page holds at most `limit` messages,
//   - consecutive pages neither overlap nor drop messages,
//   - the merged list is sorted newest-first, and
//   - total reflects the full filtered set.
func TestIntegration_ListMessagesPagination(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	// Listing agent plus a peer so the agent has both sent and received
	// traffic.
	agentKey := registerLocalAgent(t, testServer.URL)
	registerLocalAgentWithAddress(t, testServer.URL, "peer")

	// Seed messages with strictly decreasing timestamps so the expected
	// newest-first order is deterministic. Even indices are sent by the
	// listing agent, odd indices are received by it (sent by the peer);
	// interleaving both directions exposes any sent-then-received merge
	// ordering bug.
	const total = 8
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	newestFirst := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ts := base.Add(time.Duration(total-1-i) * time.Minute).Format(time.RFC3339)
		sender, recipient := "test@localhost", "peer@localhost"
		if i%2 == 1 {
			sender, recipient = "peer@localhost", "test@localhost"
		}
		msgID := sendTestMessage(t, testServer.URL, sender, recipient,
			fmt.Sprintf("pagination-%d", i), ts)
		newestFirst = append(newestFirst, msgID)
	}

	// Page through the full set with limit=3 and verify stability across
	// offsets.
	const limit = 3
	seen := make(map[string]bool)
	var all []string
	for offset := 0; offset < total; offset += limit {
		resp := listMessages(t, testServer.URL, agentKey, limit, offset)

		if resp.Total != total {
			t.Errorf("offset=%d: expected total %d, got %d", offset, total, resp.Total)
		}
		if resp.Offset != offset {
			t.Errorf("offset=%d: expected echoed offset %d", offset, resp.Offset)
		}
		if resp.Limit != limit {
			t.Errorf("offset=%d: expected echoed limit %d", offset, resp.Limit)
		}
		if len(resp.Messages) > limit {
			t.Errorf("offset=%d: expected at most %d messages, got %d",
				offset, limit, len(resp.Messages))
		}

		// Each page must be ordered newest-first.
		for j := 1; j < len(resp.Messages); j++ {
			prev, err := time.Parse(time.RFC3339, resp.Messages[j-1].Timestamp)
			if err != nil {
				t.Fatalf("parse previous timestamp %q: %v", resp.Messages[j-1].Timestamp, err)
			}
			cur, err := time.Parse(time.RFC3339, resp.Messages[j].Timestamp)
			if err != nil {
				t.Fatalf("parse current timestamp %q: %v", resp.Messages[j].Timestamp, err)
			}
			if !prev.After(cur) {
				t.Errorf("offset=%d: messages not newest-first: %s (%s) before %s (%s)",
					offset, resp.Messages[j-1].MessageID, prev, resp.Messages[j].MessageID, cur)
			}
		}

		for _, m := range resp.Messages {
			if seen[m.MessageID] {
				t.Errorf("offset=%d: duplicate message %s across pages", offset, m.MessageID)
			}
			seen[m.MessageID] = true
			all = append(all, m.MessageID)
		}
	}

	// The union of all pages must cover every seeded message exactly once,
	// in newest-first order.
	if len(all) != total {
		t.Errorf("expected %d messages across pages, got %d", total, len(all))
	}
	for i, id := range newestFirst {
		if i < len(all) && all[i] != id {
			t.Errorf("position %d: expected %s, got %s (merged list must be newest-first)",
				i, id, all[i])
		}
	}

	// An unpaginated listing returns everything, newest-first.
	resp := listMessages(t, testServer.URL, agentKey, 100, 0)
	if resp.Total != total {
		t.Errorf("expected total %d, got %d", total, resp.Total)
	}
	if len(resp.Messages) != total {
		t.Errorf("expected %d messages, got %d", total, len(resp.Messages))
	}
	for i, id := range newestFirst {
		if i < len(resp.Messages) && resp.Messages[i].MessageID != id {
			t.Errorf("unpaginated position %d: expected %s, got %s",
				i, id, resp.Messages[i].MessageID)
		}
	}
}

// TestIntegration_ListMessagesInvalidStatus is a functional verification
// test for status query parameter validation on GET /v1/messages. An
// unvalidated status flows into the storage filter where, on the database
// backend, it is compared against the Postgres delivery_status enum column
// and a bogus value raises a 22P02 enum-cast error (500 MESSAGE_LIST_FAILED);
// the memory backend would silently return an empty list. The handler must
// reject unknown values with 400 INVALID_STATUS instead, so behavior is
// identical across backends, and must keep accepting every known status.
func TestIntegration_ListMessagesInvalidStatus(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	agentKey := registerLocalAgent(t, testServer.URL)

	// An unknown status must be rejected with 400 before it reaches storage.
	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages?status=bogus", nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+agentKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list messages with invalid status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("invalid status: expected status %d, got %d: %s",
			http.StatusBadRequest, resp.StatusCode, string(rb))
	}

	var errorResponse types.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorResponse.Error.Code != "INVALID_STATUS" {
		t.Errorf("expected error code INVALID_STATUS, got %s", errorResponse.Error.Code)
	}

	// Every known delivery status must remain accepted.
	for _, status := range []string{"pending", "queued", "delivering", "delivered", "failed", "retrying"} {
		req, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages?status="+status, nil)
		if err != nil {
			t.Fatalf("build list request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+agentKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list messages with status %s: %v", status, err)
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("valid status %q: expected status %d, got %d: %s",
				status, http.StatusOK, resp.StatusCode, string(rb))
		}
	}
}

// TestIntegration_AdminCanInspectUnregisteredSenderMessage is a functional
// verification test for the admin key fallback on message query endpoints.
// POST /v1/messages is public and accepts any sender, so a message submitted
// by an unregistered sender (or routed in from a foreign domain) has no
// registered agent key that matches sender or recipient. Without the admin
// fallback such messages are permanently unreadable: unauthenticated polling
// returns 401 and any registered agent key returns 404. The admin key must be
// able to fetch the message, its status, and list it, so operators can
// inspect traffic that no agent key can reach.
func TestIntegration_AdminCanInspectUnregisteredSenderMessage(t *testing.T) {
	// Route foreign-domain deliveries to a mock AMTP gateway so the send
	// succeeds and the message is persisted with a status.
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	cfg := createTestConfig(t)
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

	// A registered agent whose key must NOT grant access to the foreign
	// message below (neither sender nor recipient matches it).
	agentKey := registerLocalAgent(t, testServer.URL)

	// Submit a message from an unregistered sender to an unregistered
	// recipient in a foreign domain.
	msgID := sendTestMessage(t, testServer.URL,
		"unregistered@example.com", "remote-peer@example.com",
		"admin-inspect", time.Now().UTC().Format(time.RFC3339))

	// 1. Unauthenticated polling is rejected.
	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages/"+msgID+"/status", nil)
	if err != nil {
		t.Fatalf("build status request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: expected status %d, got %d: %s", http.StatusUnauthorized, resp.StatusCode, string(rb))
	}

	// 2. A registered agent key does not grant access (neither sender nor
	// recipient is the registered agent).
	req, err = http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages/"+msgID+"/status", nil)
	if err != nil {
		t.Fatalf("build status request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+agentKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	rb, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("agent key: expected status %d, got %d: %s", http.StatusNotFound, resp.StatusCode, string(rb))
	}

	// 3. The admin key can fetch the message.
	req, err = http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages/"+msgID, nil)
	if err != nil {
		t.Fatalf("build get request: %v", err)
	}
	req.Header.Set("X-Admin-Key", adminKeyValue)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin get: expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	var got types.Message
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.MessageID != msgID {
		t.Errorf("admin get: expected message %s, got %s", msgID, got.MessageID)
	}
	if got.Sender != "unregistered@example.com" {
		t.Errorf("admin get: expected sender unregistered@example.com, got %s", got.Sender)
	}

	// 4. The admin key can fetch the status.
	req, err = http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages/"+msgID+"/status", nil)
	if err != nil {
		t.Fatalf("build status request: %v", err)
	}
	req.Header.Set("X-Admin-Key", adminKeyValue)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin status: expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	var status types.MessageStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.MessageID != msgID {
		t.Errorf("admin status: expected message %s, got %s", msgID, status.MessageID)
	}

	// 5. The admin key can list messages, including one whose participants
	// are both foreign, and filter by the foreign sender address.
	req, err = http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages?sender=unregistered@example.com", nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	req.Header.Set("X-Admin-Key", adminKeyValue)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin list: expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	var listing listMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, m := range listing.Messages {
		if m.MessageID == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("admin list: expected message %s in filtered list of %d, got %v",
			msgID, listing.Total, listing.Messages)
	}
}

func TestIntegration_MessageLifecycle(t *testing.T) {
	// Create mock AMTP server for deliveries
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	// Update DNS mock records to point to the mock server
	cfg := createTestConfig(t)
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

	// Register a local agent so message queries can authenticate.
	agentKey := registerLocalAgent(t, testServer.URL)

	// Test 1: Send a message
	sendRequest := types.SendMessageRequest{
		Sender:     "test@localhost",
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
	getReq, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages/"+messageID, nil)
	if err != nil {
		t.Fatalf("build get request: %v", err)
	}
	getReq.Header.Set("Authorization", "Bearer "+agentKey)
	getResp, err := http.DefaultClient.Do(getReq)
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
	statusReq, err := http.NewRequest(http.MethodGet, testServer.URL+"/v1/messages/"+messageID+"/status", nil)
	if err != nil {
		t.Fatalf("build status request: %v", err)
	}
	statusReq.Header.Set("Authorization", "Bearer "+agentKey)
	statusResp, err := http.DefaultClient.Do(statusReq)
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
	cfg := createTestConfig(t)
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
	cfg := createTestConfig(t)
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

// patchAgent sends an admin-authenticated PATCH to /v1/admin/agents/:name and
// returns the HTTP status and response body.
func patchAgent(t *testing.T, baseURL, name string, body interface{}) (int, []byte) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, baseURL+"/v1/admin/agents/"+name, bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("build patch request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", adminKeyValue)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch agent: %v", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, rb
}

// TestIntegration_UpdateAgentPushInvariant is a functional verification test
// for the push-mode delivery invariant (push mode requires a non-empty push
// target). The invariant must hold for the combined state, not just the
// pre-update record: two concurrent PATCHes — one clearing push_target, the
// other switching to push mode — must not jointly store the invalid
// combination. Each request validates against the state committed by the
// other, so exactly one succeeds and the final state stays valid.
func TestIntegration_UpdateAgentPushInvariant(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	baseURL := testServer.URL

	// 1. Push mode without a target is rejected.
	registerLocalAgentWithAddress(t, baseURL, "push-rigid")
	status, body := patchAgent(t, baseURL, "push-rigid", map[string]string{"delivery_mode": "push"})
	if status != http.StatusBadRequest {
		t.Fatalf("push without target: expected status %d, got %d: %s", http.StatusBadRequest, status, string(body))
	}
	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(body, &errorResponse); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorResponse.Error.Code != "AGENT_UPDATE_FAILED" {
		t.Errorf("push without target: expected AGENT_UPDATE_FAILED, got %s", errorResponse.Error.Code)
	}

	// 2. Push mode with a target succeeds.
	status, body = patchAgent(t, baseURL, "push-rigid", map[string]string{
		"delivery_mode": "push",
		"push_target":   "https://hooks.example.com/push-rigid",
	})
	if status != http.StatusOK {
		t.Fatalf("push with target: expected status %d, got %d: %s", http.StatusOK, status, string(body))
	}

	// 3. Clearing the target while in push mode is rejected, and the stored
	// record stays unchanged.
	status, body = patchAgent(t, baseURL, "push-rigid", map[string]string{"push_target": ""})
	if status != http.StatusBadRequest {
		t.Fatalf("clear target in push mode: expected status %d, got %d: %s", http.StatusBadRequest, status, string(body))
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/admin/agents", nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	req.Header.Set("X-Admin-Key", adminKeyValue)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Agents map[string]struct {
			DeliveryMode string `json:"delivery_mode"`
			PushTarget   string `json:"push_target"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	stored, ok := listing.Agents["push-rigid@localhost"]
	if !ok {
		t.Fatalf("expected agent push-rigid@localhost in listing, got %v", listing.Agents)
	}
	if stored.DeliveryMode != "push" || stored.PushTarget != "https://hooks.example.com/push-rigid" {
		t.Errorf("agent mutated by rejected update: got mode=%q target=%q", stored.DeliveryMode, stored.PushTarget)
	}

	// 4. Two concurrent PATCHes cannot jointly store push mode with an empty
	// target. Starting state: pull mode with a non-empty target. The pair is
	// repeated on several agents so a regression that allows the invalid
	// combination is reliably caught regardless of goroutine scheduling.
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("race-agent-%d", i)
		registerLocalAgentWithAddress(t, baseURL, name)
		status, body = patchAgent(t, baseURL, name, map[string]string{
			"delivery_mode": "push",
			"push_target":   "https://hooks.example.com/" + name,
		})
		if status != http.StatusOK {
			t.Fatalf("seed %s: expected status %d, got %d: %s", name, http.StatusOK, status, string(body))
		}
		status, body = patchAgent(t, baseURL, name, map[string]string{"delivery_mode": "pull"})
		if status != http.StatusOK {
			t.Fatalf("switch %s to pull: expected status %d, got %d: %s", name, http.StatusOK, status, string(body))
		}

		// Fire both PATCHes at once: one clears the target, the other switches
		// back to push. Each validates against the state committed by the
		// other, so exactly one must fail and the final state must stay valid.
		start := make(chan struct{})
		results := make(chan int, 2)
		var wg sync.WaitGroup
		for _, payload := range []map[string]string{{"push_target": ""}, {"delivery_mode": "push"}} {
			wg.Add(1)
			go func(p map[string]string) {
				defer wg.Done()
				<-start
				status, _ := patchAgent(t, baseURL, name, p)
				results <- status
			}(payload)
		}
		close(start)
		wg.Wait()
		close(results)

		okCount, badCount := 0, 0
		for s := range results {
			switch s {
			case http.StatusOK:
				okCount++
			case http.StatusBadRequest:
				badCount++
			}
		}
		if okCount != 1 || badCount != 1 {
			t.Fatalf("%s: concurrent updates: expected exactly one 200 and one 400, got %d 200 and %d 400",
				name, okCount, badCount)
		}

		req, err = http.NewRequest(http.MethodGet, baseURL+"/v1/admin/agents", nil)
		if err != nil {
			t.Fatalf("build list request: %v", err)
		}
		req.Header.Set("X-Admin-Key", adminKeyValue)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list agents: %v", err)
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		listing = struct {
			Agents map[string]struct {
				DeliveryMode string `json:"delivery_mode"`
				PushTarget   string `json:"push_target"`
			} `json:"agents"`
		}{}
		if err := json.Unmarshal(rb, &listing); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		final, ok := listing.Agents[name+"@localhost"]
		if !ok {
			t.Fatalf("expected agent %s@localhost in listing, got %v", name, listing.Agents)
		}
		if final.DeliveryMode == "push" && final.PushTarget == "" {
			t.Errorf("%s: concurrent updates stored invalid state: push mode with empty push target", name)
		}
	}
}

// listMessagesQuery queries GET /v1/messages with an explicit query string
// (without the leading "?") as the given agent and returns the parsed
// response. The caller supplies limit/offset in the query if needed.
func listMessagesQuery(t *testing.T, baseURL, apiKey, query string) listMessagesResponse {
	t.Helper()
	url := baseURL + "/v1/messages"
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("list messages status %d: %s", resp.StatusCode, string(rb))
	}

	var out listMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return out
}

// TestIntegration_ListMessagesCounterpartFilters is a functional verification
// test for counterpart conversation filters on GET /v1/messages. An agent
// must be able to filter its own traffic by the other side of a conversation:
// "?recipient=bob@remote.com" lists messages the agent sent to bob and
// "?sender=bob@remote.com" lists messages bob sent to the agent, instead of
// 403 ACCESS_DENIED for any filter value other than the agent itself. The
// result set must contain exactly the messages of that conversation and
// nothing the agent is not a participant in.
func TestIntegration_ListMessagesCounterpartFilters(t *testing.T) {
	// Route foreign-domain deliveries to a mock AMTP gateway so sends to
	// foreign recipients succeed and the messages are persisted.
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	cfg := createTestConfig(t)
	cfg.DNS.MockRecords = map[string]string{
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;auth=none;max-size=10485760", mockAMTPServer.URL),
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	testServer := httptest.NewServer(srv.GetRouter())
	defer testServer.Close()

	// The listing agent plus a local counterpart; foreign counterparts are
	// resolved via the mock gateway.
	agentKey := registerLocalAgent(t, testServer.URL)
	registerLocalAgentWithAddress(t, testServer.URL, "peer")

	// Seed one message per conversation direction. Keep a map from message ID
	// to subject so the returned listing can be attributed.
	now := time.Now().UTC()
	seed := []struct {
		sender, recipient, subject string
	}{
		{"test@localhost", "peer@localhost", "alice-to-peer"},
		{"peer@localhost", "test@localhost", "peer-to-alice"},
		{"test@localhost", "bob@example.com", "alice-to-bob"},
		{"bob@example.com", "test@localhost", "bob-to-alice"},
	}
	subjectByID := make(map[string]string, len(seed))
	for _, m := range seed {
		msgID := sendTestMessage(t, testServer.URL, m.sender, m.recipient, m.subject,
			now.Add(time.Duration(-len(subjectByID))*time.Minute).Format(time.RFC3339))
		subjectByID[msgID] = m.subject
	}

	// subjectSetOf returns the subjects of the listed messages, failing the
	// test if a listed message ID is unknown.
	subjectSetOf := func(resp listMessagesResponse) map[string]bool {
		set := make(map[string]bool, len(resp.Messages))
		for _, m := range resp.Messages {
			subject, ok := subjectByID[m.MessageID]
			if !ok {
				t.Errorf("listed unknown message %s", m.MessageID)
				continue
			}
			set[subject] = true
		}
		return set
	}
	want := func(subjects ...string) map[string]bool {
		set := make(map[string]bool, len(subjects))
		for _, s := range subjects {
			set[s] = true
		}
		return set
	}

	// Single-sided counterpart filters pin the agent as the other side.
	tests := []struct {
		name  string
		query string
		want  map[string]bool
	}{
		{"recipient local counterpart", "recipient=peer@localhost", want("alice-to-peer")},
		{"sender local counterpart", "sender=peer@localhost", want("peer-to-alice")},
		{"recipient foreign counterpart", "recipient=bob@example.com", want("alice-to-bob")},
		{"sender foreign counterpart", "sender=bob@example.com", want("bob-to-alice")},
		// Explicit conversation: the agent pinned as one side.
		{"explicit sent to counterpart", "sender=test@localhost&recipient=bob@example.com", want("alice-to-bob")},
		{"explicit received from counterpart", "sender=bob@example.com&recipient=test@localhost", want("bob-to-alice")},
		// Bare-name counterpart normalizes to the full local address.
		{"bare recipient counterpart", "recipient=peer", want("alice-to-peer")},
		// Sender-only with the agent itself still lists everything sent.
		{"sender self", "sender=test@localhost", want("alice-to-peer", "alice-to-bob")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := listMessagesQuery(t, testServer.URL, agentKey, tt.query)
			got := subjectSetOf(resp)
			if resp.Total != len(tt.want) {
				t.Errorf("expected total %d, got %d (subjects %v)", len(tt.want), resp.Total, got)
			}
			for s := range tt.want {
				if !got[s] {
					t.Errorf("expected %q in result, got %v", s, got)
				}
			}
			for s := range got {
				if !tt.want[s] {
					t.Errorf("unexpected %q in result", s)
				}
			}
		})
	}

	// A conversation where neither side is the authenticated agent stays
	// rejected: the caller cannot inspect traffic it is not a participant in.
	req, err := http.NewRequest(http.MethodGet,
		testServer.URL+"/v1/messages?sender=bob@example.com&recipient=carol@example.com", nil)
	if err != nil {
		t.Fatalf("build list request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+agentKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		rb, _ := io.ReadAll(resp.Body)
		t.Errorf("neither-side conversation: expected status %d, got %d: %s",
			http.StatusForbidden, resp.StatusCode, string(rb))
	}
}

func TestIntegration_InvalidMessageID(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	agentKey := registerLocalAgent(t, testServer.URL)

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
			req, err := http.NewRequest(http.MethodGet, testServer.URL+test.endpoint, nil)
			if err != nil {
				t.Fatalf("Failed to build request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+agentKey)
			resp, err := http.DefaultClient.Do(req)
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

// rotateAgentKey posts to POST /v1/admin/agents/:name/rotate-key with the
// admin key and returns the HTTP status and response body.
func rotateAgentKey(t *testing.T, baseURL, name string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/admin/agents/"+name+"/rotate-key", nil)
	if err != nil {
		t.Fatalf("build rotate request: %v", err)
	}
	req.Header.Set("X-Admin-Key", adminKeyValue)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, rb
}

// TestIntegration_RotateAgentKeyValidation is a functional verification test
// for POST /v1/admin/agents/:address/rotate-key. The endpoint must validate
// the address like its sibling PATCH/DELETE endpoints:
//   - a bare agent name is normalized to the full local address before
//     reaching the registry, and the response echoes it;
//   - a foreign-domain address is rejected with an explicit domain-mismatch
//     error instead of being passed to the registry unchecked (which would
//     surface as a misleading "agent not found");
//   - a full local address is accepted;
//   - a nonexistent agent still fails.
func TestIntegration_RotateAgentKeyValidation(t *testing.T) {
	testServer := createTestServer(t)
	defer testServer.Close()

	baseURL := testServer.URL
	registerLocalAgent(t, baseURL)

	// 1. A bare name is normalized to the full address.
	status, body := rotateAgentKey(t, baseURL, "test")
	if status != http.StatusOK {
		t.Fatalf("rotate bare name: expected status %d, got %d: %s", http.StatusOK, status, string(body))
	}
	var rotated struct {
		Address string `json:"address"`
		APIKey  string `json:"api_key"`
	}
	if err := json.Unmarshal(body, &rotated); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if rotated.Address != "test@localhost" {
		t.Errorf("expected normalized address test@localhost, got %s", rotated.Address)
	}
	if rotated.APIKey == "" {
		t.Error("expected a new api_key in the response")
	}

	// 2. A foreign-domain address is rejected with an explicit
	// domain-mismatch error, not "agent not found".
	status, body = rotateAgentKey(t, baseURL, "foo@evil.com")
	if status != http.StatusBadRequest {
		t.Fatalf("rotate foreign domain: expected status %d, got %d: %s", http.StatusBadRequest, status, string(body))
	}
	var errorResponse types.ErrorResponse
	if err := json.Unmarshal(body, &errorResponse); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorResponse.Error.Code != "AGENT_KEY_ROTATION_FAILED" {
		t.Errorf("expected AGENT_KEY_ROTATION_FAILED, got %s", errorResponse.Error.Code)
	}
	detail, _ := errorResponse.Error.Details["error"].(string)
	if !strings.Contains(detail, "does not match local domain") {
		t.Errorf("expected domain-mismatch error, got %q", detail)
	}

	// 3. A full local address is accepted.
	status, body = rotateAgentKey(t, baseURL, "test@localhost")
	if status != http.StatusOK {
		t.Fatalf("rotate full local address: expected status %d, got %d: %s", http.StatusOK, status, string(body))
	}

	// 4. A nonexistent agent still fails.
	status, body = rotateAgentKey(t, baseURL, "ghost")
	if status != http.StatusBadRequest {
		t.Fatalf("rotate nonexistent: expected status %d, got %d: %s", http.StatusBadRequest, status, string(body))
	}
	var ghostErr types.ErrorResponse
	if err := json.Unmarshal(body, &ghostErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if ghostErr.Error.Code != "AGENT_KEY_ROTATION_FAILED" {
		t.Errorf("expected AGENT_KEY_ROTATION_FAILED, got %s", ghostErr.Error.Code)
	}
}

func TestIntegration_Idempotency(t *testing.T) {
	// Create mock AMTP server for deliveries
	mockAMTPServer := createMockAMTPServer(t)
	defer mockAMTPServer.Close()

	// Update DNS mock records to point to the mock server
	cfg := createTestConfig(t)
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
	b.Skip("Integration tests temporarily disabled")
	cfg := createTestConfig(b)

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
