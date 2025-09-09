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
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestNewMemoryStorage(t *testing.T) {
	config := MemoryStorageConfig{
		MaxMessages: 1000,
		TTL:         24,
	}

	storage := NewMemoryStorage(config)

	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	if storage.config.MaxMessages != 1000 {
		t.Errorf("Expected MaxMessages to be 1000, got %d", storage.config.MaxMessages)
	}

	if storage.config.TTL != 24 {
		t.Errorf("Expected TTL to be 24, got %d", storage.config.TTL)
	}

	if storage.messages == nil {
		t.Error("Expected messages map to be initialized")
	}

	if storage.statuses == nil {
		t.Error("Expected statuses map to be initialized")
	}
}

func TestMemoryStorage_StoreMessage(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	message := &types.Message{
		MessageID:  "test-message-1",
		Sender:     "sender@example.com",
		Recipients: []string{"recipient@example.com"},
		Subject:    "Test Message",
		Timestamp:  time.Now().UTC(),
	}

	err := storage.StoreMessage(ctx, message)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify message was stored
	storedMessage, err := storage.GetMessage(ctx, "test-message-1")
	if err != nil {
		t.Fatalf("Expected no error retrieving message, got %v", err)
	}

	if storedMessage.MessageID != message.MessageID {
		t.Errorf("Expected MessageID %s, got %s", message.MessageID, storedMessage.MessageID)
	}

	if storedMessage.Sender != message.Sender {
		t.Errorf("Expected Sender %s, got %s", message.Sender, storedMessage.Sender)
	}
}

func TestMemoryStorage_StoreMessage_NilMessage(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.StoreMessage(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil message")
	}

	if err.Error() != "message cannot be nil" {
		t.Errorf("Expected 'message cannot be nil', got %s", err.Error())
	}
}

func TestMemoryStorage_StoreMessage_EmptyMessageID(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	message := &types.Message{
		MessageID: "",
		Sender:    "sender@example.com",
	}

	err := storage.StoreMessage(ctx, message)
	if err == nil {
		t.Error("Expected error for empty message ID")
	}

	if err.Error() != "message ID cannot be empty" {
		t.Errorf("Expected 'message ID cannot be empty', got %s", err.Error())
	}
}

func TestMemoryStorage_StoreMessage_CapacityLimit(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{
		MaxMessages: 2,
	})
	ctx := context.Background()

	// Store first message
	message1 := &types.Message{
		MessageID: "test-message-1",
		Sender:    "sender@example.com",
	}
	err := storage.StoreMessage(ctx, message1)
	if err != nil {
		t.Fatalf("Expected no error for first message, got %v", err)
	}

	// Store second message
	message2 := &types.Message{
		MessageID: "test-message-2",
		Sender:    "sender@example.com",
	}
	err = storage.StoreMessage(ctx, message2)
	if err != nil {
		t.Fatalf("Expected no error for second message, got %v", err)
	}

	// Store third message (should fail)
	message3 := &types.Message{
		MessageID: "test-message-3",
		Sender:    "sender@example.com",
	}
	err = storage.StoreMessage(ctx, message3)
	if err == nil {
		t.Error("Expected error when exceeding capacity")
	}

	if err.Error() != "storage capacity exceeded: max 2 messages" {
		t.Errorf("Expected capacity error, got %s", err.Error())
	}
}

func TestMemoryStorage_GetMessage(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	message := &types.Message{
		MessageID: "test-message-1",
		Sender:    "sender@example.com",
		Subject:   "Test Message",
	}

	// Store message first
	err := storage.StoreMessage(ctx, message)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Retrieve message
	retrieved, err := storage.GetMessage(ctx, "test-message-1")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if retrieved.MessageID != message.MessageID {
		t.Errorf("Expected MessageID %s, got %s", message.MessageID, retrieved.MessageID)
	}

	if retrieved.Subject != message.Subject {
		t.Errorf("Expected Subject %s, got %s", message.Subject, retrieved.Subject)
	}
}

func TestMemoryStorage_GetMessage_NotFound(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	_, err := storage.GetMessage(ctx, "non-existent-message")
	if err == nil {
		t.Error("Expected error for non-existent message")
	}

	if err.Error() != "message not found: non-existent-message" {
		t.Errorf("Expected 'message not found' error, got %s", err.Error())
	}
}

