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
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
)

// ErrInvalidDeliveryConfig is wrapped by ValidateDeliveryConfig errors so
// callers (e.g. the server layer mapping failures to HTTP responses) can
// distinguish a rejected delivery configuration — a client-side 400 — from
// a genuine storage failure — a 5xx — even when the validation runs inside
// the storage backend's atomic update.
var ErrInvalidDeliveryConfig = errors.New("invalid delivery configuration")

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

	// lastAccessMu guards lastAccessAt, which debounces last_access writes
	// so a burst of read requests does not issue one storage UPDATE each.
	lastAccessMu sync.Mutex
	lastAccessAt map[string]time.Time
}

// lastAccessWriteDebounce is the minimum interval between two last_access
// writes for the same agent. The timestamp only feeds the discovery
// "active_only" filter (which uses day-scale thresholds), so sub-minute
// staleness is immaterial.
const lastAccessWriteDebounce = time.Minute

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
		lastAccessAt:  make(map[string]time.Time),
	}
}

// schemaManagerAvailable reports whether the schema manager is usable,
// guarding against "typed nil" interfaces (a nil *schema.Manager stored in
// the SchemaManager interface, which is non-nil as an interface value but
// panics when any method is invoked).
func (r *Registry) schemaManagerAvailable() bool {
	if r.schemaManager == nil {
		return false
	}
	if sm, ok := r.schemaManager.(*schema.Manager); ok {
		return sm != nil
	}
	return true
}

