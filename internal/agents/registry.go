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

package agents

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
)

// LocalAgent represents a local agent configuration
type LocalAgent struct {
	Address          string            `json:"address"`           // agent@domain format
	DeliveryMode     string            `json:"delivery_mode"`     // "push" or "pull"
	PushTarget       string            `json:"push_target"`       // webhook URL for push delivery (required for push mode)
	Headers          map[string]string `json:"headers"`           // additional headers for push
	APIKey           string            `json:"api_key"`           // unique API key for inbox access
	SupportedSchemas []string          `json:"supported_schemas"` // schemas this agent can handle (e.g., ["agntcy:commerce.*", "agntcy:auth.user.*"])
	RequiresSchema   bool              `json:"requires_schema"`   // whether this agent requires schema validation (auto-determined from SupportedSchemas)
	CreatedAt        time.Time         `json:"created_at"`        // registration timestamp
	LastAccess       time.Time         `json:"last_access"`       // last inbox access timestamp
}

// Registry manages local agent registrations and configurations
type Registry struct {
	localDomain   string
	schemaManager SchemaManager
	agents        map[string]*LocalAgent // registered local agents by address
	agentsMutex   sync.RWMutex
}

// SchemaManager interface for schema validation
type SchemaManager interface {
	GetSchema(ctx context.Context, id schema.SchemaIdentifier) (*schema.Schema, error)
	ListSchemas(ctx context.Context, pattern string) ([]schema.SchemaIdentifier, error)
}

// RegistryConfig defines agent registry configuration
type RegistryConfig struct {
	LocalDomain   string
	SchemaManager SchemaManager
}

// NewRegistry creates a new agent registry
func NewRegistry(config RegistryConfig) *Registry {
	return &Registry{
		localDomain:   config.LocalDomain,
		schemaManager: config.SchemaManager,
		agents:        make(map[string]*LocalAgent),
	}
}

// RegisterAgent registers a local agent with delivery configuration
func (r *Registry) RegisterAgent(agent *LocalAgent) error {
	if agent.Address == "" {
		return fmt.Errorf("agent address is required")
	}

	// Process agent address - allow both agent names and full addresses
	fullAddress, err := r.normalizeAgentAddress(agent.Address)
	if err != nil {
		return fmt.Errorf("invalid agent address: %w", err)
	}

	// Update the agent with the normalized full address
	agent.Address = fullAddress

	if agent.DeliveryMode != "push" && agent.DeliveryMode != "pull" {
		return fmt.Errorf("delivery mode must be 'push' or 'pull'")
	}

	if agent.DeliveryMode == "push" && agent.PushTarget == "" {
		return fmt.Errorf("push target URL is required for push delivery mode")
	}

	// Validate supported schemas
	if err := r.validateSupportedSchemas(context.Background(), agent.SupportedSchemas); err != nil {
		return fmt.Errorf("invalid supported schemas: %w", err)
	}

	// Determine if agent requires schema validation based on supported schemas
	// If agent specifies schemas, it requires schema validation
	// If agent has empty schemas, it accepts unstructured messages (no schema required)
	agent.RequiresSchema = len(agent.SupportedSchemas) > 0

	// Generate API key if not provided
	if agent.APIKey == "" {
		apiKey, err := r.GenerateAPIKey()
		if err != nil {
			return fmt.Errorf("failed to generate API key: %w", err)
		}
		agent.APIKey = apiKey
	}

	// Set timestamps
	now := time.Now().UTC()
	agent.CreatedAt = now
	agent.LastAccess = now

	r.agentsMutex.Lock()
	defer r.agentsMutex.Unlock()

	r.agents[agent.Address] = agent
	return nil
}

// UnregisterAgent removes a local agent
func (r *Registry) UnregisterAgent(agentNameOrAddress string) error {
	// Normalize the input to full address
	fullAddress, err := r.normalizeAgentAddress(agentNameOrAddress)
	if err != nil {
		return fmt.Errorf("invalid agent identifier: %w", err)
	}

	r.agentsMutex.Lock()
	defer r.agentsMutex.Unlock()

	if _, exists := r.agents[fullAddress]; !exists {
		return fmt.Errorf("agent not found: %s", fullAddress)
	}

	delete(r.agents, fullAddress)
	return nil
}