func TestMemoryStorage_GetMessage_EmptyID(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	_, err := storage.GetMessage(ctx, "")
	if err == nil {
		t.Error("Expected error for empty message ID")
	}

	if err.Error() != "message ID cannot be empty" {
		t.Errorf("Expected 'message ID cannot be empty', got %s", err.Error())
	}
}

func TestMemoryStorage_DeleteMessage(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	message := &types.Message{
		MessageID: "test-message-1",
		Sender:    "sender@example.com",
	}

	// Store message first
	err := storage.StoreMessage(ctx, message)
	if err != nil {
		t.Fatalf("Failed to store message: %v", err)
	}

	// Delete message
	err = storage.DeleteMessage(ctx, "test-message-1")
	if err != nil {
		t.Fatalf("Expected no error deleting message, got %v", err)
	}

	// Verify message is gone
	_, err = storage.GetMessage(ctx, "test-message-1")
	if err == nil {
		t.Error("Expected error retrieving deleted message")
	}
}

func TestMemoryStorage_DeleteMessage_NotFound(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.DeleteMessage(ctx, "non-existent-message")
	if err == nil {
		t.Error("Expected error deleting non-existent message")
	}

	if err.Error() != "message not found: non-existent-message" {
		t.Errorf("Expected 'message not found' error, got %s", err.Error())
	}
}

