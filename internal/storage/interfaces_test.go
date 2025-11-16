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

// TestStorageInterface verifies that MemoryStorage implements Storage interface
func TestStorageInterface(t *testing.T) {
	var _ Storage = (*MemoryStorage)(nil)
}

// TestStorageStats verifies StorageStats structure
func TestStorageStats(t *testing.T) {
	stats := StorageStats{
		TotalMessages:        100,
		TotalStatuses:        100,
		PendingMessages:      10,
		DeliveredMessages:    80,
		FailedMessages:       10,
		InboxMessages:        25,
		AcknowledgedMessages: 55,
	}

	if stats.TotalMessages != 100 {
		t.Errorf("Expected TotalMessages to be 100, got %d", stats.TotalMessages)
	}

	if stats.TotalStatuses != 100 {
		t.Errorf("Expected TotalStatuses to be 100, got %d", stats.TotalStatuses)
	}

	if stats.PendingMessages != 10 {
		t.Errorf("Expected PendingMessages to be 10, got %d", stats.PendingMessages)
	}

	if stats.DeliveredMessages != 80 {
		t.Errorf("Expected DeliveredMessages to be 80, got %d", stats.DeliveredMessages)
	}

	if stats.FailedMessages != 10 {
		t.Errorf("Expected FailedMessages to be 10, got %d", stats.FailedMessages)
	}

	if stats.InboxMessages != 25 {
		t.Errorf("Expected InboxMessages to be 25, got %d", stats.InboxMessages)
	}

	if stats.AcknowledgedMessages != 55 {
		t.Errorf("Expected AcknowledgedMessages to be 55, got %d", stats.AcknowledgedMessages)
	}
}

// TestMessageFilter verifies MessageFilter structure and functionality
func TestMessageFilter(t *testing.T) {
	since := time.Now().Unix() - 3600 // 1 hour ago

	filter := MessageFilter{
		Sender:     "sender@example.com",
		Recipients: []string{"recipient1@example.com", "recipient2@example.com"},
		Status:     types.StatusDelivered,
		Since:      &since,
		Limit:      10,
		Offset:     5,
	}

	if filter.Sender != "sender@example.com" {
		t.Errorf("Expected Sender to be 'sender@example.com', got %s", filter.Sender)
	}

	if len(filter.Recipients) != 2 {
		t.Errorf("Expected 2 recipients, got %d", len(filter.Recipients))
	}

	if filter.Recipients[0] != "recipient1@example.com" {
		t.Errorf("Expected first recipient to be 'recipient1@example.com', got %s", filter.Recipients[0])
	}

	if filter.Status != types.StatusDelivered {
		t.Errorf("Expected Status to be %s, got %s", types.StatusDelivered, filter.Status)
	}

	if filter.Since == nil {
		t.Error("Expected Since to be set")
	} else if *filter.Since != since {
		t.Errorf("Expected Since to be %d, got %d", since, *filter.Since)
	}

	if filter.Limit != 10 {
		t.Errorf("Expected Limit to be 10, got %d", filter.Limit)
	}

	if filter.Offset != 5 {
		t.Errorf("Expected Offset to be 5, got %d", filter.Offset)
	}
}

