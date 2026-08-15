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
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// AgentStore defines the storage operations required by the agent registry
type AgentStore interface {
	CreateAgent(ctx context.Context, agent *LocalAgent) error
	DeleteAgent(ctx context.Context, agentAddress string) error
	GetAgent(ctx context.Context, agentAddress string) (*LocalAgent, error)
	UpdateAgent(ctx context.Context, agent *LocalAgent) error
	// UpdateAgentFields updates only the specified fields of an agent,
	// leaving everything else (notably the API key hash) untouched. This
	// avoids read-modify-write races where a full-record update could clobber
	// a concurrent key rotation. When SupportedSchemas is provided,
	// RequiresSchema is derived as len(SupportedSchemas) > 0.
	UpdateAgentFields(ctx context.Context, agentAddress string, fields AgentFields) error
	ListAgents(ctx context.Context) ([]*LocalAgent, error)
	GetSupportedSchemas(ctx context.Context) ([]string, error)
}

// AgentFields contains the optional fields for a field-level agent update.
// Nil fields are left unchanged. APIKey must already be hashed.
type AgentFields struct {
	DeliveryMode     *string
	PushTarget       *string
	PushHeaders      map[string]string
	SupportedSchemas []string
	LastAccess       *time.Time
	APIKey           *string
}

// AgentRegistry defines the interface for managing local agents
type AgentRegistry interface {
	// Agent management
	RegisterAgent(ctx context.Context, agent *LocalAgent) error
	UnregisterAgent(ctx context.Context, agentNameOrAddress string) error
	GetAgent(ctx context.Context, agentAddress string) (*LocalAgent, error)
	GetAllAgents(ctx context.Context) map[string]*LocalAgent
	GetSupportedSchemas(ctx context.Context) []string
	UpdateAgent(ctx context.Context, agentNameOrAddress string, updates *AgentUpdate) (*LocalAgent, error)
	// ResolveAgentAddress accepts either a bare agent name or a full address
	// matching the local domain and returns the normalized full address. It
	// fails when the input is not a valid agent name or references a foreign
	// domain.
	ResolveAgentAddress(nameOrAddress string) (string, error)

	// API key management
	GenerateAPIKey() (string, error)
	VerifyAPIKey(ctx context.Context, agentAddress, apiKey string) bool
	// AuthenticateAgent returns the address of the registered agent that owns
	// the given API key (ok=false if none does). It hashes the key once and
	// compares against a single listing, avoiding a per-agent storage read
	// for every attempt.
	AuthenticateAgent(ctx context.Context, apiKey string) (string, bool)
	UpdateLastAccess(ctx context.Context, agentAddress string)
	RotateAPIKey(ctx context.Context, agentAddress string) (string, error)

	// Inbox management (for pull-mode agents)
	StoreMessage(recipient string, message *types.Message) error
	GetInboxMessages(recipient string) []*types.Message
	AcknowledgeMessage(recipient, messageID string) error

	// Statistics
	GetStats() map[string]interface{}
}

// Ensure Registry implements AgentRegistry
var _ AgentRegistry = (*Registry)(nil)
