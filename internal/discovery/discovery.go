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
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AMTPCapabilities represents AMTP capabilities discovered via DNS or HTTP
type AMTPCapabilities struct {
	Version      string        `json:"version"`
	Gateway      string        `json:"gateway"`
	Schemas      []string      `json:"schemas,omitempty"`
	Auth         []string      `json:"auth,omitempty"`
	MaxSize      int64         `json:"max_size,omitempty"`
	Features     []string      `json:"features,omitempty"`
	JWKS         string        `json:"jwks,omitempty"`
	Domain       string        `json:"domain,omitempty"`
	DiscoveredAt time.Time     `json:"discovered_at"`
	TTL          time.Duration `json:"ttl"`
}

// Agent represents an agent in the agent discovery response
type Agent struct {
	Address          string     `json:"address"`
	DeliveryMode     string     `json:"delivery_mode"`
	SupportedSchemas []string   `json:"supported_schemas,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	LastActive       *time.Time `json:"last_active,omitempty"`
}

// AgentDiscoveryResponse represents the response from the agent discovery endpoint
type AgentDiscoveryResponse struct {
	Agents     []Agent   `json:"agents"`
	AgentCount int       `json:"agent_count"`
	Domain     string    `json:"domain"`
	Timestamp  time.Time `json:"timestamp"`
}

// Discovery provides AMTP capability discovery via DNS TXT records
type Discovery struct {
	resolver   *net.Resolver
	cache      map[string]*cacheEntry
	cacheMutex sync.RWMutex
	timeout    time.Duration
	defaultTTL time.Duration
}

type cacheEntry struct {
	capabilities *AMTPCapabilities
	expiresAt    time.Time
}

// NewDiscovery creates a new discovery service
func NewDiscovery(timeout, defaultTTL time.Duration, resolvers []string) *Discovery {
	var resolver *net.Resolver

	if len(resolvers) > 0 {
		// Use custom DNS resolver
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: timeout,
				}
				// Use the first custom resolver instead of the default
				return d.DialContext(ctx, network, resolvers[0])
			},
		}
	} else {
		// Use system default resolver
		resolver = net.DefaultResolver
	}

	return &Discovery{
		resolver:   resolver,
		cache:      make(map[string]*cacheEntry),
		timeout:    timeout,
		defaultTTL: defaultTTL,
	}
}

// MockDiscovery provides a mock DNS discovery service for development/testing
type MockDiscovery struct {
	records    map[string]string
	cache      map[string]*cacheEntry
	cacheMutex sync.RWMutex
	defaultTTL time.Duration
}

// NewMockDiscovery creates a new mock discovery service
func NewMockDiscovery(mockRecords map[string]string, defaultTTL time.Duration) *MockDiscovery {
	return &MockDiscovery{
		records:    mockRecords,
		cache:      make(map[string]*cacheEntry),
		defaultTTL: defaultTTL,
	}
}

// DiscoverCapabilities discovers AMTP capabilities using mock records
func (m *MockDiscovery) DiscoverCapabilities(ctx context.Context, domain string) (*AMTPCapabilities, error) {
	// Check cache first
	if cached := m.getCached(domain); cached != nil {
		return cached, nil
	}

	// Check mock records
	if record, exists := m.records[domain]; exists {
		if capabilities := m.parseAMTPRecord(record); capabilities != nil {
			capabilities.DiscoveredAt = time.Now()
			capabilities.TTL = m.defaultTTL
			m.cacheCapabilities(domain, capabilities)
			return capabilities, nil
		}
	}

	return nil, fmt.Errorf("no AMTP capabilities found for domain %s", domain)
}

// SupportsSchema checks if a domain supports a specific schema using mock data
func (m *MockDiscovery) SupportsSchema(ctx context.Context, domain, schema string) (bool, error) {
	capabilities, err := m.DiscoverCapabilities(ctx, domain)
	if err != nil {
		return false, err
	}

	// If no schemas specified, assume all are supported
	if len(capabilities.Schemas) == 0 {
		return true, nil
	}

	// Check for exact match or wildcard match
	for _, supportedSchema := range capabilities.Schemas {
		if supportedSchema == schema {
			return true, nil
		}

		// Check wildcard patterns (e.g., "agntcy:test.*")
		if strings.HasSuffix(supportedSchema, "*") {
			prefix := strings.TrimSuffix(supportedSchema, "*")
			if strings.HasPrefix(schema, prefix) {
				return true, nil
			}
		}
	}

	return false, nil
}

// parseAMTPRecord parses an AMTP DNS TXT record (reused from Discovery)
func (m *MockDiscovery) parseAMTPRecord(record string) *AMTPCapabilities {
	// AMTP TXT record format: "v=amtp1;gateway=https://...;auth=...;max-size=..."
	if !strings.HasPrefix(record, "v=amtp") {
		return nil
	}

	capabilities := &AMTPCapabilities{}
	parts := strings.Split(record, ";")

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		switch key {
		case "v":
			if value == "amtp1" {
				capabilities.Version = "1.0"
			} else {
				return nil // Unsupported version
			}

		case "gateway":
			capabilities.Gateway = value

		case "auth":
			if value != "" {
				capabilities.Auth = strings.Split(value, ",")
				// Trim whitespace from each auth method
				for i, auth := range capabilities.Auth {
					capabilities.Auth[i] = strings.TrimSpace(auth)
				}
			}

		case "max-size":
			if size, err := strconv.ParseInt(value, 10, 64); err == nil {
				capabilities.MaxSize = size
			}

		case "features":
			if value != "" {
				capabilities.Features = strings.Split(value, ",")
				// Trim whitespace from each feature
				for i, feature := range capabilities.Features {
					capabilities.Features[i] = strings.TrimSpace(feature)
				}
			}
		}
	}

	// Validate required fields
	if capabilities.Version == "" || capabilities.Gateway == "" {
		return nil
	}

	return capabilities
}

// getCached retrieves cached capabilities if still valid
func (m *MockDiscovery) getCached(domain string) *AMTPCapabilities {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	entry, exists := m.cache[domain]
	if !exists || time.Now().After(entry.expiresAt) {
		return nil
	}

	return entry.capabilities
}

// cacheCapabilities stores capabilities in cache
func (m *MockDiscovery) cacheCapabilities(domain string, capabilities *AMTPCapabilities) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	expiresAt := time.Now().Add(capabilities.TTL)
	m.cache[domain] = &cacheEntry{
		capabilities: capabilities,
		expiresAt:    expiresAt,
	}
}

// DiscoverAgents discovers agents for a domain using mock discovery
func (m *MockDiscovery) DiscoverAgents(ctx context.Context, domain string) (*AgentDiscoveryResponse, error) {
	// First discover the gateway capabilities to get the gateway URL
	capabilities, err := m.DiscoverCapabilities(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to discover domain capabilities: %w", err)
	}

	// Create HTTP client for agent discovery
	httpClient := &http.Client{Timeout: 5 * time.Second}

	// Construct agent discovery URL
	agentDiscoveryURL := fmt.Sprintf("%s/v1/discovery/agents", capabilities.Gateway)

	// Make HTTP request to agent discovery endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", agentDiscoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent discovery request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // nolint:errcheck
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent discovery request failed with status: %d", resp.StatusCode)
	}

	// Parse the agent discovery response (direct format, not wrapped in success/data)
	var agentResponse AgentDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentResponse); err != nil {
		return nil, fmt.Errorf("failed to decode agent discovery response: %w", err)
	}

	return &agentResponse, nil
}

// DiscoverCapabilities discovers AMTP capabilities for a domain using DNS TXT records only
func (d *Discovery) DiscoverCapabilities(ctx context.Context, domain string) (*AMTPCapabilities, error) {
	// Check cache first
	if cached := d.getCached(domain); cached != nil {
		return cached, nil
	}

	// Use DNS TXT record discovery only
	capabilities, err := d.discoverViaDNS(ctx, domain)
	if err == nil {
		d.cacheCapabilities(domain, capabilities)
		return capabilities, nil
	}

	return nil, fmt.Errorf("no AMTP capabilities found for domain %s", domain)
}

// discoverViaDNS discovers capabilities via DNS TXT records
func (d *Discovery) discoverViaDNS(ctx context.Context, domain string) (*AMTPCapabilities, error) {
	// Query _amtp.{domain} TXT record
	txtRecords, err := d.resolver.LookupTXT(ctx, "_amtp."+domain)
	if err != nil {
		return nil, fmt.Errorf("DNS TXT lookup failed: %w", err)
	}

	// Parse AMTP TXT record
	for _, record := range txtRecords {
		if capabilities := d.parseAMTPRecord(record); capabilities != nil {
			capabilities.DiscoveredAt = time.Now()
			capabilities.TTL = d.defaultTTL
			return capabilities, nil
		}
	}

	return nil, fmt.Errorf("no valid AMTP TXT record found")
}

// parseAMTPRecord parses an AMTP DNS TXT record
func (d *Discovery) parseAMTPRecord(record string) *AMTPCapabilities {
	// Clean up the record - remove extra quotes that may be added by DNS servers
	record = strings.Trim(record, "\"")

	// AMTP TXT record format: "v=amtp1;gateway=https://...;auth=...;max-size=..."
	if !strings.HasPrefix(record, "v=amtp") {
		return nil
	}

	capabilities := &AMTPCapabilities{}
	parts := strings.Split(record, ";")

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		switch key {
		case "v":
			if value == "amtp1" {
				capabilities.Version = "1.0"
			} else {
				return nil // Unsupported version
			}

		case "gateway":
			capabilities.Gateway = value

		case "auth":
			if value != "" {
				capabilities.Auth = strings.Split(value, ",")
				// Trim whitespace from each auth method
				for i, auth := range capabilities.Auth {
					capabilities.Auth[i] = strings.TrimSpace(auth)
				}
			}

		case "max-size":
			if size, err := strconv.ParseInt(value, 10, 64); err == nil {
				capabilities.MaxSize = size
			}

		case "features":
			if value != "" {
				capabilities.Features = strings.Split(value, ",")
				// Trim whitespace from each feature
				for i, feature := range capabilities.Features {
					capabilities.Features[i] = strings.TrimSpace(feature)
				}
			}
		}
	}

	// Validate required fields
	if capabilities.Version == "" || capabilities.Gateway == "" {
		return nil
	}

	return capabilities
}

// DiscoverMXRecords discovers MX records for SMTP fallback
func (d *Discovery) DiscoverMXRecords(ctx context.Context, domain string) ([]*net.MX, error) {
	mxRecords, err := d.resolver.LookupMX(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("MX lookup failed: %w", err)
	}

	return mxRecords, nil
}

// HasAMTPSupport checks if a domain supports AMTP
func (d *Discovery) HasAMTPSupport(ctx context.Context, domain string) bool {
	_, err := d.DiscoverCapabilities(ctx, domain)
	return err == nil
}

// SupportsSchema checks if a domain supports a specific schema
func (d *Discovery) SupportsSchema(ctx context.Context, domain, schema string) (bool, error) {
	capabilities, err := d.DiscoverCapabilities(ctx, domain)
	if err != nil {
		return false, err
	}

	// If no schemas specified, assume all are supported
	if len(capabilities.Schemas) == 0 {
		return true, nil
	}

	// Check for exact match or wildcard match
	for _, supportedSchema := range capabilities.Schemas {
		if supportedSchema == schema {
			return true, nil
		}

		// Check wildcard patterns (e.g., "agntcy:commerce.*")
		if strings.HasSuffix(supportedSchema, "*") {
			prefix := strings.TrimSuffix(supportedSchema, "*")
			if strings.HasPrefix(schema, prefix) {
				return true, nil
			}
		}
	}

	return false, nil
}

// getCached retrieves cached capabilities if still valid
func (d *Discovery) getCached(domain string) *AMTPCapabilities {
	d.cacheMutex.RLock()
	defer d.cacheMutex.RUnlock()

	entry, exists := d.cache[domain]
	if !exists || time.Now().After(entry.expiresAt) {
		return nil
	}

	return entry.capabilities
}

// cacheCapabilities stores capabilities in cache
func (d *Discovery) cacheCapabilities(domain string, capabilities *AMTPCapabilities) {
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()

	expiresAt := time.Now().Add(capabilities.TTL)
	d.cache[domain] = &cacheEntry{
		capabilities: capabilities,
		expiresAt:    expiresAt,
	}
}

// ClearCache clears the discovery cache
func (d *Discovery) ClearCache() {
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()

	d.cache = make(map[string]*cacheEntry)
}

// ExtractDomain extracts domain from an email address
func ExtractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// ValidateGatewayURL validates that a gateway URL is properly formatted
func ValidateGatewayURL(url string, allowHTTP bool) error {
	if allowHTTP {
		// Allow both HTTP and HTTPS when explicitly enabled
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return fmt.Errorf("gateway URL must use HTTP or HTTPS")
		}

		// Validate HTTP or HTTPS URL format
		urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(?::[0-9]+)?(?:/.*)?$`)
		if !urlRegex.MatchString(url) {
			return fmt.Errorf("invalid gateway URL format")
		}
	} else {
		// Require HTTPS by default for security
		if !strings.HasPrefix(url, "https://") {
			return fmt.Errorf("gateway URL must use HTTPS")
		}

		// Basic HTTPS URL validation
		urlRegex := regexp.MustCompile(`^https://[a-zA-Z0-9.-]+(?::[0-9]+)?(?:/.*)?$`)
		if !urlRegex.MatchString(url) {
			return fmt.Errorf("invalid gateway URL format")
		}
	}

	return nil
}

