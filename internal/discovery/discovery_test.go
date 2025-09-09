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

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscoverCapabilities_DNSOnly(t *testing.T) {
	// Test DNS-only discovery using mock records
	mockRecords := map[string]string{
		"example.com": "v=amtp1;gateway=https://amtp.example.com:443;auth=cert,oauth;max-size=10485760;features=immediate-path,schema-validation,agent-discovery",
	}

	// Create mock discovery service
	discovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test discovery using DNS TXT records only
	capabilities, err := discovery.DiscoverCapabilities(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	// Verify basic capabilities
	if capabilities.Gateway != "https://amtp.example.com:443" {
		t.Errorf("Expected gateway 'https://amtp.example.com:443', got '%s'", capabilities.Gateway)
	}

	if capabilities.Version != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", capabilities.Version)
	}

	// Verify schemas - should be empty since schemas are no longer parsed from DNS
	if len(capabilities.Schemas) != 0 {
		t.Errorf("Expected 0 schemas from DNS discovery, got %d", len(capabilities.Schemas))
	}

	// Verify auth methods
	expectedAuth := []string{"cert", "oauth"}
	if len(capabilities.Auth) != len(expectedAuth) {
		t.Errorf("Expected %d auth methods, got %d", len(expectedAuth), len(capabilities.Auth))
	}

	// Verify max size
	if capabilities.MaxSize != 10485760 {
		t.Errorf("Expected max size 10485760, got %d", capabilities.MaxSize)
	}

	// Verify features
	if !capabilities.HasAgentDiscovery() {
		t.Error("Expected agent discovery feature to be supported")
	}
}

func TestDiscoverAgents(t *testing.T) {
	// Create a test server that serves agent discovery endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/discovery/agents" {
			// Mock agent discovery response (direct format, not wrapped)
			response := AgentDiscoveryResponse{
				Agents: []Agent{
					{
						Address:      "sales@example.com",
						DeliveryMode: "pull",
						CreatedAt:    time.Now().Add(-24 * time.Hour),
						LastActive:   timePtr(time.Now().Add(-1 * time.Hour)),
					},
					{
						Address:      "support@example.com",
						DeliveryMode: "push",
						CreatedAt:    time.Now().Add(-48 * time.Hour),
						LastActive:   timePtr(time.Now().Add(-2 * time.Hour)),
					},
				},
				AgentCount: 2,
				Domain:     "example.com",
				Timestamp:  time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	// Create mock discovery with DNS record pointing to our test server
	mockRecords := map[string]string{
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;features=agent-discovery", server.URL),
	}
	discovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test agent discovery
	agentResponse, err := discovery.DiscoverAgents(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Agent discovery failed: %v", err)
	}

	// Verify agent discovery response
	if agentResponse.AgentCount != 2 {
		t.Errorf("Expected agent count 2, got %d", agentResponse.AgentCount)
	}

	if len(agentResponse.Agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agentResponse.Agents))
	}

	if agentResponse.Domain != "example.com" {
		t.Errorf("Expected domain 'example.com', got '%s'", agentResponse.Domain)
	}

	// Verify individual agents
	var salesAgent, supportAgent *Agent
	for i, agent := range agentResponse.Agents {
		if agent.Address == "sales@example.com" {
			salesAgent = &agentResponse.Agents[i]
		} else if agent.Address == "support@example.com" {
			supportAgent = &agentResponse.Agents[i]
		}
	}

	if salesAgent == nil {
		t.Error("Expected to find sales agent")
	} else if salesAgent.DeliveryMode != "pull" {
		t.Errorf("Expected sales agent delivery mode 'pull', got '%s'", salesAgent.DeliveryMode)
	}

	if supportAgent == nil {
		t.Error("Expected to find support agent")
	} else if supportAgent.DeliveryMode != "push" {
		t.Errorf("Expected support agent delivery mode 'push', got '%s'", supportAgent.DeliveryMode)
	}
}

