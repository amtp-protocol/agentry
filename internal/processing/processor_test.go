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
	"fmt"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestNewMessageProcessor(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()

	processor := NewMessageProcessor(discovery, deliveryEngine)

	if processor == nil {
		t.Fatal("NewMessageProcessor returned nil")
	}

	if processor.discovery != discovery {
		t.Error("Discovery not set correctly")
	}

	if processor.deliveryEngine != deliveryEngine {
		t.Error("DeliveryEngine not set correctly")
	}

	if processor.idempotencyMap == nil {
		t.Error("IdempotencyMap not initialized")
	}

	if processor.messageStore == nil {
		t.Error("MessageStore not initialized")
	}

	if processor.statusStore == nil {
		t.Error("StatusStore not initialized")
	}
}

func TestProcessMessage_ImmediatePath(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	options := ProcessingOptions{
		ImmediatePath: true,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()
	result, err := processor.ProcessMessage(ctx, message, options)

	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	if result == nil {
		t.Fatal("ProcessMessage returned nil result")
	}

	if result.MessageID != message.MessageID {
		t.Errorf("Expected MessageID %s, got %s", message.MessageID, result.MessageID)
	}

	if result.Status != types.StatusDelivered {
		t.Errorf("Expected status %s, got %s", types.StatusDelivered, result.Status)
	}

	if len(result.Recipients) != 1 {
		t.Errorf("Expected 1 recipient, got %d", len(result.Recipients))
	}

	if result.Recipients[0].Status != types.StatusDelivered {
		t.Errorf("Expected recipient status %s, got %s", types.StatusDelivered, result.Recipients[0].Status)
	}
}

func TestProcessMessage_ParallelCoordination(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	message.Recipients = []string{"recipient1@test.com", "recipient2@test.com"}
	message.Coordination = &types.CoordinationConfig{
		Type:    "parallel",
		Timeout: 30,
	}

	options := ProcessingOptions{
		ImmediatePath: false,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()
	result, err := processor.ProcessMessage(ctx, message, options)

	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	if result.Status != types.StatusDelivered {
		t.Errorf("Expected status %s, got %s", types.StatusDelivered, result.Status)
	}

	if len(result.Recipients) != 2 {
		t.Errorf("Expected 2 recipients, got %d", len(result.Recipients))
	}

	for _, recipient := range result.Recipients {
		if recipient.Status != types.StatusDelivered {
			t.Errorf("Expected recipient status %s, got %s", types.StatusDelivered, recipient.Status)
		}
	}
}

func TestProcessMessage_SequentialCoordination(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	message.Recipients = []string{"recipient1@test.com", "recipient2@test.com"}
	message.Coordination = &types.CoordinationConfig{
		Type:     "sequential",
		Sequence: []string{"recipient1@test.com", "recipient2@test.com"},
		Timeout:  30,
	}

	options := ProcessingOptions{
		ImmediatePath: false,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()
	result, err := processor.ProcessMessage(ctx, message, options)

	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	if result.Status != types.StatusDelivered {
		t.Errorf("Expected status %s, got %s", types.StatusDelivered, result.Status)
	}

	if len(result.Recipients) != 2 {
		t.Errorf("Expected 2 recipients, got %d", len(result.Recipients))
	}
}

func TestProcessMessage_SequentialCoordination_StopOnFailure(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	// Set first recipient to fail by setting a delivery error
	deliveryEngine.SetDeliveryError(fmt.Errorf("test delivery failure"))

	message := createTestMessage()
	message.Recipients = []string{"recipient1@test.com", "recipient2@test.com"}
	message.Coordination = &types.CoordinationConfig{
		Type:          "sequential",
		Sequence:      []string{"recipient1@test.com", "recipient2@test.com"},
		StopOnFailure: true,
		Timeout:       30,
	}

	options := ProcessingOptions{
		ImmediatePath: false,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()
	result, err := processor.ProcessMessage(ctx, message, options)

	if err == nil {
		t.Fatal("Expected error for sequential coordination with stop on failure")
	}

	if result.Status != types.StatusFailed {
		t.Errorf("Expected status %s, got %s", types.StatusFailed, result.Status)
	}
}

func TestProcessMessage_Idempotency(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	options := ProcessingOptions{
		ImmediatePath: true,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()

	// First processing
	result1, err := processor.ProcessMessage(ctx, message, options)
	if err != nil {
		t.Fatalf("First ProcessMessage failed: %v", err)
	}

	// Second processing with same idempotency key
	result2, err := processor.ProcessMessage(ctx, message, options)
	if err != nil {
		t.Fatalf("Second ProcessMessage failed: %v", err)
	}

	// Should return the same result
	if result1.MessageID != result2.MessageID {
		t.Errorf("Expected same MessageID, got %s and %s", result1.MessageID, result2.MessageID)
	}

	if result1.Status != result2.Status {
		t.Errorf("Expected same status, got %s and %s", result1.Status, result2.Status)
	}
}

func TestGetMessage(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	options := ProcessingOptions{
		ImmediatePath: true,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()
	_, err := processor.ProcessMessage(ctx, message, options)
	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	// Retrieve the message
	retrievedMessage, err := processor.GetMessage(message.MessageID)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}

	if retrievedMessage.MessageID != message.MessageID {
		t.Errorf("Expected MessageID %s, got %s", message.MessageID, retrievedMessage.MessageID)
	}

	if retrievedMessage.Sender != message.Sender {
		t.Errorf("Expected Sender %s, got %s", message.Sender, retrievedMessage.Sender)
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	_, err := processor.GetMessage("nonexistent-id")
	if err == nil {
		t.Error("Expected error for nonexistent message")
	}

	expectedError := "message not found: nonexistent-id"
	if err.Error() != expectedError {
		t.Errorf("Expected error %s, got %s", expectedError, err.Error())
	}
}

func TestGetMessageStatus(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	options := ProcessingOptions{
		ImmediatePath: true,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()
	_, err := processor.ProcessMessage(ctx, message, options)
	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	// Retrieve the status
	status, err := processor.GetMessageStatus(message.MessageID)
	if err != nil {
		t.Fatalf("GetMessageStatus failed: %v", err)
	}

	if status.MessageID != message.MessageID {
		t.Errorf("Expected MessageID %s, got %s", message.MessageID, status.MessageID)
	}

	if status.Status != types.StatusDelivered {
		t.Errorf("Expected status %s, got %s", types.StatusDelivered, status.Status)
	}

	if len(status.Recipients) != 1 {
		t.Errorf("Expected 1 recipient, got %d", len(status.Recipients))
	}
}

func TestGetMessageStatus_NotFound(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	_, err := processor.GetMessageStatus("nonexistent-id")
	if err == nil {
		t.Error("Expected error for nonexistent message status")
	}

	expectedError := "message status not found: nonexistent-id"
	if err.Error() != expectedError {
		t.Errorf("Expected error %s, got %s", expectedError, err.Error())
	}
}

func TestCleanupExpiredEntries(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	// Add an expired entry
	expiredResult := &ProcessingResult{
		MessageID: "expired-message",
		Status:    types.StatusDelivered,
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
	}
	processor.storeIdempotencyResult("expired-key", expiredResult)

	// Add a valid entry
	validResult := &ProcessingResult{
		MessageID: "valid-message",
		Status:    types.StatusDelivered,
		ExpiresAt: time.Now().Add(1 * time.Hour), // Expires in 1 hour
	}
	processor.storeIdempotencyResult("valid-key", validResult)

	// Verify both entries exist
	if len(processor.idempotencyMap) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(processor.idempotencyMap))
	}

	// Cleanup expired entries
	processor.CleanupExpiredEntries()

	// Verify only valid entry remains
	if len(processor.idempotencyMap) != 1 {
		t.Errorf("Expected 1 entry after cleanup, got %d", len(processor.idempotencyMap))
	}

	if _, exists := processor.idempotencyMap["valid-key"]; !exists {
		t.Error("Valid entry should still exist after cleanup")
	}

	if _, exists := processor.idempotencyMap["expired-key"]; exists {
		t.Error("Expired entry should be removed after cleanup")
	}
}

func TestEvaluateCondition(t *testing.T) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()

	tests := []struct {
		condition string
		expected  bool
	}{
		{"always", true},
		{"never", false},
		{"unknown", true}, // Default to true for unknown conditions
	}

	for _, test := range tests {
		result := processor.evaluateCondition(test.condition, message)
		if result != test.expected {
			t.Errorf("evaluateCondition(%s) = %v, expected %v", test.condition, result, test.expected)
		}
	}
}

func BenchmarkProcessMessage(b *testing.B) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	message := createTestMessage()
	options := ProcessingOptions{
		ImmediatePath: true,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different idempotency keys to avoid caching
		message.IdempotencyKey = "benchmark-key-" + string(rune(i))
		_, err := processor.ProcessMessage(ctx, message, options)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIdempotencyCheck(b *testing.B) {
	discovery := NewMockDiscovery()
	deliveryEngine := NewMockDeliveryEngine()
	processor := NewMessageProcessor(discovery, deliveryEngine)

	// Pre-populate with some entries
	for i := 0; i < 1000; i++ {
		result := &ProcessingResult{
			MessageID: "message-" + string(rune(i)),
			Status:    types.StatusDelivered,
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		processor.storeIdempotencyResult("key-"+string(rune(i)), result)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "key-" + string(rune(i%1000))
		processor.checkIdempotency(key)
	}
}
