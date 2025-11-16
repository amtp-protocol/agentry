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

package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
)

// MockDeliveryEngine for testing
type MockDeliveryEngine struct {
	deliveryResults map[string]*DeliveryResult
	deliveryError   error
}

func NewMockDeliveryEngine() *MockDeliveryEngine {
	return &MockDeliveryEngine{
		deliveryResults: make(map[string]*DeliveryResult),
	}
}

func (m *MockDeliveryEngine) DeliverMessage(ctx context.Context, message *types.Message, recipient string) (*DeliveryResult, error) {
	if m.deliveryError != nil {
		return nil, m.deliveryError
	}

	if result, exists := m.deliveryResults[recipient]; exists {
		return result, nil
	}

	// Default successful delivery
	return &DeliveryResult{
		Status:     types.StatusDelivered,
		StatusCode: 200,
		Timestamp:  time.Now().UTC(),
		Attempts:   1,
	}, nil
}

func (m *MockDeliveryEngine) SetDeliveryResult(recipient string, result *DeliveryResult) {
	m.deliveryResults[recipient] = result
}

func (m *MockDeliveryEngine) SetDeliveryError(err error) {
	m.deliveryError = err
}

// MockDiscovery for testing
type MockDiscovery struct {
	capabilities map[string]*discovery.AMTPCapabilities
	error        error
}

func NewMockDiscovery() *MockDiscovery {
	return &MockDiscovery{
		capabilities: make(map[string]*discovery.AMTPCapabilities),
	}
}

func (m *MockDiscovery) DiscoverCapabilities(ctx context.Context, domain string) (*discovery.AMTPCapabilities, error) {
	if m.error != nil {
		return nil, m.error
	}

	if cap, exists := m.capabilities[domain]; exists {
		return cap, nil
	}

	// Default capabilities
	return &discovery.AMTPCapabilities{
		Version:      "1.0",
		Gateway:      "https://" + domain,
		MaxSize:      10485760,
		Features:     []string{"immediate-path"},
		DiscoveredAt: time.Now(),
		TTL:          5 * time.Minute,
	}, nil
}

func (m *MockDiscovery) SetCapabilities(domain string, cap *discovery.AMTPCapabilities) {
	m.capabilities[domain] = cap
}

func (m *MockDiscovery) SetError(err error) {
	m.error = err
}

// MockStorage for testing
type MockStorage struct {
	messages map[string]*types.Message
	statuses map[string]*types.MessageStatus
	agents   map[string]*agents.LocalAgent
	mutex    sync.RWMutex
	error    error
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		messages: make(map[string]*types.Message),
		statuses: make(map[string]*types.MessageStatus),
		agents:   make(map[string]*agents.LocalAgent),
	}
}