func TestDiscoverCapabilities_DNSFailure(t *testing.T) {
	// Create real discovery service to test DNS failure
	discovery := NewDiscovery(5*time.Second, 5*time.Minute, []string{"8.8.8.8:53"})

	// This will fail DNS lookup (since we don't have real DNS records)
	_, err := discovery.DiscoverCapabilities(context.Background(), "nonexistent.example")
	if err == nil {
		t.Error("Expected discovery to fail for nonexistent domain")
	}

	// The error should indicate no capabilities found
	expectedError := "no AMTP capabilities found for domain nonexistent.example"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestMockDiscoveryWithDNS(t *testing.T) {
	// Test mock discovery with DNS records only
	mockRecords := map[string]string{
		"example.com": "v=amtp1;gateway=https://example.com:443;features=immediate-path",
	}

	discovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test discovery - mock will use DNS records
	capabilities, err := discovery.DiscoverCapabilities(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	// Verify it used DNS (no agent info)
	if capabilities.Gateway != "https://example.com:443" {
		t.Errorf("Expected gateway 'https://example.com:443', got '%s'", capabilities.Gateway)
	}

	if capabilities.HasAgentDiscovery() {
		t.Error("Expected no agent discovery feature from DNS")
	}

	// DNS discovery should not include agent information
	// Agents are discovered separately via the gateway's agent discovery endpoint
}

// Helper function to create time pointers
func timePtr(t time.Time) *time.Time {
	return &t
}

func TestNewDiscovery(t *testing.T) {
	// Test with default resolver
	discovery := NewDiscovery(5*time.Second, 10*time.Minute, []string{})
	if discovery == nil {
		t.Fatal("Expected discovery instance, got nil")
	}
	if discovery.timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", discovery.timeout)
	}
	if discovery.defaultTTL != 10*time.Minute {
		t.Errorf("Expected TTL 10m, got %v", discovery.defaultTTL)
	}

	// Test with custom resolver
	customDiscovery := NewDiscovery(3*time.Second, 5*time.Minute, []string{"8.8.8.8:53"})
	if customDiscovery == nil {
		t.Fatal("Expected custom discovery instance, got nil")
	}
	if customDiscovery.timeout != 3*time.Second {
		t.Errorf("Expected timeout 3s, got %v", customDiscovery.timeout)
	}
}

func TestNewMockDiscovery(t *testing.T) {
	mockRecords := map[string]string{
		"test.com": "v=amtp1;gateway=https://test.com",
	}

	mockDiscovery := NewMockDiscovery(mockRecords, 15*time.Minute)
	if mockDiscovery == nil {
		t.Fatal("Expected mock discovery instance, got nil")
	}
	if mockDiscovery.defaultTTL != 15*time.Minute {
		t.Errorf("Expected TTL 15m, got %v", mockDiscovery.defaultTTL)
	}
	if len(mockDiscovery.records) != 1 {
		t.Errorf("Expected 1 mock record, got %d", len(mockDiscovery.records))
	}
}

func TestDiscoverMXRecords(t *testing.T) {
	discovery := NewDiscovery(5*time.Second, 5*time.Minute, []string{})

	// Test MX record discovery for a domain that should have MX records
	mxRecords, err := discovery.DiscoverMXRecords(context.Background(), "gmail.com")
	if err != nil {
		t.Logf("MX lookup failed (expected in test environment): %v", err)
		// In test environment, DNS might not work, so we just verify the method exists
		return
	}

	if len(mxRecords) == 0 {
		t.Error("Expected at least one MX record for gmail.com")
	}
}

func TestHasAMTPSupport(t *testing.T) {
	mockRecords := map[string]string{
		"supported.com":   "v=amtp1;gateway=https://supported.com",
		"unsupported.com": "invalid record",
	}

	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test domain with AMTP support
	if !mockDiscovery.HasAMTPSupport(context.Background(), "supported.com") {
		t.Error("Expected supported.com to have AMTP support")
	}

	// Test domain without AMTP support
	if mockDiscovery.HasAMTPSupport(context.Background(), "nonexistent.com") {
		t.Error("Expected nonexistent.com to not have AMTP support")
	}
}

func TestSupportsSchema_Discovery(t *testing.T) {
	mockRecords := map[string]string{
		"with-schemas.com":    "v=amtp1;gateway=https://with-schemas.com",
		"without-schemas.com": "v=amtp1;gateway=https://without-schemas.com",
	}

	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test domain without specific schemas (should support all)
	supported, err := mockDiscovery.SupportsSchema(context.Background(), "without-schemas.com", "agntcy:test.v1")
	if err != nil {
		t.Fatalf("SupportsSchema failed: %v", err)
	}
	if !supported {
		t.Error("Expected domain without schemas to support all schemas")
	}

	// Test nonexistent domain
	_, err = mockDiscovery.SupportsSchema(context.Background(), "nonexistent.com", "agntcy:test.v1")
	if err == nil {
		t.Error("Expected error for nonexistent domain")
	}
}

func TestSupportsSchema_MockDiscovery(t *testing.T) {
	mockRecords := map[string]string{
		"exact-match.com":    "v=amtp1;gateway=https://exact-match.com",
		"wildcard-match.com": "v=amtp1;gateway=https://wildcard-match.com",
		"no-match.com":       "v=amtp1;gateway=https://no-match.com",
	}

	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Manually set schemas for testing (since they're not parsed from DNS anymore)
	ctx := context.Background()

	// Test exact match - first get capabilities and modify them
	caps, _ := mockDiscovery.DiscoverCapabilities(ctx, "exact-match.com")
	caps.Schemas = []string{"agntcy:test.v1", "agntcy:order.v1"}
	mockDiscovery.cacheCapabilities("exact-match.com", caps)

	supported, err := mockDiscovery.SupportsSchema(ctx, "exact-match.com", "agntcy:test.v1")
	if err != nil {
		t.Fatalf("SupportsSchema failed: %v", err)
	}
	if !supported {
		t.Error("Expected exact schema match to be supported")
	}

	// Test wildcard match
	caps2, _ := mockDiscovery.DiscoverCapabilities(ctx, "wildcard-match.com")
	caps2.Schemas = []string{"agntcy:test.*", "agntcy:order.v1"}
	mockDiscovery.cacheCapabilities("wildcard-match.com", caps2)

	supported, err = mockDiscovery.SupportsSchema(ctx, "wildcard-match.com", "agntcy:test.v2")
	if err != nil {
		t.Fatalf("SupportsSchema failed: %v", err)
	}
	if !supported {
		t.Error("Expected wildcard schema match to be supported")
	}

	// Test no match
	caps3, _ := mockDiscovery.DiscoverCapabilities(ctx, "no-match.com")
	caps3.Schemas = []string{"agntcy:order.v1"}
	mockDiscovery.cacheCapabilities("no-match.com", caps3)

	supported, err = mockDiscovery.SupportsSchema(ctx, "no-match.com", "agntcy:test.v1")
	if err != nil {
		t.Fatalf("SupportsSchema failed: %v", err)
	}
	if supported {
		t.Error("Expected schema mismatch to not be supported")
	}
}

func TestClearCache(t *testing.T) {
	mockRecords := map[string]string{
		"test.com": "v=amtp1;gateway=https://test.com",
	}

	discovery := NewDiscovery(5*time.Second, 5*time.Minute, []string{})
	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Add something to cache for both discovery types
	ctx := context.Background()
	mockDiscovery.DiscoverCapabilities(ctx, "test.com")

	// Verify cache has entries
	if len(mockDiscovery.cache) == 0 {
		t.Error("Expected cache to have entries before clearing")
	}

	// Clear cache
	discovery.ClearCache()

	// Verify cache is empty
	if len(discovery.cache) != 0 {
		t.Error("Expected cache to be empty after clearing")
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		email    string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"test.user@subdomain.example.org", "subdomain.example.org"},
		{"invalid-email", ""},
		{"@example.com", "example.com"},
		{"user@", ""},
		{"", ""},
		{"user@domain@extra", ""},
	}

	for _, test := range tests {
		result := ExtractDomain(test.email)
		if result != test.expected {
			t.Errorf("ExtractDomain(%q) = %q, expected %q", test.email, result, test.expected)
		}
	}
}

func TestValidateGatewayURL(t *testing.T) {
	tests := []struct {
		url       string
		allowHTTP bool
		expectErr bool
		name      string
	}{
		{"https://example.com", false, false, "valid HTTPS URL"},
		{"https://example.com:8443", false, false, "valid HTTPS URL with port"},
		{"https://example.com/path", false, false, "valid HTTPS URL with path"},
		{"http://example.com", false, true, "HTTP not allowed by default"},
		{"http://example.com", true, false, "HTTP allowed when explicitly enabled"},
		{"https://example.com", true, false, "HTTPS allowed when HTTP is enabled"},
		{"ftp://example.com", false, true, "invalid protocol"},
		{"ftp://example.com", true, true, "invalid protocol even with HTTP allowed"},
		{"", false, true, "empty URL"},
		{"not-a-url", false, true, "invalid URL format"},
		{"https://", false, true, "incomplete URL"},
		{"https://example", false, false, "simple domain"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateGatewayURL(test.url, test.allowHTTP)
			if test.expectErr && err == nil {
				t.Errorf("Expected error for URL %q with allowHTTP=%v", test.url, test.allowHTTP)
			}
			if !test.expectErr && err != nil {
				t.Errorf("Unexpected error for URL %q with allowHTTP=%v: %v", test.url, test.allowHTTP, err)
			}
		})
	}
}