// TestStatusUpdater verifies StatusUpdater function type
func TestStatusUpdater(t *testing.T) {
	status := &types.MessageStatus{
		MessageID: "test-message",
		Status:    types.StatusQueued,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Create a status updater function
	updater := func(s *types.MessageStatus) error {
		s.Status = types.StatusDelivered
		s.UpdatedAt = time.Now().UTC()
		return nil
	}

	// Apply the updater
	err := updater(status)
	if err != nil {
		t.Fatalf("Expected no error from updater, got %v", err)
	}

	if status.Status != types.StatusDelivered {
		t.Errorf("Expected Status to be updated to %s, got %s", types.StatusDelivered, status.Status)
	}
}

// TestStorageConfig verifies StorageConfig structure (memory only)
func TestStorageConfig(t *testing.T) {
	config := StorageConfig{
		Type: "memory",
		Memory: &MemoryStorageConfig{
			MaxMessages: 1000,
			TTL:         24,
		},
	}

	if config.Type != "memory" {
		t.Errorf("Expected Type to be 'memory', got %s", config.Type)
	}

	// Test Memory config
	if config.Memory == nil {
		t.Fatal("Expected Memory config to be set")
	}

	if config.Memory.MaxMessages != 1000 {
		t.Errorf("Expected Memory MaxMessages to be 1000, got %d", config.Memory.MaxMessages)
	}

	if config.Memory.TTL != 24 {
		t.Errorf("Expected Memory TTL to be 24, got %d", config.Memory.TTL)
	}

	// Other configs should be nil since we only support memory
	if config.Database != nil {
		t.Error("Expected Database config to be nil (not supported)")
	}

	if config.Redis != nil {
		t.Error("Expected Redis config to be nil (not supported)")
	}
}

// TestMemoryStorageConfig verifies MemoryStorageConfig structure
func TestMemoryStorageConfig(t *testing.T) {
	config := MemoryStorageConfig{
		MaxMessages: 5000,
		TTL:         48,
	}

	if config.MaxMessages != 5000 {
		t.Errorf("Expected MaxMessages to be 5000, got %d", config.MaxMessages)
	}

	if config.TTL != 48 {
		t.Errorf("Expected TTL to be 48, got %d", config.TTL)
	}
}

// TestStorageInterfaceCompliance tests that our storage implementations comply with the interface
func TestStorageInterfaceCompliance(t *testing.T) {
	ctx := context.Background()

	// Test with memory storage
	storage := NewMemoryStorage(MemoryStorageConfig{})

	// Test all interface methods exist and work
	testMessage := &types.Message{
		MessageID:  "interface-test",
		Sender:     "test@example.com",
		Recipients: []string{"recipient@example.com"},
		Subject:    "Interface Test",
		Timestamp:  time.Now().UTC(),
	}

	testStatus := &types.MessageStatus{
		MessageID: "interface-test",
		Status:    types.StatusQueued,
		Recipients: []types.RecipientStatus{
			{
				Address:   "recipient@example.com",
				Status:    types.StatusQueued,
				Timestamp: time.Now().UTC(),
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Test StoreMessage
	err := storage.StoreMessage(ctx, testMessage)
	if err != nil {
		t.Errorf("StoreMessage failed: %v", err)
	}

	// Test GetMessage
	_, err = storage.GetMessage(ctx, "interface-test")
	if err != nil {
		t.Errorf("GetMessage failed: %v", err)
	}

	// Test StoreStatus
	err = storage.StoreStatus(ctx, "interface-test", testStatus)
	if err != nil {
		t.Errorf("StoreStatus failed: %v", err)
	}

	// Test GetStatus
	_, err = storage.GetStatus(ctx, "interface-test")
	if err != nil {
		t.Errorf("GetStatus failed: %v", err)
	}

	// Test UpdateStatus
	err = storage.UpdateStatus(ctx, "interface-test", func(s *types.MessageStatus) error {
		s.Status = types.StatusDelivered
		return nil
	})
	if err != nil {
		t.Errorf("UpdateStatus failed: %v", err)
	}

	// Test ListMessages
	_, err = storage.ListMessages(ctx, MessageFilter{})
	if err != nil {
		t.Errorf("ListMessages failed: %v", err)
	}

	// Test GetInboxMessages
	_, err = storage.GetInboxMessages(ctx, "recipient@example.com")
	if err != nil {
		t.Errorf("GetInboxMessages failed: %v", err)
	}

	// Test AcknowledgeMessage (will fail because message not in inbox, but method should exist)
	_ = storage.AcknowledgeMessage(ctx, "recipient@example.com", "interface-test")

	// Test DeleteMessage
	err = storage.DeleteMessage(ctx, "interface-test")
	if err != nil {
		t.Errorf("DeleteMessage failed: %v", err)
	}

	// Test DeleteStatus
	err = storage.DeleteStatus(ctx, "interface-test")
	if err != nil {
		t.Errorf("DeleteStatus failed: %v", err)
	}

	// Test GetStats
	_, err = storage.GetStats(ctx)
	if err != nil {
		t.Errorf("GetStats failed: %v", err)
	}

	// Test HealthCheck
	err = storage.HealthCheck(ctx)
	if err != nil {
		t.Errorf("HealthCheck failed: %v", err)
	}

	// Test Close
	err = storage.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestEmptyMessageFilter tests behavior with empty filter
func TestEmptyMessageFilter(t *testing.T) {
	filter := MessageFilter{}

	if filter.Sender != "" {
		t.Errorf("Expected empty Sender, got %s", filter.Sender)
	}

	if len(filter.Recipients) != 0 {
		t.Errorf("Expected empty Recipients, got %d items", len(filter.Recipients))
	}

	if filter.Status != "" {
		t.Errorf("Expected empty Status, got %s", filter.Status)
	}

	if filter.Since != nil {
		t.Error("Expected Since to be nil")
	}

	if filter.Limit != 0 {
		t.Errorf("Expected Limit to be 0, got %d", filter.Limit)
	}

	if filter.Offset != 0 {
		t.Errorf("Expected Offset to be 0, got %d", filter.Offset)
	}
}
