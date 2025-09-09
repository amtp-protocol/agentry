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
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// MemoryStorage implements MessageStorage using in-memory maps
type MemoryStorage struct {
	config      MemoryStorageConfig
	messages    map[string]*types.Message
	statuses    map[string]*types.MessageStatus
	messagesMux sync.RWMutex
	statusesMux sync.RWMutex
	createdAt   time.Time
}

// NewMemoryStorage creates a new in-memory storage instance
func NewMemoryStorage(config MemoryStorageConfig) *MemoryStorage {
	return &MemoryStorage{
		config:    config,
		messages:  make(map[string]*types.Message),
		statuses:  make(map[string]*types.MessageStatus),
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

	ms.messages[message.MessageID] = message
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

	return message, nil
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

	var results []*types.Message
	count := 0

	for messageID, message := range ms.messages {
		// Skip offset
		if count < filter.Offset {
			count++
			continue
		}

		// Check limit
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}

		// Apply filters
		if ms.matchesFilter(message, messageID, filter) {
			results = append(results, message)
		}
		count++
	}

	return results, nil
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

	ms.statuses[messageID] = status
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

	return status, nil
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
				inboxMessages = append(inboxMessages, message)
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