// GetAgent returns a specific agent by address
func (r *Registry) GetAgent(agentAddress string) (*LocalAgent, error) {
	r.agentsMutex.RLock()
	defer r.agentsMutex.RUnlock()

	agent, exists := r.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}

	// Return a copy to avoid race conditions
	agentCopy := *agent
	return &agentCopy, nil
}

// GetAllAgents returns all registered local agents
func (r *Registry) GetAllAgents() map[string]*LocalAgent {
	r.agentsMutex.RLock()
	defer r.agentsMutex.RUnlock()

	// Return a copy to avoid race conditions
	agents := make(map[string]*LocalAgent)
	for addr, agent := range r.agents {
		agentCopy := *agent
		agents[addr] = &agentCopy
	}
	return agents
}

// GetSupportedSchemas returns all schemas supported by registered agents
func (r *Registry) GetSupportedSchemas() []string {
	r.agentsMutex.RLock()
	defer r.agentsMutex.RUnlock()

	schemaSet := make(map[string]bool)
	for _, agent := range r.agents {
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

// GenerateAPIKey generates a cryptographically secure API key for an agent
func (r *Registry) GenerateAPIKey() (string, error) {
	// Generate 32 random bytes (256 bits)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode as URL-safe base64 (no padding for cleaner keys)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes), nil
}

// VerifyAPIKey verifies that the provided API key belongs to the specified agent
func (r *Registry) VerifyAPIKey(agentAddress, apiKey string) bool {
	r.agentsMutex.RLock()
	defer r.agentsMutex.RUnlock()

	agent, exists := r.agents[agentAddress]
	if !exists {
		return false
	}

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(agent.APIKey), []byte(apiKey)) == 1
}

// UpdateLastAccess updates the last access timestamp for an agent
func (r *Registry) UpdateLastAccess(agentAddress string) {
	r.agentsMutex.Lock()
	defer r.agentsMutex.Unlock()

	if agent, exists := r.agents[agentAddress]; exists {
		agent.LastAccess = time.Now().UTC()
	}
}

// RotateAPIKey generates a new API key for an existing agent
func (r *Registry) RotateAPIKey(agentAddress string) (string, error) {
	r.agentsMutex.Lock()
	defer r.agentsMutex.Unlock()

	agent, exists := r.agents[agentAddress]
	if !exists {
		return "", fmt.Errorf("agent not found: %s", agentAddress)
	}

	// Generate new API key
	newAPIKey, err := r.GenerateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate new API key: %w", err)
	}

	// Update agent with new key
	agent.APIKey = newAPIKey

	return newAPIKey, nil
}

// StoreMessage is deprecated - inbox storage is now handled by unified message storage
// This method is kept for interface compatibility but does nothing
func (r *Registry) StoreMessage(recipient string, message *types.Message) error {
	// No-op: unified storage handles this now
	return nil
}

// GetInboxMessages is deprecated - inbox access is now handled by unified message storage
// This method is kept for interface compatibility but returns empty
func (r *Registry) GetInboxMessages(recipient string) []*types.Message {
	// No-op: unified storage handles this now
	return []*types.Message{}
}

// AcknowledgeMessage is deprecated - acknowledgment is now handled by unified message storage
// This method is kept for interface compatibility but does nothing
func (r *Registry) AcknowledgeMessage(recipient, messageID string) error {
	// No-op: unified storage handles this now
	return fmt.Errorf("acknowledgment should be handled by unified message storage")
}

