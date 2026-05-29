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
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
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

// TestMemoryStorage_ListMessages_FilterWithPagination verifies that offset and
// limit are applied to the filtered result set (not to the raw, unfiltered
// iteration) and that results are returned in a deterministic order.
func TestMemoryStorage_ListMessages_FilterWithPagination(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	base := time.Now()
	// Interleave matching (sender1) and non-matching (sender2) messages so a
	// pagination bug that counts non-matching rows against the offset is exposed.
	// sender1 messages, newest first: match-0 (newest) .. match-4 (oldest).
	for i := 0; i < 5; i++ {
		if err := storage.StoreMessage(ctx, &types.Message{
			MessageID:  fmt.Sprintf("match-%d", i),
			Sender:     "sender1@example.com",
			Recipients: []string{"recipient@example.com"},
			Timestamp:  base.Add(time.Duration(-i) * time.Minute),
		}); err != nil {
			t.Fatalf("store match-%d: %v", i, err)
		}
		if err := storage.StoreMessage(ctx, &types.Message{
			MessageID:  fmt.Sprintf("other-%d", i),
			Sender:     "sender2@example.com",
			Recipients: []string{"recipient@example.com"},
			Timestamp:  base.Add(time.Duration(-i) * time.Minute),
		}); err != nil {
			t.Fatalf("store other-%d: %v", i, err)
		}
	}

	// Page through the 5 matching messages 2 at a time, newest first.
	filter := MessageFilter{Sender: "sender1@example.com", Offset: 2, Limit: 2}
	result, err := storage.ListMessages(ctx, filter)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Expected 2 messages for offset=2 limit=2, got %d", len(result))
	}

	// Every returned message must match the filter.
	for _, msg := range result {
		if msg.Sender != "sender1@example.com" {
			t.Errorf("Expected only sender1 messages, got sender %q (id %s)", msg.Sender, msg.MessageID)
		}
	}

	// Newest-first ordering means offset=2 skips match-0 and match-1.
	if result[0].MessageID != "match-2" || result[1].MessageID != "match-3" {
		t.Errorf("Expected [match-2, match-3], got [%s, %s]", result[0].MessageID, result[1].MessageID)
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

func TestMemoryStorage_CreateAgent(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()
	agent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	err := storage.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Expected no error creating agent, got %v", err)
	}

	// Verify agent was created
	storedAgent, err := storage.GetAgent(ctx, "agent1@localhost")
	if err != nil {
		t.Fatalf("Expected no error retrieving agent, got %v", err)
	}

	if storedAgent.Address != agent.Address {
		t.Errorf("Expected Address %s, got %s", agent.Address, storedAgent.Address)
	}
}

func TestMemoryStorage_CreateAgent_Duplicate(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()
	agent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	err := storage.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Expected no error creating agent, got %v", err)
	}

	// Try to create duplicate agent
	err = storage.CreateAgent(ctx, agent)
	if err == nil {
		t.Error("Expected error creating duplicate agent")
	}

	if err.Error() != "agent already exists: agent1@localhost" {
		t.Errorf("Expected 'agent already exists' error, got %s", err.Error())
	}
}

func TestMemoryStorage_CreateAgent_NilAgent(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.CreateAgent(ctx, nil)
	if err == nil {
		t.Error("Expected error creating nil agent")
	}

	if err.Error() != "agent cannot be nil" {
		t.Errorf("Expected 'agent cannot be nil' error, got %s", err.Error())
	}
}

func TestMemoryStorage_GetAgent_NotFound(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	_, err := storage.GetAgent(ctx, "non-existent-agent")
	if err == nil {
		t.Error("Expected error retrieving non-existent agent")
	}

	if err.Error() != "agent not found: non-existent-agent" {
		t.Errorf("Expected 'agent not found' error, got %s", err.Error())
	}
}

func TestMemoryStorage_GetAgent_EmptyAddress(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	_, err := storage.GetAgent(ctx, "")
	if err == nil {
		t.Error("Expected error retrieving agent with empty address")
	}

	if err.Error() != "agent address cannot be empty" {
		t.Errorf("Expected 'agent address cannot be empty' error, got %s", err.Error())
	}
}

func TestMemoryStorage_UpdateAgent(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()
	agent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	err := storage.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Expected no error creating agent, got %v", err)
	}

	// Update agent
	agent.PushTarget = "http://localhost:8080/agent1/new-webhook"
	err = storage.UpdateAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Expected no error updating agent, got %v", err)
	}

	// Verify update
	updatedAgent, err := storage.GetAgent(ctx, "agent1@localhost")
	if err != nil {
		t.Fatalf("Expected no error retrieving agent, got %v", err)
	}

	if updatedAgent.PushTarget != "http://localhost:8080/agent1/new-webhook" {
		t.Errorf("Expected PushTarget to be updated, got %s", updatedAgent.PushTarget)
	}
}