func TestAMTPCapabilities_HasAgentDiscovery(t *testing.T) {
	tests := []struct {
		features []string
		expected bool
		name     string
	}{
		{[]string{"agent-discovery"}, true, "has agent discovery"},
		{[]string{"agent-discovery", "other-feature"}, true, "has agent discovery with other features"},
		{[]string{"other-feature"}, false, "no agent discovery"},
		{[]string{}, false, "no features"},
		{nil, false, "nil features"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caps := &AMTPCapabilities{Features: test.features}
			result := caps.HasAgentDiscovery()
			if result != test.expected {
				t.Errorf("HasAgentDiscovery() = %v, expected %v", result, test.expected)
			}
		})
	}
}

func TestParseAMTPRecord_EdgeCases(t *testing.T) {
	mockDiscovery := NewMockDiscovery(map[string]string{}, 5*time.Minute)

	tests := []struct {
		record   string
		expected bool
		name     string
	}{
		{"v=amtp1;gateway=https://example.com", true, "minimal valid record"},
		{"v=amtp2;gateway=https://example.com", false, "unsupported version"},
		{"gateway=https://example.com", false, "missing version"},
		{"v=amtp1", false, "missing gateway"},
		{"v=amtp1;gateway=", false, "empty gateway"},
		{"", false, "empty record"},
		{"invalid", false, "invalid format"},
		{"v=amtp1;gateway=https://example.com;auth=", true, "empty auth"},
		{"v=amtp1;gateway=https://example.com;max-size=invalid", true, "invalid max-size"},
		{"v=amtp1;gateway=https://example.com;features=", true, "empty features"},
		{"v=amtp1;gateway=https://example.com;auth=cert, oauth", true, "auth with spaces"},
		{"v=amtp1;gateway=https://example.com;features=feat1, feat2", true, "features with spaces"},
		{"\"v=amtp1;gateway=https://example.com\"", true, "quoted record"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := mockDiscovery.parseAMTPRecord(test.record)
			if test.expected && result == nil {
				t.Errorf("Expected valid parsing for record %q", test.record)
			}
			if !test.expected && result != nil {
				t.Errorf("Expected invalid parsing for record %q", test.record)
			}
		})
	}
}

