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

package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RegistryConfig holds configuration for HTTP registry client
type RegistryConfig struct {
	BaseURL    string            `yaml:"base_url" json:"base_url"`
	Timeout    time.Duration     `yaml:"timeout" json:"timeout"`
	Headers    map[string]string `yaml:"headers" json:"headers"`
	AuthToken  string            `yaml:"auth_token" json:"auth_token"`
	TLSConfig  TLSConfig         `yaml:"tls" json:"tls"`
	RetryCount int               `yaml:"retry_count" json:"retry_count"`
}

// TLSConfig holds TLS configuration for registry client
type TLSConfig struct {
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
	CertFile           string `yaml:"cert_file" json:"cert_file"`
	KeyFile            string `yaml:"key_file" json:"key_file"`
	CAFile             string `yaml:"ca_file" json:"ca_file"`
}

// HTTPRegistryClient implements RegistryClient for HTTP-based registries
type HTTPRegistryClient struct {
	baseURL    string
	httpClient *http.Client
	headers    map[string]string
	authToken  string
}

// NewHTTPRegistryClient creates a new HTTP registry client
func NewHTTPRegistryClient(config RegistryConfig) *HTTPRegistryClient {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTTPRegistryClient{
		baseURL: strings.TrimRight(config.BaseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		headers:   config.Headers,
		authToken: config.AuthToken,
	}
}

// GetSchema retrieves a schema from the HTTP registry
func (c *HTTPRegistryClient) GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	path := fmt.Sprintf("/schemas/%s", url.PathEscape(id.String()))

	resp, err := c.makeRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("schema not found: %s", id.String())
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for schema %s", resp.StatusCode, id.String())
	}

	var schema Schema
	if err := json.NewDecoder(resp.Body).Decode(&schema); err != nil {
		return nil, fmt.Errorf("failed to decode schema response: %w", err)
	}

	return &schema, nil
}

