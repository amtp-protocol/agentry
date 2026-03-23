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

package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/types"
)

// MemoryStorage implements Storage using in-memory maps
type MemoryStorage struct {
	config      MemoryStorageConfig
	messages    map[string]*types.Message
	statuses    map[string]*types.MessageStatus
	agents      map[string]*agents.LocalAgent
	messagesMux sync.RWMutex
	statusesMux sync.RWMutex
	workflows   map[string]*types.Workflow
	workflowsMux sync.RWMutex
	agentsMux   sync.RWMutex
	createdAt   time.Time
}

// NewMemoryStorage creates a new in-memory storage instance
func NewMemoryStorage(config MemoryStorageConfig) *MemoryStorage {
	return &MemoryStorage{
		config:    config,
		messages:  make(map[string]*types.Message),
		statuses:  make(map[string]*types.MessageStatus),
		workflows:  make(map[string]*types.Workflow),
		agents:    make(map[string]*agents.LocalAgent),
		createdAt: time.Now().UTC(),
	}
}

// StoreMessage stores a message in memory
func (ms *MemoryStorage) StoreMessage(ctx context.Context, message *types.Message) error {
	if message == nil {
		return fmt.Errorf("message cannot be nil")
	}
	if message.MessageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	ms.messagesMux.Lock()
	defer ms.messagesMux.Unlock()

	// Check capacity limits if configured
	if ms.config.MaxMessages > 0 && len(ms.messages) >= ms.config.MaxMessages {
		return fmt.Errorf("storage capacity exceeded: max %d messages", ms.config.MaxMessages)
	}

	ms.messages[message.MessageID] = cloneMessage(message)
	return nil
}

// GetMessage retrieves a message by ID
func (ms *MemoryStorage) GetMessage(ctx context.Context, messageID string) (*types.Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID cannot be empty")
	}

	ms.messagesMux.RLock()
	defer ms.messagesMux.RUnlock()

	message, exists := ms.messages[messageID]
	if !exists {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}

	return cloneMessage(message), nil
}

// DeleteMessage removes a message from storage
func (ms *MemoryStorage) DeleteMessage(ctx context.Context, messageID string) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	ms.messagesMux.Lock()
	defer ms.messagesMux.Unlock()

	if _, exists := ms.messages[messageID]; !exists {
		return fmt.Errorf("message not found: %s", messageID)
	}

	delete(ms.messages, messageID)
	return nil
}

// ListMessages returns messages matching the filter criteria
func (ms *MemoryStorage) ListMessages(ctx context.Context, filter MessageFilter) ([]*types.Message, error) {
	ms.messagesMux.RLock()
	ms.statusesMux.RLock()
	defer ms.messagesMux.RUnlock()
	defer ms.statusesMux.RUnlock()

	// Collect all matching messages first, then apply ordering and pagination.
	// Applying offset/limit during the raw map iteration is wrong because the
	// offset would be consumed by non-matching messages, and map iteration
	// order is non-deterministic.
	var matched []*types.Message
	for messageID, message := range ms.messages {
		if ms.matchesFilter(message, messageID, filter) {
			matched = append(matched, cloneMessage(message))
		}
	}

	// Order newest-first to mirror the database backend (ORDER BY created_at
	// DESC) and to make pagination deterministic. Ties are broken by message
	// ID so the ordering is total.
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Timestamp.Equal(matched[j].Timestamp) {
			return matched[i].MessageID > matched[j].MessageID
		}
		return matched[i].Timestamp.After(matched[j].Timestamp)
	})

	// Apply offset.
	if filter.Offset > 0 {
		if filter.Offset >= len(matched) {
			return []*types.Message{}, nil
		}
		matched = matched[filter.Offset:]
	}

	// Apply limit.
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}

	return matched, nil
}

// StoreStatus stores message status
func (ms *MemoryStorage) StoreStatus(ctx context.Context, messageID string, status *types.MessageStatus) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}
	if status == nil {
		return fmt.Errorf("status cannot be nil")
	}

	ms.statusesMux.Lock()
	defer ms.statusesMux.Unlock()

	ms.statuses[messageID] = cloneStatus(status)
	return nil
}

// GetStatus retrieves message status by ID
func (ms *MemoryStorage) GetStatus(ctx context.Context, messageID string) (*types.MessageStatus, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID cannot be empty")
	}

	ms.statusesMux.RLock()
	defer ms.statusesMux.RUnlock()

	status, exists := ms.statuses[messageID]
	if !exists {
		return nil, fmt.Errorf("message status not found: %s", messageID)
	}

	return cloneStatus(status), nil
}

// UpdateStatus updates message status using the provided updater function
func (ms *MemoryStorage) UpdateStatus(ctx context.Context, messageID string, updater StatusUpdater) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}
	if updater == nil {
		return fmt.Errorf("updater function cannot be nil")
	}

	ms.statusesMux.Lock()
	defer ms.statusesMux.Unlock()

	status, exists := ms.statuses[messageID]
	if !exists {
		return fmt.Errorf("message status not found: %s", messageID)
	}

	return updater(status)
}