// GetStats returns agent registry statistics
func (r *Registry) GetStats() map[string]interface{} {
	r.agentsMutex.RLock()
	defer r.agentsMutex.RUnlock()

	totalAgents := len(r.agents)
	pushAgents := 0
	pullAgents := 0

	for _, agent := range r.agents {
		if agent.DeliveryMode == "push" {
			pushAgents++
		} else {
			pullAgents++
		}
	}

	return map[string]interface{}{
		"local_agents": totalAgents,
		"push_agents":  pushAgents,
		"pull_agents":  pullAgents,
	}
}

// validateSupportedSchemas validates agent's supported schema declarations
func (r *Registry) validateSupportedSchemas(ctx context.Context, schemas []string) error {
	for _, schemaStr := range schemas {
		if schemaStr == "" {
			continue // Skip empty schemas
		}

		// Validate schema format
		if err := r.validateSchemaFormat(schemaStr); err != nil {
			return fmt.Errorf("invalid schema format '%s': %w", schemaStr, err)
		}

		// For non-wildcard schemas, check if they exist in the registry
		if !strings.HasSuffix(schemaStr, "*") && r.schemaManager != nil {
			schemaID, err := schema.ParseSchemaIdentifier(schemaStr)
			if err != nil {
				return fmt.Errorf("invalid schema identifier '%s': %w", schemaStr, err)
			}

			// Check if schema exists in registry
			_, err = r.schemaManager.GetSchema(ctx, *schemaID)
			if err != nil {
				return fmt.Errorf("schema '%s' not found in registry: %w", schemaStr, err)
			}
		}
	}
	return nil
}

// validateSchemaFormat validates the basic format of a schema identifier
func (r *Registry) validateSchemaFormat(schemaStr string) error {
	// Must start with agntcy:
	if !strings.HasPrefix(schemaStr, "agntcy:") {
		return fmt.Errorf("schema must start with 'agntcy:'")
	}

	// Remove agntcy: prefix for validation
	schemaBody := strings.TrimPrefix(schemaStr, "agntcy:")

	// Handle wildcard patterns
	if strings.HasSuffix(schemaBody, "*") {
		schemaBody = strings.TrimSuffix(schemaBody, "*")
		if schemaBody == "" {
			return fmt.Errorf("wildcard schema cannot be just 'agntcy:*'")
		}
	}

	// Must have at least domain.entity format
	if !strings.Contains(schemaBody, ".") {
		return fmt.Errorf("schema must have domain.entity format")
	}

	// For exact schemas (not wildcards), validate full format
	if !strings.HasSuffix(schemaStr, "*") {
		// Should match: agntcy:domain.entity.version
		schemaRegex := regexp.MustCompile(`^agntcy:[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.v[0-9]+$`)
		if !schemaRegex.MatchString(schemaStr) {
			return fmt.Errorf("schema must match format agntcy:domain.entity.version")
		}
	}

	return nil
}

// normalizeAgentAddress processes agent name and constructs full address
func (r *Registry) normalizeAgentAddress(agentName string) (string, error) {
	// Reject full addresses - only accept agent names
	if strings.Contains(agentName, "@") {
		return "", fmt.Errorf("only agent names are allowed, not full addresses. Use '%s' instead of '%s'",
			strings.Split(agentName, "@")[0], agentName)
	}

	// Validate agent name
	if agentName == "" {
		return "", fmt.Errorf("agent name cannot be empty")
	}

	// Validate agent name format
	if !isValidAgentName(agentName) {
		return "", fmt.Errorf("invalid agent name '%s': only letters, numbers, hyphens, underscores, and dots allowed", agentName)
	}

	// Construct full address with local domain
	fullAddress := fmt.Sprintf("%s@%s", agentName, r.localDomain)
	return fullAddress, nil
}

// isValidAgentName validates that an agent name follows proper naming conventions
func isValidAgentName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}

	// Allow letters, numbers, hyphens, underscores, and dots
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.') {
			return false
		}
	}

	// Cannot start or end with special characters
	if name[0] == '-' || name[0] == '_' || name[0] == '.' ||
		name[len(name)-1] == '-' || name[len(name)-1] == '_' || name[len(name)-1] == '.' {
		return false
	}

	return true
}
