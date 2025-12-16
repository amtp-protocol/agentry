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

	"github.com/amtp-protocol/agentry/internal/types"
)

// AgentStore defines the storage operations required by the agent registry
type AgentStore interface {
	CreateAgent(ctx context.Context, agent *LocalAgent) error
	DeleteAgent(ctx context.Context, agentAddress string) error
	GetAgent(ctx context.Context, agentAddress string) (*LocalAgent, error)
	UpdateAgent(ctx context.Context, agent *LocalAgent) error
	ListAgents(ctx context.Context) ([]*LocalAgent, error)
	GetSupportedSchemas(ctx context.Context) ([]string, error)
}

// AgentRegistry defines the interface for managing local agents
type AgentRegistry interface {
	// Agent management
	RegisterAgent(ctx context.Context, agent *LocalAgent) error
	UnregisterAgent(ctx context.Context, agentNameOrAddress string) error
	GetAgent(ctx context.Context, agentAddress string) (*LocalAgent, error)
	GetAllAgents(ctx context.Context) map[string]*LocalAgent
	GetSupportedSchemas(ctx context.Context) []string

	// API key management
	GenerateAPIKey() (string, error)
	VerifyAPIKey(ctx context.Context, agentAddress, apiKey string) bool
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