func TestMemoryStorage_UpdateAgent_NotFound(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()
	agent := &agents.LocalAgent{
		Address:          "non-existent-agent",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	err := storage.UpdateAgent(ctx, agent)
	if err == nil {
		t.Error("Expected error updating non-existent agent")
	}

	if err.Error() != "agent not found: non-existent-agent" {
		t.Errorf("Expected 'agent not found' error, got %s", err.Error())
	}
}

func TestMemoryStorage_UpdateAgent_NilAgent(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.UpdateAgent(ctx, nil)
	if err == nil {
		t.Error("Expected error updating nil agent")
	}

	if err.Error() != "agent cannot be nil" {
		t.Errorf("Expected 'agent cannot be nil' error, got %s", err.Error())
	}
}

func TestMemoryStorage_DeleteAgent(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()
	agent := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	err := storage.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Expected no error creating agent, got %v", err)
	}

	// Delete agent
	err = storage.DeleteAgent(ctx, "agent1@localhost")
	if err != nil {
		t.Fatalf("Expected no error deleting agent, got %v", err)
	}

	// Verify deletion
	_, err = storage.GetAgent(ctx, "agent1@localhost")
	if err == nil {
		t.Error("Expected error retrieving deleted agent")
	}
}

func TestMemoryStorage_DeleteAgent_NotFound(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.DeleteAgent(ctx, "non-existent-agent")
	if err == nil {
		t.Error("Expected error deleting non-existent agent")
	}

	if err.Error() != "agent not found: non-existent-agent" {
		t.Errorf("Expected 'agent not found' error, got %s", err.Error())
	}
}

func TestMemoryStorage_DeleteAgent_EmptyAddress(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	err := storage.DeleteAgent(ctx, "")
	if err == nil {
		t.Error("Expected error deleting agent with empty address")
	}

	if err.Error() != "agent address cannot be empty" {
		t.Errorf("Expected 'agent address cannot be empty' error, got %s", err.Error())
	}
}

func TestMemoryStorage_ListAgents(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	agent1 := &agents.LocalAgent{
		Address:          "agent1@localhost",
		DeliveryMode:     "push",
		PushTarget:       "http://localhost:8080/agent1/webhook",
		Headers:          map[string]string{"accept": "application/json"},
		SupportedSchemas: []string{"schema1", "schema2"},
		RequiresSchema:   true,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	agent2 := &agents.LocalAgent{
		Address:          "agent2@localhost",
		DeliveryMode:     "pull",
		SupportedSchemas: []string{"schema3"},
		RequiresSchema:   false,
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	storage.CreateAgent(ctx, agent1)
	storage.CreateAgent(ctx, agent2)

	agentsList, err := storage.ListAgents(ctx)
	if err != nil {
		t.Fatalf("Expected no error listing agents, got %v", err)
	}

	if len(agentsList) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agentsList))
	}
}

