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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
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
	storage       AgentStore
	apiKeySalt    string
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
	APIKeySalt    string
}

// NewRegistry creates a new agent registry
func NewRegistry(config RegistryConfig, storage AgentStore) *Registry {
	return &Registry{
		localDomain:   config.LocalDomain,
		schemaManager: config.SchemaManager,
		storage:       storage,
		apiKeySalt:    config.APIKeySalt,
	}
}

// RegisterAgent registers a local agent with delivery configuration
func (r *Registry) RegisterAgent(ctx context.Context, agent *LocalAgent) error {
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
	plainAPIKey := agent.APIKey
	if plainAPIKey == "" {
		apiKey, err := r.GenerateAPIKey()
		if err != nil {
			return fmt.Errorf("failed to generate API key: %w", err)
		}
		plainAPIKey = apiKey
	}

	// Store hash
	agent.APIKey = r.hashAPIKey(plainAPIKey)

	// Set timestamps
	now := time.Now().UTC()
	agent.CreatedAt = now
	agent.LastAccess = now

	err = r.storage.CreateAgent(ctx, agent)

	// Restore plain key for the caller
	agent.APIKey = plainAPIKey

	if err != nil {
		return fmt.Errorf("failed to register agent: %w", err)
	}
	return nil
}

// UnregisterAgent removes a local agent
func (r *Registry) UnregisterAgent(ctx context.Context, agentNameOrAddress string) error {
	// Normalize the input to full address
	fullAddress, err := r.normalizeAgentAddress(agentNameOrAddress)
	if err != nil {
		return fmt.Errorf("invalid agent identifier: %w", err)
	}

	err = r.storage.DeleteAgent(ctx, fullAddress)
	if err != nil {
		return fmt.Errorf("failed to unregister agent: %w", err)
	}
	return nil
}

// GetAgent returns a specific agent by address
// Note: API Key is redacted for security
func (r *Registry) GetAgent(ctx context.Context, agentAddress string) (*LocalAgent, error) {
	agent, err := r.getAgentInternal(ctx, agentAddress)
	if err != nil {
		return nil, err
	}

	// Return a copy to avoid race conditions and redact sensitive info
	agentCopy := *agent
	agentCopy.APIKey = "" // Redact API key
	return &agentCopy, nil
}

// getAgentInternal returns the raw agent data including hashed API key
func (r *Registry) getAgentInternal(ctx context.Context, agentAddress string) (*LocalAgent, error) {
	agent, err := r.storage.GetAgent(ctx, agentAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}
	return agent, nil
}

// GetAllAgents returns all registered local agents
func (r *Registry) GetAllAgents(ctx context.Context) map[string]*LocalAgent {
	result := make(map[string]*LocalAgent)
	agents, err := r.storage.ListAgents(ctx)
	if err != nil {
		return result
	}

	for _, agent := range agents {
		if agent == nil {
			continue
		}
		agentCopy := *agent
		agentCopy.APIKey = "" // Redact API key
		result[agentCopy.Address] = &agentCopy
	}

	return result
}

// GetSupportedSchemas returns all schemas supported by registered agents
func (r *Registry) GetSupportedSchemas(ctx context.Context) []string {
	schemas, err := r.storage.GetSupportedSchemas(ctx)
	if err != nil {
		return []string{}
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
func (r *Registry) VerifyAPIKey(ctx context.Context, agentAddress, apiKey string) bool {
	agent, err := r.getAgentInternal(ctx, agentAddress)
	if err != nil || agent == nil {
		return false
	}

	hashedInput := r.hashAPIKey(apiKey)

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(agent.APIKey), []byte(hashedInput)) == 1
}

// UpdateLastAccess updates the last access timestamp for an agent
func (r *Registry) UpdateLastAccess(ctx context.Context, agentAddress string) {
	agent, err := r.GetAgent(ctx, agentAddress)
	if err != nil || agent == nil {
		return
	}

	agent.LastAccess = time.Now().UTC()
	err = r.storage.UpdateAgent(ctx, agent)
	if err != nil {
		return
	}
}

// RotateAPIKey generates a new API key for an existing agent
func (r *Registry) RotateAPIKey(ctx context.Context, agentAddress string) (string, error) {
	agent, err := r.GetAgent(ctx, agentAddress)
	if err != nil || agent == nil {
		return "", fmt.Errorf("agent not found: %s", agentAddress)
	}

	// Generate new API key
	newAPIKey, err := r.GenerateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate new API key: %w", err)
	}

	// Update agent with new key
	agent.APIKey = r.hashAPIKey(newAPIKey)
	err = r.storage.UpdateAgent(ctx, agent)
	if err != nil {
		return "", fmt.Errorf("failed to update agent with new API key: %w", err)
	}

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
	agents, err := r.storage.ListAgents(context.Background())
	if err != nil {
		return map[string]interface{}{
			"local_agents": 0,
			"push_agents":  0,
			"pull_agents":  0,
		}
	}

	totalAgents := len(agents)
	pushAgents := 0
	pullAgents := 0

	for _, agent := range agents {
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
		if (char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') &&
			char != '-' && char != '_' && char != '.' {
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

// hashAPIKey creates a SHA256 hash of the API key
func (r *Registry) hashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key + r.apiKeySalt))
	return hex.EncodeToString(hash[:])
}