// DeleteStatus removes message status from storage
func (ms *MemoryStorage) DeleteStatus(ctx context.Context, messageID string) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	ms.statusesMux.Lock()
	defer ms.statusesMux.Unlock()

	if _, exists := ms.statuses[messageID]; !exists {
		return fmt.Errorf("message status not found: %s", messageID)
	}

	delete(ms.statuses, messageID)
	return nil
}

// GetInboxMessages returns messages for a specific recipient using unified storage view
func (ms *MemoryStorage) GetInboxMessages(ctx context.Context, recipient string) ([]*types.Message, error) {
	if recipient == "" {
		return nil, fmt.Errorf("recipient cannot be empty")
	}

	ms.messagesMux.RLock()
	ms.statusesMux.RLock()
	defer ms.messagesMux.RUnlock()
	defer ms.statusesMux.RUnlock()

	var inboxMessages []*types.Message

	// Iterate through all messages and find those delivered to this recipient's inbox
	for messageID, message := range ms.messages {
		status, exists := ms.statuses[messageID]
		if !exists {
			continue
		}

		// Check if this message has been delivered to the recipient's inbox
		for _, recipientStatus := range status.Recipients {
			if recipientStatus.Address == recipient &&
				recipientStatus.LocalDelivery &&
				recipientStatus.InboxDelivered &&
				!recipientStatus.Acknowledged {
				inboxMessages = append(inboxMessages, cloneMessage(message))
				break
			}
		}
	}

	return inboxMessages, nil
}

// AcknowledgeMessage marks a message as acknowledged for a specific recipient
func (ms *MemoryStorage) AcknowledgeMessage(ctx context.Context, recipient, messageID string) error {
	if recipient == "" {
		return fmt.Errorf("recipient cannot be empty")
	}
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	ms.statusesMux.Lock()
	defer ms.statusesMux.Unlock()

	status, exists := ms.statuses[messageID]
	if !exists {
		return fmt.Errorf("message not found: %s", messageID)
	}

	// Find and acknowledge the recipient
	for i, recipientStatus := range status.Recipients {
		if recipientStatus.Address == recipient {
			if !recipientStatus.LocalDelivery || !recipientStatus.InboxDelivered {
				return fmt.Errorf("message not available in inbox for recipient: %s", recipient)
			}
			if recipientStatus.Acknowledged {
				return fmt.Errorf("message already acknowledged: %s", messageID)
			}

			// Mark as acknowledged
			now := time.Now().UTC()
			status.Recipients[i].Acknowledged = true
			status.Recipients[i].AcknowledgedAt = &now
			status.UpdatedAt = now

			return nil
		}
	}

	return fmt.Errorf("recipient not found for message: %s", recipient)
}

// Close closes the storage (no-op for memory storage)
func (ms *MemoryStorage) Close() error {
	// No resources to clean up for memory storage
	return nil
}

// HealthCheck performs a health check on the storage
func (ms *MemoryStorage) HealthCheck(ctx context.Context) error {
	// Memory storage is always healthy if the struct exists
	return nil
}

// GetStats returns storage statistics
func (ms *MemoryStorage) GetStats(ctx context.Context) (StorageStats, error) {
	ms.messagesMux.RLock()
	ms.statusesMux.RLock()
	defer ms.messagesMux.RUnlock()
	defer ms.statusesMux.RUnlock()

	stats := StorageStats{
		TotalMessages: int64(len(ms.messages)),
		TotalStatuses: int64(len(ms.statuses)),
	}

	// Count messages by status
	for _, status := range ms.statuses {
		switch status.Status {
		case types.StatusPending, types.StatusQueued, types.StatusDelivering:
			stats.PendingMessages++
		case types.StatusDelivered:
			stats.DeliveredMessages++
		case types.StatusFailed:
			stats.FailedMessages++
		}

		// Count inbox and acknowledged messages
		for _, recipientStatus := range status.Recipients {
			if recipientStatus.LocalDelivery && recipientStatus.InboxDelivered {
				if recipientStatus.Acknowledged {
					stats.AcknowledgedMessages++
				} else {
					stats.InboxMessages++
				}
			}
		}
	}

	return stats, nil
}

