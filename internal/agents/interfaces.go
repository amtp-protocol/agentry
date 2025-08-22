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
	"github.com/amtp-protocol/agentry/internal/types"
)

// AgentRegistry defines the interface for managing local agents
type AgentRegistry interface {
	// Agent management
	RegisterAgent(agent *LocalAgent) error
	UnregisterAgent(agentNameOrAddress string) error
	GetAgent(agentAddress string) (*LocalAgent, error)
	GetAllAgents() map[string]*LocalAgent
	GetSupportedSchemas() []string

	// API key management
	GenerateAPIKey() (string, error)
	VerifyAPIKey(agentAddress, apiKey string) bool
	UpdateLastAccess(agentAddress string)
	RotateAPIKey(agentAddress string) (string, error)

	// Inbox management (for pull-mode agents)
	StoreMessage(recipient string, message *types.Message) error
	GetInboxMessages(recipient string) []*types.Message
	AcknowledgeMessage(recipient, messageID string) error

	// Statistics
	GetStats() map[string]interface{}
}

// Ensure Registry implements AgentRegistry
var _ AgentRegistry = (*Registry)(nil)