func TestMemoryStorage_GetSuportedSchemas(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	agent1 := &agents.LocalAgent{
		Address:          "agent1@localhost",
		SupportedSchemas: []string{"schema1", "schema2"},
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	agent2 := &agents.LocalAgent{
		Address:          "agent2@localhost",
		SupportedSchemas: []string{"schema2", "schema3"},
		CreatedAt:        time.Now(),
		LastAccess:       time.Now(),
	}

	storage.CreateAgent(ctx, agent1)
	storage.CreateAgent(ctx, agent2)

	schemas, err := storage.GetSupportedSchemas(ctx)
	if err != nil {
		t.Fatalf("Expected no error getting supported schemas, got %v", err)
	}

	expectedSchemas := map[string]bool{
		"schema1": true,
		"schema2": true,
		"schema3": true,
	}

	if len(schemas) != len(expectedSchemas) {
		t.Errorf("Expected %d unique schemas, got %d", len(expectedSchemas), len(schemas))
	}

	for _, schema := range schemas {
		if !expectedSchemas[schema] {
			t.Errorf("Unexpected schema found: %s", schema)
		}
	}
}

// TestMemoryStorage_ReturnedValuesAreIsolated verifies that the in-memory
// store hands callers independent copies, so mutating a returned value (or the
// value originally passed to a Store/Create call) never alters stored state.
func TestMemoryStorage_ReturnedValuesAreIsolated(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// --- Message: mutation of the stored input and of a returned value must
	//     not affect what the store holds. ---
	msg := &types.Message{
		MessageID:  "iso-msg",
		Sender:     "a@example.com",
		Recipients: []string{"b@example.com"},
		Headers:    map[string]interface{}{"k": "v"},
		Payload:    []byte(`{"n":1}`),
		Timestamp:  time.Now(),
	}
	if err := storage.StoreMessage(ctx, msg); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	// Mutate the original after storing.
	msg.Recipients[0] = "tampered@example.com"
	msg.Headers["k"] = "tampered"
	msg.Payload[2] = 'X'

	got, err := storage.GetMessage(ctx, "iso-msg")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if got.Recipients[0] != "b@example.com" {
		t.Errorf("stored recipient mutated via input alias: %s", got.Recipients[0])
	}
	if got.Headers["k"] != "v" {
		t.Errorf("stored header mutated via input alias: %v", got.Headers["k"])
	}
	if string(got.Payload) != `{"n":1}` {
		t.Errorf("stored payload mutated via input alias: %s", got.Payload)
	}
	// Mutate the returned value; a fresh Get must be unaffected.
	got.Recipients[0] = "tampered2@example.com"
	got.Headers["k"] = "tampered2"
	got2, _ := storage.GetMessage(ctx, "iso-msg")
	if got2.Recipients[0] != "b@example.com" || got2.Headers["k"] != "v" {
		t.Errorf("stored message mutated via returned alias: %v / %v", got2.Recipients[0], got2.Headers["k"])
	}

	// --- MessageStatus: Recipients slice is mutated in place by Update/Ack,
	//     so returned values must own a distinct backing array. ---
	st := &types.MessageStatus{
		MessageID:  "iso-msg",
		Status:     types.StatusQueued,
		Recipients: []types.RecipientStatus{{Address: "b@example.com", Status: types.StatusQueued}},
	}
	if err := storage.StoreStatus(ctx, "iso-msg", st); err != nil {
		t.Fatalf("StoreStatus: %v", err)
	}
	gotSt, _ := storage.GetStatus(ctx, "iso-msg")
	gotSt.Recipients[0].Status = types.StatusDelivered
	gotSt.Status = types.StatusDelivered
	gotSt2, _ := storage.GetStatus(ctx, "iso-msg")
	if gotSt2.Status != types.StatusQueued || gotSt2.Recipients[0].Status != types.StatusQueued {
		t.Errorf("stored status mutated via returned alias: %v / %v", gotSt2.Status, gotSt2.Recipients[0].Status)
	}

	// --- LocalAgent: Headers map and SupportedSchemas slice must be isolated. ---
	ag := &agents.LocalAgent{
		Address:          "agent@example.com",
		DeliveryMode:     "pull",
		Headers:          map[string]string{"h": "1"},
		SupportedSchemas: []string{"agntcy:test.*"},
	}
	if err := storage.CreateAgent(ctx, ag); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	ag.Headers["h"] = "tampered"
	ag.SupportedSchemas[0] = "tampered"
	gotAg, _ := storage.GetAgent(ctx, "agent@example.com")
	if gotAg.Headers["h"] != "1" || gotAg.SupportedSchemas[0] != "agntcy:test.*" {
		t.Errorf("stored agent mutated via input alias: %v / %v", gotAg.Headers["h"], gotAg.SupportedSchemas[0])
	}
	gotAg.Headers["h"] = "tampered2"
	gotAg.SupportedSchemas[0] = "tampered2"
	gotAg2, _ := storage.GetAgent(ctx, "agent@example.com")
	if gotAg2.Headers["h"] != "1" || gotAg2.SupportedSchemas[0] != "agntcy:test.*" {
		t.Errorf("stored agent mutated via returned alias: %v / %v", gotAg2.Headers["h"], gotAg2.SupportedSchemas[0])
	}
}

// TestMemoryStorage_ConcurrentReadDuringUpdate exercises the data race between a
// reader holding a returned status and a concurrent in-place status update.
// Run with -race.
func TestMemoryStorage_ConcurrentReadDuringUpdate(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	if err := storage.StoreStatus(ctx, "race-msg", &types.MessageStatus{
		MessageID:  "race-msg",
		Status:     types.StatusQueued,
		Recipients: []types.RecipientStatus{{Address: "b@example.com", Status: types.StatusQueued}},
	}); err != nil {
		t.Fatalf("StoreStatus: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Writer: repeatedly mutate the stored status in place.
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = storage.UpdateStatus(ctx, "race-msg", func(s *types.MessageStatus) error {
				s.Attempts++
				s.Recipients[0].Status = types.StatusDelivered
				return nil
			})
		}
	}()
	// Reader: read returned snapshots' mutable fields.
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if s, err := storage.GetStatus(ctx, "race-msg"); err == nil {
				_ = s.Attempts
				if len(s.Recipients) > 0 {
					_ = s.Recipients[0].Status
				}
			}
		}
	}()
	wg.Wait()
}