// matchesFilter checks if a message matches the given filter criteria
func (ms *MemoryStorage) matchesFilter(message *types.Message, messageID string, filter MessageFilter) bool {
	// Check sender filter
	if filter.Sender != "" && message.Sender != filter.Sender {
		return false
	}

	// Check recipients filter
	if len(filter.Recipients) > 0 {
		found := false
		for _, filterRecipient := range filter.Recipients {
			for _, messageRecipient := range message.Recipients {
				if messageRecipient == filterRecipient {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check status filter
	if filter.Status != "" {
		status, exists := ms.statuses[messageID]
		if !exists || status.Status != filter.Status {
			return false
		}
	}

	// Check since filter
	if filter.Since != nil {
		if message.Timestamp.Unix() < *filter.Since {
			return false
		}
	}

	return true
}

// CreateAgent creates a new local agent
func (ms *MemoryStorage) CreateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}
	ms.agentsMux.Lock()
	defer ms.agentsMux.Unlock()

	if _, exists := ms.agents[agent.Address]; exists {
		return fmt.Errorf("agent already exists: %s", agent.Address)
	}

	// Store a copy to prevent external modifications (like API key restoration) from affecting storage
	ms.agents[agent.Address] = cloneAgent(agent)
	return nil
}

// GetAgent retrieves a local agent by address
func (ms *MemoryStorage) GetAgent(ctx context.Context, agentAddress string) (*agents.LocalAgent, error) {
	if agentAddress == "" {
		return nil, fmt.Errorf("agent address cannot be empty")
	}

	ms.agentsMux.RLock()
	defer ms.agentsMux.RUnlock()

	agent, exists := ms.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}

	return cloneAgent(agent), nil
}

// UpdateAgent updates an existing local agent
func (ms *MemoryStorage) UpdateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}
	ms.agentsMux.Lock()
	defer ms.agentsMux.Unlock()

	if _, exists := ms.agents[agent.Address]; !exists {
		return fmt.Errorf("agent not found: %s", agent.Address)
	}

	// Store a copy to prevent external modifications from affecting storage
	ms.agents[agent.Address] = cloneAgent(agent)
	return nil
}

// DeleteAgent removes a local agent from storage
func (ms *MemoryStorage) DeleteAgent(ctx context.Context, agentAddress string) error {
	if agentAddress == "" {
		return fmt.Errorf("agent address cannot be empty")
	}

	ms.agentsMux.Lock()
	defer ms.agentsMux.Unlock()

	if _, exists := ms.agents[agentAddress]; !exists {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}

	delete(ms.agents, agentAddress)
	return nil
}

// ListAgents returns all local agents
func (ms *MemoryStorage) ListAgents(ctx context.Context) ([]*agents.LocalAgent, error) {
	ms.agentsMux.RLock()
	defer ms.agentsMux.RUnlock()

	var agentList []*agents.LocalAgent
	for _, agent := range ms.agents {
		agentList = append(agentList, cloneAgent(agent))
	}

	return agentList, nil
}

// GetSupportedSchemas returns all supported schemas across local agents
func (ms *MemoryStorage) GetSupportedSchemas(ctx context.Context) ([]string, error) {
	ms.agentsMux.RLock()
	defer ms.agentsMux.RUnlock()

	schemaSet := make(map[string]struct{})
	for _, agent := range ms.agents {
		for _, schemaID := range agent.SupportedSchemas {
			schemaSet[schemaID] = struct{}{}
		}
	}

	var schemas []string
	for schemaID := range schemaSet {
		schemas = append(schemas, schemaID)
	}

	return schemas, nil
}

// The clone helpers below give the in-memory store sole ownership of the values
// it holds. Every value is copied on the way in (Store/Create/Update) and on
// the way out (Get/List), so neither the caller's original nor a returned value
// shares mutable state (slices, maps, payload bytes, pointer fields) with the
// stored object. Without this, a caller mutating a returned value would race
// with concurrent readers/writers and silently corrupt stored state. This
// mirrors the database backend, which returns freshly converted structs.

func cloneMessage(m *types.Message) *types.Message {
	if m == nil {
		return nil
	}
	c := *m
	if m.Recipients != nil {
		c.Recipients = append([]string(nil), m.Recipients...)
	}
	if m.Headers != nil {
		c.Headers = make(map[string]interface{}, len(m.Headers))
		for k, v := range m.Headers {
			c.Headers[k] = v
		}
	}
	if m.Payload != nil {
		c.Payload = append([]byte(nil), m.Payload...)
	}
	if m.Attachments != nil {
		c.Attachments = append([]types.Attachment(nil), m.Attachments...)
	}
	if m.Coordination != nil {
		coord := *m.Coordination
		c.Coordination = &coord
	}
	if m.Signature != nil {
		sig := *m.Signature
		c.Signature = &sig
	}
	return &c
}

func cloneStatus(s *types.MessageStatus) *types.MessageStatus {
	if s == nil {
		return nil
	}
	c := *s
	if s.Recipients != nil {
		c.Recipients = append([]types.RecipientStatus(nil), s.Recipients...)
	}
	if s.NextRetry != nil {
		t := *s.NextRetry
		c.NextRetry = &t
	}
	if s.DeliveredAt != nil {
		t := *s.DeliveredAt
		c.DeliveredAt = &t
	}
	return &c
}

func cloneAgent(a *agents.LocalAgent) *agents.LocalAgent {
	if a == nil {
		return nil
	}
	c := *a
	if a.Headers != nil {
		c.Headers = make(map[string]string, len(a.Headers))
		for k, v := range a.Headers {
			c.Headers[k] = v
		}
	}
	if a.SupportedSchemas != nil {
		c.SupportedSchemas = append([]string(nil), a.SupportedSchemas...)
	}
	return &c
}