// HasAgentDiscovery checks if the capabilities support agent discovery
func (c *AMTPCapabilities) HasAgentDiscovery() bool {
	for _, feature := range c.Features {
		if feature == "agent-discovery" {
			return true
		}
	}
	return false
}

// DiscoverAgents discovers agents for a domain by calling the gateway's agent discovery endpoint
// This method should be called after DiscoverCapabilities to get the current agent list
func (d *Discovery) DiscoverAgents(ctx context.Context, domain string) (*AgentDiscoveryResponse, error) {
	// First discover the gateway capabilities to get the gateway URL
	capabilities, err := d.DiscoverCapabilities(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to discover domain capabilities: %w", err)
	}

	// Create HTTP client for agent discovery
	httpClient := &http.Client{Timeout: d.timeout}

	// Construct agent discovery URL
	agentDiscoveryURL := fmt.Sprintf("%s/v1/discovery/agents", capabilities.Gateway)

	// Make HTTP request to agent discovery endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", agentDiscoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent discovery request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // nolint:errcheck
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent discovery request failed with status: %d", resp.StatusCode)
	}

	// Parse the agent discovery response (direct format, not wrapped in success/data)
	var agentResponse AgentDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentResponse); err != nil {
		return nil, fmt.Errorf("failed to decode agent discovery response: %w", err)
	}

	return &agentResponse, nil
}