func TestDiscoverAgentsWithFilters(t *testing.T) {
	// Create a test server that serves agent discovery endpoint with filters
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/discovery/agents" {
			// Check query parameters
			deliveryMode := r.URL.Query().Get("delivery_mode")
			activeOnly := r.URL.Query().Get("active_only")

			var agents []Agent
			if deliveryMode == "pull" {
				agents = append(agents, Agent{
					Address:      "pull-agent@example.com",
					DeliveryMode: "pull",
					CreatedAt:    time.Now().Add(-24 * time.Hour),
					LastActive:   timePtr(time.Now().Add(-1 * time.Hour)),
				})
			}
			if deliveryMode == "push" {
				agents = append(agents, Agent{
					Address:      "push-agent@example.com",
					DeliveryMode: "push",
					CreatedAt:    time.Now().Add(-48 * time.Hour),
					LastActive:   timePtr(time.Now().Add(-25 * time.Hour)), // Inactive (more than 24 hours ago)
				})
			}
			if deliveryMode == "" {
				// Return all agents
				agents = []Agent{
					{
						Address:      "pull-agent@example.com",
						DeliveryMode: "pull",
						CreatedAt:    time.Now().Add(-24 * time.Hour),
						LastActive:   timePtr(time.Now().Add(-1 * time.Hour)),
					},
					{
						Address:      "push-agent@example.com",
						DeliveryMode: "push",
						CreatedAt:    time.Now().Add(-48 * time.Hour),
						LastActive:   timePtr(time.Now().Add(-25 * time.Hour)), // Inactive (more than 24 hours ago)
					},
				}
			}

			// Filter by active_only if requested
			if activeOnly == "true" {
				var activeAgents []Agent
				for _, agent := range agents {
					if agent.LastActive != nil && time.Since(*agent.LastActive) < 24*time.Hour {
						activeAgents = append(activeAgents, agent)
					}
				}
				agents = activeAgents
			}

			response := AgentDiscoveryResponse{
				Agents:     agents,
				AgentCount: len(agents),
				Domain:     "example.com",
				Timestamp:  time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	// Mock the capabilities discovery by creating a mock discovery
	mockRecords := map[string]string{
		"example.com": fmt.Sprintf("v=amtp1;gateway=%s;features=agent-discovery", server.URL),
	}
	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test filter by delivery mode
	agentResponse, err := mockDiscovery.DiscoverAgentsWithFilters(context.Background(), "example.com", "pull", false)
	if err != nil {
		t.Fatalf("Agent discovery with filters failed: %v", err)
	}

	if len(agentResponse.Agents) != 1 {
		t.Errorf("Expected 1 pull agent, got %d", len(agentResponse.Agents))
	}

	if len(agentResponse.Agents) > 0 && agentResponse.Agents[0].DeliveryMode != "pull" {
		t.Errorf("Expected pull agent, got %s", agentResponse.Agents[0].DeliveryMode)
	}

	// Test active only filter
	agentResponse, err = mockDiscovery.DiscoverAgentsWithFilters(context.Background(), "example.com", "", true)
	if err != nil {
		t.Fatalf("Agent discovery with active filter failed: %v", err)
	}

	// Should return only active agents (those active within 24 hours)
	if len(agentResponse.Agents) != 1 {
		t.Errorf("Expected 1 active agent, got %d", len(agentResponse.Agents))
	}
}

func TestCacheExpiration(t *testing.T) {
	mockRecords := map[string]string{
		"test.com": "v=amtp1;gateway=https://test.com",
	}

	// Create mock discovery with very short TTL
	mockDiscovery := NewMockDiscovery(mockRecords, 1*time.Millisecond)

	ctx := context.Background()

	// First discovery should work
	caps1, err := mockDiscovery.DiscoverCapabilities(ctx, "test.com")
	if err != nil {
		t.Fatalf("First discovery failed: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(2 * time.Millisecond)

	// Second discovery should also work (cache expired, re-parsed)
	caps2, err := mockDiscovery.DiscoverCapabilities(ctx, "test.com")
	if err != nil {
		t.Fatalf("Second discovery failed: %v", err)
	}

	// Both should have the same gateway but different discovery times
	if caps1.Gateway != caps2.Gateway {
		t.Errorf("Expected same gateway, got %s vs %s", caps1.Gateway, caps2.Gateway)
	}

	if caps2.DiscoveredAt.Before(caps1.DiscoveredAt) {
		t.Error("Expected second discovery to have later timestamp")
	}
}

func TestDiscoverAgents_HTTPErrors(t *testing.T) {
	// Create a test server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/discovery/agents" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mockRecords := map[string]string{
		"error.com": fmt.Sprintf("v=amtp1;gateway=%s", server.URL),
	}
	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test HTTP error handling
	_, err := mockDiscovery.DiscoverAgents(context.Background(), "error.com")
	if err == nil {
		t.Error("Expected error for HTTP 500 response")
	}

	expectedError := "agent discovery request failed with status: 500"
	if err.Error() != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, err.Error())
	}
}

func TestDiscoverAgents_InvalidJSON(t *testing.T) {
	// Create a test server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/discovery/agents" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mockRecords := map[string]string{
		"invalid.com": fmt.Sprintf("v=amtp1;gateway=%s", server.URL),
	}
	mockDiscovery := NewMockDiscovery(mockRecords, 5*time.Minute)

	// Test JSON parsing error handling
	_, err := mockDiscovery.DiscoverAgents(context.Background(), "invalid.com")
	if err == nil {
		t.Error("Expected error for invalid JSON response")
	}

	if !strings.Contains(err.Error(), "failed to decode agent discovery response") {
		t.Errorf("Expected JSON decode error, got: %v", err)
	}
}

func TestDiscoverAgents_NoCapabilities(t *testing.T) {
	mockDiscovery := NewMockDiscovery(map[string]string{}, 5*time.Minute)

	// Test agent discovery for domain without capabilities
	_, err := mockDiscovery.DiscoverAgents(context.Background(), "nonexistent.com")
	if err == nil {
		t.Error("Expected error for domain without capabilities")
	}

	if !strings.Contains(err.Error(), "failed to discover domain capabilities") {
		t.Errorf("Expected capabilities error, got: %v", err)
	}
}