// ListSchemas lists schemas from the HTTP registry
func (c *HTTPRegistryClient) ListSchemas(ctx context.Context, pattern string) ([]SchemaIdentifier, error) {
	path := "/schemas"
	if pattern != "" {
		path += "?pattern=" + url.QueryEscape(pattern)
	}

	resp, err := c.makeRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for schema list", resp.StatusCode)
	}

	var response struct {
		Schemas []SchemaIdentifier `json:"schemas"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode schema list response: %w", err)
	}

	return response.Schemas, nil
}

// ValidateSchema validates a schema definition
func (c *HTTPRegistryClient) ValidateSchema(ctx context.Context, schema *Schema) error {
	requestBody, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal schema for validation: %w", err)
	}

	resp, err := c.makeRequest(ctx, "POST", "/schemas/validate", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to validate schema: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errorResp) // #nosec G104 -- ignore decode error
		return fmt.Errorf("schema validation failed: %s", errorResp.Error)
	}

	return nil
}

// CheckCompatibility checks compatibility between schemas
func (c *HTTPRegistryClient) CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error) {
	// Construct request body
	compatRequest := map[string]interface{}{
		"current": current,
		"new":     new,
	}

	requestBody, err := json.Marshal(compatRequest)
	if err != nil {
		return false, fmt.Errorf("failed to marshal compatibility request: %w", err)
	}

	resp, err := c.makeRequest(ctx, "POST", "/schemas/compatibility", bytes.NewReader(requestBody))
	if err != nil {
		return false, fmt.Errorf("failed to check compatibility: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("registry returned status %d for compatibility check", resp.StatusCode)
	}

	var compatResult CompatibilityResult
	if err := json.NewDecoder(resp.Body).Decode(&compatResult); err != nil {
		return false, fmt.Errorf("failed to decode compatibility response: %w", err)
	}

	return compatResult.Compatible, nil
}

// RegisterSchema registers a new schema (not implemented for HTTP client)
func (c *HTTPRegistryClient) RegisterSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	return fmt.Errorf("RegisterSchema not implemented for HTTP registry client")
}

// RegisterOrUpdateSchema registers or updates a schema (not implemented for HTTP client)
func (c *HTTPRegistryClient) RegisterOrUpdateSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	return fmt.Errorf("RegisterOrUpdateSchema not implemented for HTTP registry client")
}

// DeleteSchema deletes a schema (not implemented for HTTP client)
func (c *HTTPRegistryClient) DeleteSchema(ctx context.Context, id SchemaIdentifier) error {
	return fmt.Errorf("DeleteSchema not implemented for HTTP registry client")
}

// GetStats returns registry statistics (not implemented for HTTP client)
func (c *HTTPRegistryClient) GetStats() RegistryStats {
	return RegistryStats{}
}

// makeRequest makes an HTTP request to the registry
func (c *HTTPRegistryClient) makeRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Add custom headers
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	// Add auth token if provided
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	return c.httpClient.Do(req)
}

// CompatibilityResult represents the result of a compatibility check
type CompatibilityResult struct {
	Compatible bool   `json:"compatible"`
	Reason     string `json:"reason,omitempty"`
}

// MockRegistryClient implements RegistryClient for testing
type MockRegistryClient struct {
	schemas map[string]*Schema
	errors  map[string]error
}

// NewMockRegistryClient creates a new mock registry client
func NewMockRegistryClient() *MockRegistryClient {
	return &MockRegistryClient{
		schemas: make(map[string]*Schema),
		errors:  make(map[string]error),
	}
}

// AddSchema adds a schema to the mock registry
func (m *MockRegistryClient) AddSchema(schema *Schema) {
	m.schemas[schema.ID.String()] = schema
}

// SetError sets an error for a specific schema ID
func (m *MockRegistryClient) SetError(schemaID string, err error) {
	m.errors[schemaID] = err
}

// GetSchema retrieves a schema from the mock registry
func (m *MockRegistryClient) GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	if err, exists := m.errors[id.String()]; exists {
		return nil, err
	}

	schema, exists := m.schemas[id.String()]
	if !exists {
		return nil, fmt.Errorf("schema not found: %s", id.String())
	}

	// Return a copy to prevent modification
	schemaCopy := *schema
	return &schemaCopy, nil
}

// ListSchemas lists schemas from the mock registry
func (m *MockRegistryClient) ListSchemas(ctx context.Context, pattern string) ([]SchemaIdentifier, error) {
	var result []SchemaIdentifier

	for _, schema := range m.schemas {
		if pattern == "" || schema.ID.MatchesPattern(pattern) {
			result = append(result, schema.ID)
		}
	}

	return result, nil
}

// ValidateSchema validates a schema in the mock registry
func (m *MockRegistryClient) ValidateSchema(ctx context.Context, schema *Schema) error {
	// Mock validation always passes
	return nil
}

// CheckCompatibility checks compatibility in the mock registry
func (m *MockRegistryClient) CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error) {
	// Mock compatibility check based on version compatibility
	return current.IsCompatibleWith(&new), nil
}

// RegisterSchema registers a new schema in the mock registry
func (m *MockRegistryClient) RegisterSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	// Check if schema already exists
	if _, exists := m.schemas[schema.ID.String()]; exists {
		return fmt.Errorf("schema already exists: %s", schema.ID.String())
	}

	// Add to mock registry
	m.schemas[schema.ID.String()] = schema
	return nil
}

// RegisterOrUpdateSchema registers or updates a schema in the mock registry
func (m *MockRegistryClient) RegisterOrUpdateSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	// Add or update schema in map
	m.schemas[schema.ID.String()] = schema
	return nil
}

// DeleteSchema deletes a schema from the mock registry
func (m *MockRegistryClient) DeleteSchema(ctx context.Context, id SchemaIdentifier) error {
	if _, exists := m.schemas[id.String()]; !exists {
		return fmt.Errorf("schema not found: %s", id.String())
	}

	// Remove from map
	delete(m.schemas, id.String())
	return nil
}

// GetStats returns mock registry statistics
func (m *MockRegistryClient) GetStats() RegistryStats {
	domains := make(map[string]int)
	entities := make(map[string]int)

	for _, schema := range m.schemas {
		domains[schema.ID.Domain]++
		entities[schema.ID.Entity]++
	}

	return RegistryStats{
		TotalSchemas: len(m.schemas),
		Domains:      domains,
		Entities:     entities,
	}
}