// DiscoverAgentsWithFilters discovers agents with optional filtering parameters
func (d *Discovery) DiscoverAgentsWithFilters(ctx context.Context, domain string, deliveryMode string, activeOnly bool) (*AgentDiscoveryResponse, error) {
	// First discover the gateway capabilities
	capabilities, err := d.DiscoverCapabilities(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to discover domain capabilities: %w", err)
	}

	// Create HTTP client for agent discovery
	httpClient := &http.Client{Timeout: d.timeout}

	// Construct agent discovery URL with query parameters
	agentDiscoveryURL := fmt.Sprintf("%s/v1/discovery/agents", capabilities.Gateway)

	// Add query parameters
	req, err := http.NewRequestWithContext(ctx, "GET", agentDiscoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent discovery request: %w", err)
	}

	q := req.URL.Query()
	if deliveryMode != "" {
		q.Add("delivery_mode", deliveryMode)
	}
	if activeOnly {
		q.Add("active_only", "true")
	}
	req.URL.RawQuery = q.Encode()

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent discovery request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // nolint:errcheck
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent discovery request failed with status: %d", resp.StatusCode)
	}

	// Parse the agent discovery response (direct format, not wrapped in success/data)
	var agentResponse AgentDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentResponse); err != nil {
		return nil, fmt.Errorf("failed to decode agent discovery response: %w", err)
	}

	return &agentResponse, nil
}