func TestMemoryStorage_StoreStatus(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	status := &types.MessageStatus{
		MessageID: "test-message-1",
		Status:    types.StatusDelivered,
		Recipients: []types.RecipientStatus{
			{
				Address:   "recipient@example.com",
				Status:    types.StatusDelivered,
				Timestamp: time.Now().UTC(),
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := storage.StoreStatus(ctx, "test-message-1", status)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify status was stored
	storedStatus, err := storage.GetStatus(ctx, "test-message-1")
	if err != nil {
		t.Fatalf("Expected no error retrieving status, got %v", err)
	}

	if storedStatus.MessageID != status.MessageID {
		t.Errorf("Expected MessageID %s, got %s", status.MessageID, storedStatus.MessageID)
	}

	if storedStatus.Status != status.Status {
		t.Errorf("Expected Status %s, got %s", status.Status, storedStatus.Status)
	}
}

func TestMemoryStorage_StoreStatus_NilStatus(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.StoreStatus(ctx, "test-message-1", nil)
	if err == nil {
		t.Error("Expected error for nil status")
	}

	if err.Error() != "status cannot be nil" {
		t.Errorf("Expected 'status cannot be nil', got %s", err.Error())
	}
}

func TestMemoryStorage_UpdateStatus(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// Store initial status
	status := &types.MessageStatus{
		MessageID: "test-message-1",
		Status:    types.StatusQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := storage.StoreStatus(ctx, "test-message-1", status)
	if err != nil {
		t.Fatalf("Failed to store initial status: %v", err)
	}

	// Update status
	err = storage.UpdateStatus(ctx, "test-message-1", func(s *types.MessageStatus) error {
		s.Status = types.StatusDelivered
		s.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		t.Fatalf("Expected no error updating status, got %v", err)
	}

	// Verify status was updated
	updatedStatus, err := storage.GetStatus(ctx, "test-message-1")
	if err != nil {
		t.Fatalf("Failed to retrieve updated status: %v", err)
	}

	if updatedStatus.Status != types.StatusDelivered {
		t.Errorf("Expected Status %s, got %s", types.StatusDelivered, updatedStatus.Status)
	}
}

func TestMemoryStorage_UpdateStatus_NotFound(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.UpdateStatus(ctx, "non-existent-message", func(s *types.MessageStatus) error {
		return nil
	})
	if err == nil {
		t.Error("Expected error updating non-existent status")
	}

	if err.Error() != "message status not found: non-existent-message" {
		t.Errorf("Expected 'message status not found' error, got %s", err.Error())
	}
}

func TestMemoryStorage_GetInboxMessages(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// Store messages
	message1 := &types.Message{
		MessageID:  "test-message-1",
		Sender:     "sender@example.com",
		Recipients: []string{"agent1@localhost", "agent2@localhost"},
		Subject:    "Test Message 1",
	}

	message2 := &types.Message{
		MessageID:  "test-message-2",
		Sender:     "sender@example.com",
		Recipients: []string{"agent1@localhost"},
		Subject:    "Test Message 2",
	}

	storage.StoreMessage(ctx, message1)
	storage.StoreMessage(ctx, message2)

	// Store statuses with inbox delivery
	status1 := &types.MessageStatus{
		MessageID: "test-message-1",
		Status:    types.StatusDelivered,
		Recipients: []types.RecipientStatus{
			{
				Address:        "agent1@localhost",
				Status:         types.StatusDelivered,
				LocalDelivery:  true,
				InboxDelivered: true,
				Acknowledged:   false,
			},
			{
				Address:        "agent2@localhost",
				Status:         types.StatusDelivered,
				LocalDelivery:  true,
				InboxDelivered: true,
				Acknowledged:   false,
			},
		},
	}

	status2 := &types.MessageStatus{
		MessageID: "test-message-2",
		Status:    types.StatusDelivered,
		Recipients: []types.RecipientStatus{
			{
				Address:        "agent1@localhost",
				Status:         types.StatusDelivered,
				LocalDelivery:  true,
				InboxDelivered: true,
				Acknowledged:   true, // Already acknowledged
			},
		},
	}

	storage.StoreStatus(ctx, "test-message-1", status1)
	storage.StoreStatus(ctx, "test-message-2", status2)

	// Get inbox messages for agent1
	inboxMessages, err := storage.GetInboxMessages(ctx, "agent1@localhost")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should only get message1 (message2 is acknowledged)
	if len(inboxMessages) != 1 {
		t.Errorf("Expected 1 inbox message, got %d", len(inboxMessages))
	}

	if len(inboxMessages) > 0 && inboxMessages[0].MessageID != "test-message-1" {
		t.Errorf("Expected message test-message-1, got %s", inboxMessages[0].MessageID)
	}

	// Get inbox messages for agent2
	inboxMessages, err = storage.GetInboxMessages(ctx, "agent2@localhost")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should get message1
	if len(inboxMessages) != 1 {
		t.Errorf("Expected 1 inbox message for agent2, got %d", len(inboxMessages))
	}
}

func TestMemoryStorage_AcknowledgeMessage(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// Store message and status
	message := &types.Message{
		MessageID:  "test-message-1",
		Recipients: []string{"agent1@localhost"},
	}

	status := &types.MessageStatus{
		MessageID: "test-message-1",
		Recipients: []types.RecipientStatus{
			{
				Address:        "agent1@localhost",
				Status:         types.StatusDelivered,
				LocalDelivery:  true,
				InboxDelivered: true,
				Acknowledged:   false,
			},
		},
	}

	storage.StoreMessage(ctx, message)
	storage.StoreStatus(ctx, "test-message-1", status)

	// Acknowledge message
	err := storage.AcknowledgeMessage(ctx, "agent1@localhost", "test-message-1")
	if err != nil {
		t.Fatalf("Expected no error acknowledging message, got %v", err)
	}

	// Verify message is acknowledged
	updatedStatus, err := storage.GetStatus(ctx, "test-message-1")
	if err != nil {
		t.Fatalf("Failed to get updated status: %v", err)
	}

	if !updatedStatus.Recipients[0].Acknowledged {
		t.Error("Expected message to be acknowledged")
	}

	if updatedStatus.Recipients[0].AcknowledgedAt == nil {
		t.Error("Expected AcknowledgedAt to be set")
	}
}

func TestMemoryStorage_AcknowledgeMessage_AlreadyAcknowledged(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// Store message and status (already acknowledged)
	now := time.Now().UTC()
	status := &types.MessageStatus{
		MessageID: "test-message-1",
		Recipients: []types.RecipientStatus{
			{
				Address:        "agent1@localhost",
				Status:         types.StatusDelivered,
				LocalDelivery:  true,
				InboxDelivered: true,
				Acknowledged:   true,
				AcknowledgedAt: &now,
			},
		},
	}

	storage.StoreStatus(ctx, "test-message-1", status)

	// Try to acknowledge again
	err := storage.AcknowledgeMessage(ctx, "agent1@localhost", "test-message-1")
	if err == nil {
		t.Error("Expected error acknowledging already acknowledged message")
	}

	if err.Error() != "message already acknowledged: test-message-1" {
		t.Errorf("Expected 'message already acknowledged' error, got %s", err.Error())
	}
}

func TestMemoryStorage_ListMessages(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// Store test messages
	messages := []*types.Message{
		{
			MessageID:  "msg-1",
			Sender:     "sender1@example.com",
			Recipients: []string{"recipient1@example.com"},
			Timestamp:  time.Now().Add(-2 * time.Hour),
		},
		{
			MessageID:  "msg-2",
			Sender:     "sender2@example.com",
			Recipients: []string{"recipient2@example.com"},
			Timestamp:  time.Now().Add(-1 * time.Hour),
		},
		{
			MessageID:  "msg-3",
			Sender:     "sender1@example.com",
			Recipients: []string{"recipient1@example.com"},
			Timestamp:  time.Now(),
		},
	}

	for _, msg := range messages {
		storage.StoreMessage(ctx, msg)
	}

	// Test listing all messages
	filter := MessageFilter{}
	result, err := storage.ListMessages(ctx, filter)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(result))
	}

	// Test filtering by sender
	filter = MessageFilter{Sender: "sender1@example.com"}
	result, err = storage.ListMessages(ctx, filter)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 messages from sender1, got %d", len(result))
	}

	// Test limit
	filter = MessageFilter{Limit: 1}
	result, err = storage.ListMessages(ctx, filter)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 message with limit, got %d", len(result))
	}
}

func TestMemoryStorage_GetStats(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// Initially empty
	stats, err := storage.GetStats(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if stats.TotalMessages != 0 {
		t.Errorf("Expected 0 total messages, got %d", stats.TotalMessages)
	}

	// Store some messages and statuses
	message := &types.Message{
		MessageID: "test-message-1",
		Sender:    "sender@example.com",
	}

	status := &types.MessageStatus{
		MessageID: "test-message-1",
		Status:    types.StatusDelivered,
		Recipients: []types.RecipientStatus{
			{
				Address:        "agent1@localhost",
				Status:         types.StatusDelivered,
				LocalDelivery:  true,
				InboxDelivered: true,
				Acknowledged:   false,
			},
		},
	}

	storage.StoreMessage(ctx, message)
	storage.StoreStatus(ctx, "test-message-1", status)

	// Check updated stats
	stats, err = storage.GetStats(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if stats.TotalMessages != 1 {
		t.Errorf("Expected 1 total message, got %d", stats.TotalMessages)
	}

	if stats.TotalStatuses != 1 {
		t.Errorf("Expected 1 total status, got %d", stats.TotalStatuses)
	}

	if stats.DeliveredMessages != 1 {
		t.Errorf("Expected 1 delivered message, got %d", stats.DeliveredMessages)
	}

	if stats.InboxMessages != 1 {
		t.Errorf("Expected 1 inbox message, got %d", stats.InboxMessages)
	}
}

func TestMemoryStorage_HealthCheck(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.HealthCheck(ctx)
	if err != nil {
		t.Errorf("Expected no error for healthy storage, got %v", err)
	}
}

func TestMemoryStorage_Close(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})

	err := storage.Close()
	if err != nil {
		t.Errorf("Expected no error closing memory storage, got %v", err)
	}
}

func TestMemoryStorage_matchesFilter(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})

	message := &types.Message{
		MessageID:  "test-msg",
		Sender:     "sender@example.com",
		Recipients: []string{"recipient1@example.com", "recipient2@example.com"},
		Timestamp:  time.Now(),
	}

	// Test sender filter match
	filter := MessageFilter{Sender: "sender@example.com"}
	if !storage.matchesFilter(message, "test-msg", filter) {
		t.Error("Expected message to match sender filter")
	}

	// Test sender filter no match
	filter = MessageFilter{Sender: "other@example.com"}
	if storage.matchesFilter(message, "test-msg", filter) {
		t.Error("Expected message to not match sender filter")
	}

	// Test recipients filter match
	filter = MessageFilter{Recipients: []string{"recipient1@example.com"}}
	if !storage.matchesFilter(message, "test-msg", filter) {
		t.Error("Expected message to match recipients filter")
	}

	// Test recipients filter no match
	filter = MessageFilter{Recipients: []string{"other@example.com"}}
	if storage.matchesFilter(message, "test-msg", filter) {
		t.Error("Expected message to not match recipients filter")
	}

	// Test since filter
	since := message.Timestamp.Unix() - 3600 // 1 hour before
	filter = MessageFilter{Since: &since}
	if !storage.matchesFilter(message, "test-msg", filter) {
		t.Error("Expected message to match since filter")
	}

	// Test since filter no match
	since = message.Timestamp.Unix() + 3600 // 1 hour after
	filter = MessageFilter{Since: &since}
	if storage.matchesFilter(message, "test-msg", filter) {
		t.Error("Expected message to not match since filter")
	}
}