// ValidateDeliveryConfig enforces the delivery-mode invariants shared by
// registration and field-level updates: the mode must be 'push' or 'pull',
// and push mode requires a non-empty push target. Storage backends call it on
// the merged state inside their atomic update, so two concurrent field-level
// updates cannot jointly store an invalid combination even though each passes
// validation against the record it read.
func ValidateDeliveryConfig(deliveryMode, pushTarget string) error {
	if deliveryMode != "push" && deliveryMode != "pull" {
		return fmt.Errorf("%w: delivery mode must be 'push' or 'pull'", ErrInvalidDeliveryConfig)
	}
	if deliveryMode == "push" && pushTarget == "" {
		return fmt.Errorf("%w: push target URL is required for push delivery mode", ErrInvalidDeliveryConfig)
	}
	return nil
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

	if err := ValidateDeliveryConfig(agent.DeliveryMode, agent.PushTarget); err != nil {
		return err
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
	fullAddress, err := r.resolveAgentAddress(agentNameOrAddress)
	if err != nil {
		return err
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

// UpdateAgent updates mutable delivery configuration for an existing agent.
// Supported fields: delivery mode, push target, push headers, and supported
// schemas. The API key is preserved.
//
// The update is applied as a field-level storage write so a concurrent key
// rotation (which only touches the API key hash) is never clobbered by a
// full-record read-modify-write.
func (r *Registry) UpdateAgent(ctx context.Context, agentNameOrAddress string, updates *AgentUpdate) (*LocalAgent, error) {
	fullAddress, err := r.resolveAgentAddress(agentNameOrAddress)
	if err != nil {
		return nil, err
	}

	agent, err := r.getAgentInternal(ctx, fullAddress)
	if err != nil {
		return nil, err
	}

	// Validate against the merged state (current values overridden by the
	// requested updates) before writing anything. This is a fast-fail check;
	// the storage layer re-validates the merged state atomically inside its
	// update, because the record read here may be stale by the time the write
	// lands when two updates race.
	deliveryMode := agent.DeliveryMode
	if updates.DeliveryMode != nil {
		deliveryMode = *updates.DeliveryMode
	}
	pushTarget := agent.PushTarget
	if updates.PushTarget != nil {
		pushTarget = *updates.PushTarget
	}
	if err := ValidateDeliveryConfig(deliveryMode, pushTarget); err != nil {
		return nil, err
	}
	if updates.SupportedSchemas != nil {
		if err := r.validateSupportedSchemas(ctx, updates.SupportedSchemas); err != nil {
			return nil, fmt.Errorf("invalid supported schemas: %w", err)
		}
	}

	fields := AgentFields{
		DeliveryMode:     updates.DeliveryMode,
		PushTarget:       updates.PushTarget,
		PushHeaders:      updates.PushHeaders,
		SupportedSchemas: updates.SupportedSchemas,
	}
	if err := r.storage.UpdateAgentFields(ctx, fullAddress, fields); err != nil {
		return nil, fmt.Errorf("failed to update agent: %w", err)
	}

	// Return a copy of the freshly stored record with the API key redacted.
	updated, err := r.getAgentInternal(ctx, fullAddress)
	if err != nil {
		return nil, err
	}
	result := *updated
	result.APIKey = ""
	return &result, nil
}

// AgentUpdate contains optional fields to update on an agent.
type AgentUpdate struct {
	DeliveryMode     *string           `json:"delivery_mode,omitempty"`
	PushTarget       *string           `json:"push_target,omitempty"`
	PushHeaders      map[string]string `json:"push_headers,omitempty"`
	SupportedSchemas []string          `json:"supported_schemas,omitempty"`
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

// AuthenticateAgent returns the full address of the registered agent that
// owns the given API key, or ok=false if none does. The presented key is
// hashed once and compared against a single listing of agents — the stored
// API key is already the salted hash — so authentication costs one listing
// plus a constant-time comparison per agent instead of a per-agent storage
// read and a redundant hash per iteration.
func (r *Registry) AuthenticateAgent(ctx context.Context, apiKey string) (string, bool) {
	if apiKey == "" {
		return "", false
	}

	hashed := r.hashAPIKey(apiKey)
	agents, err := r.storage.ListAgents(ctx)
	if err != nil {
		return "", false
	}

	for _, agent := range agents {
		if agent == nil {
			continue
		}
		// Use constant-time comparison to prevent timing attacks.
		if subtle.ConstantTimeCompare([]byte(agent.APIKey), []byte(hashed)) == 1 {
			return agent.Address, true
		}
	}
	return "", false
}

// UpdateLastAccess updates the last access timestamp for an agent using a
// field-level write so it cannot clobber a concurrent key rotation. Writes
// are debounced to at most one per debounce window per agent, so a burst of
// read requests does not issue one storage UPDATE each.
func (r *Registry) UpdateLastAccess(ctx context.Context, agentAddress string) {
	now := time.Now().UTC()

	r.lastAccessMu.Lock()
	if last, ok := r.lastAccessAt[agentAddress]; ok && now.Sub(last) < lastAccessWriteDebounce {
		r.lastAccessMu.Unlock()
		return
	}
	r.lastAccessAt[agentAddress] = now
	r.lastAccessMu.Unlock()

	if err := r.storage.UpdateAgentFields(ctx, agentAddress, AgentFields{LastAccess: &now}); err != nil {
		return
	}
}

// RotateAPIKey generates a new API key for an existing agent. Only the API
// key hash is written, so a concurrent agent update is never clobbered.
//
// No pre-check read is performed: the storage layer reports missing agents
// and propagates underlying storage errors through UpdateAgentFields, so a
// transient storage outage surfaces as such instead of being masked as
// "agent not found".
func (r *Registry) RotateAPIKey(ctx context.Context, agentAddress string) (string, error) {
	// Generate new API key
	newAPIKey, err := r.GenerateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate new API key: %w", err)
	}

	// Update only the key hash; other fields (and concurrent updates) are
	// left untouched.
	hashed := r.hashAPIKey(newAPIKey)
	if err := r.storage.UpdateAgentFields(ctx, agentAddress, AgentFields{APIKey: &hashed}); err != nil {
		return "", fmt.Errorf("failed to rotate agent API key: %w", err)
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
		if !strings.HasSuffix(schemaStr, "*") && r.schemaManagerAvailable() {
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

// schemaExactRegex matches an exact (non-wildcard) AGNTCY schema identifier:
// agntcy:domain.entity.version
var schemaExactRegex = regexp.MustCompile(`^agntcy:[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.v[0-9]+$`)

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
		if !schemaExactRegex.MatchString(schemaStr) {
			return fmt.Errorf("schema must match format agntcy:domain.entity.version")
		}
	}

	return nil
}

// resolveAgentAddress accepts either a bare agent name or a full address
// matching the local domain, and returns the normalized full address.
// Registration still requires a bare name via normalizeAgentAddress; this
// helper is used by update/unregister operations that may receive the full
// address from API clients. Domain labels are case-insensitive (RFC 1035),
// so a correctly spelled local address in the wrong case resolves like its
// lowercase form.
func (r *Registry) resolveAgentAddress(nameOrAddress string) (string, error) {
	if strings.Contains(nameOrAddress, "@") {
		parts := strings.SplitN(nameOrAddress, "@", 2)
		if !strings.EqualFold(parts[1], r.localDomain) {
			return "", fmt.Errorf("agent address domain %q does not match local domain %q", parts[1], r.localDomain)
		}
		nameOrAddress = parts[0]
	}
	return r.normalizeAgentAddress(nameOrAddress)
}

// ResolveAgentAddress accepts either a bare agent name or a full address
// matching the local domain and returns the normalized full address. It
// wraps resolveAgentAddress so callers outside the package (e.g. the server
// layer normalizing filter parameters) share the same resolution semantics.
func (r *Registry) ResolveAgentAddress(nameOrAddress string) (string, error) {
	return r.resolveAgentAddress(nameOrAddress)
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