func (m *MockStorage) StoreMessage(ctx context.Context, message *types.Message) error {
	if m.error != nil {
		return m.error
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.messages[message.MessageID] = message
	return nil
}

func (m *MockStorage) GetMessage(ctx context.Context, messageID string) (*types.Message, error) {
	if m.error != nil {
		return nil, m.error
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if msg, exists := m.messages[messageID]; exists {
		return msg, nil
	}
	return nil, fmt.Errorf("message not found: %s", messageID)
}

func (m *MockStorage) DeleteMessage(ctx context.Context, messageID string) error {
	if m.error != nil {
		return m.error
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.messages, messageID)
	return nil
}

func (m *MockStorage) ListMessages(ctx context.Context, filter storage.MessageFilter) ([]*types.Message, error) {
	if m.error != nil {
		return nil, m.error
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	var results []*types.Message
	for _, msg := range m.messages {
		results = append(results, msg)
	}
	return results, nil
}

func (m *MockStorage) StoreStatus(ctx context.Context, messageID string, status *types.MessageStatus) error {
	if m.error != nil {
		return m.error
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.statuses[messageID] = status
	return nil
}

func (m *MockStorage) GetStatus(ctx context.Context, messageID string) (*types.MessageStatus, error) {
	if m.error != nil {
		return nil, m.error
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if status, exists := m.statuses[messageID]; exists {
		return status, nil
	}
	return nil, fmt.Errorf("message status not found: %s", messageID)
}

func (m *MockStorage) UpdateStatus(ctx context.Context, messageID string, updater storage.StatusUpdater) error {
	if m.error != nil {
		return m.error
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if status, exists := m.statuses[messageID]; exists {
		return updater(status)
	}
	return fmt.Errorf("message status not found: %s", messageID)
}

func (m *MockStorage) DeleteStatus(ctx context.Context, messageID string) error {
	if m.error != nil {
		return m.error
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.statuses, messageID)
	return nil
}

func (m *MockStorage) GetInboxMessages(ctx context.Context, recipient string) ([]*types.Message, error) {
	if m.error != nil {
		return nil, m.error
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var inboxMessages []*types.Message
	for messageID, message := range m.messages {
		status, exists := m.statuses[messageID]
		if !exists {
			continue
		}

		for _, recipientStatus := range status.Recipients {
			if recipientStatus.Address == recipient &&
				recipientStatus.LocalDelivery &&
				recipientStatus.InboxDelivered &&
				!recipientStatus.Acknowledged {
				inboxMessages = append(inboxMessages, message)
				break
			}
		}
	}
	return inboxMessages, nil
}

func (m *MockStorage) AcknowledgeMessage(ctx context.Context, recipient, messageID string) error {
	if m.error != nil {
		return m.error
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()

	status, exists := m.statuses[messageID]
	if !exists {
		return fmt.Errorf("message status not found: %s", messageID)
	}

	for i, recipientStatus := range status.Recipients {
		if recipientStatus.Address == recipient {
			if !recipientStatus.LocalDelivery || !recipientStatus.InboxDelivered {
				return fmt.Errorf("message not available in inbox for recipient: %s", recipient)
			}
			if recipientStatus.Acknowledged {
				return fmt.Errorf("message already acknowledged: %s", messageID)
			}

			now := time.Now().UTC()
			status.Recipients[i].Acknowledged = true
			status.Recipients[i].AcknowledgedAt = &now
			status.UpdatedAt = now
			return nil
		}
	}
	return fmt.Errorf("recipient not found for message: %s", recipient)
}

func (m *MockStorage) Close() error {
	return nil
}

func (m *MockStorage) HealthCheck(ctx context.Context) error {
	return m.error
}

func (m *MockStorage) GetStats(ctx context.Context) (storage.StorageStats, error) {
	if m.error != nil {
		return storage.StorageStats{}, m.error
	}
	return storage.StorageStats{
		TotalMessages: int64(len(m.messages)),
		TotalStatuses: int64(len(m.statuses)),
	}, nil
}

func (m *MockStorage) SetError(err error) {
	m.error = err
}

func (m *MockStorage) CreateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	m.agents[agent.Address] = agent
	return nil
}

func (m *MockStorage) GetAgent(ctx context.Context, agentAddress string) (*agents.LocalAgent, error) {
	agent, exists := m.agents[agentAddress]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentAddress)
	}

	return agent, nil
}

func (m *MockStorage) UpdateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}
	if _, exists := m.agents[agent.Address]; !exists {
		return fmt.Errorf("agent not found: %s", agent.Address)
	}

	agentCopy := *agent
	m.agents[agent.Address] = &agentCopy
	return nil
}

func (m *MockStorage) DeleteAgent(ctx context.Context, agentAddress string) error {
	if _, exists := m.agents[agentAddress]; !exists {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}

	delete(m.agents, agentAddress)
	return nil
}

func (m *MockStorage) ListAgents(ctx context.Context) ([]*agents.LocalAgent, error) {
	var list []*agents.LocalAgent
	for _, agent := range m.agents {
		agentCopy := *agent
		list = append(list, &agentCopy)
	}
	return list, nil
}

func (m *MockStorage) GetSupportedSchemas(ctx context.Context) ([]string, error) {
	schemaSet := make(map[string]struct{})
	for _, agent := range m.agents {
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

func (m *MockDiscovery) SupportsSchema(ctx context.Context, domain, schema string) (bool, error) {
	if m.error != nil {
		return false, m.error
	}
	// Default to supporting all schemas
	return true, nil
}

// Test message creation helper
func createTestMessage() *types.Message {
	return &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@test.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"message": "Hello, World!"}`),
	}
}
