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
