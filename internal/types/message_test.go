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

package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageValidation(t *testing.T) {
	// Valid message
	validMessage := &Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	err := validMessage.Validate()
	if err != nil {
		t.Errorf("Valid message should pass validation: %v", err)
	}

	// Test invalid version
	invalidVersion := *validMessage
	invalidVersion.Version = "2.0"
	err = invalidVersion.Validate()
	if err == nil {
		t.Error("Message with invalid version should fail validation")
	}

	// Test missing message ID
	missingID := *validMessage
	missingID.MessageID = ""
	err = missingID.Validate()
	if err == nil {
		t.Error("Message with missing ID should fail validation")
	}

	// Test empty recipients
	emptyRecipients := *validMessage
	emptyRecipients.Recipients = []string{}
	err = emptyRecipients.Validate()
	if err == nil {
		t.Error("Message with empty recipients should fail validation")
	}
}

func TestCoordinationValidation(t *testing.T) {
	// Valid parallel coordination
	parallelCoord := &CoordinationConfig{
		Type:    "parallel",
		Timeout: 3600,
	}
	err := parallelCoord.Validate()
	if err != nil {
		t.Errorf("Valid parallel coordination should pass: %v", err)
	}

	// Valid sequential coordination
	sequentialCoord := &CoordinationConfig{
		Type:     "sequential",
		Timeout:  3600,
		Sequence: []string{"first@example.com", "second@example.com"},
	}
	err = sequentialCoord.Validate()
	if err != nil {
		t.Errorf("Valid sequential coordination should pass: %v", err)
	}

	// Valid conditional coordination
	conditionalCoord := &CoordinationConfig{
		Type:    "conditional",
		Timeout: 3600,
		Conditions: []ConditionalRule{
			{
				If:   "approval@manager.com responds with approve=true",
				Then: []string{"execute@system.com"},
				Else: []string{"reject@system.com"},
			},
		},
	}
	err = conditionalCoord.Validate()
	if err != nil {
		t.Errorf("Valid conditional coordination should pass: %v", err)
	}

	// Invalid coordination type
	invalidType := &CoordinationConfig{
		Type:    "invalid",
		Timeout: 3600,
	}
	err = invalidType.Validate()
	if err == nil {
		t.Error("Invalid coordination type should fail validation")
	}

	// Sequential without sequence
	sequentialNoSeq := &CoordinationConfig{
		Type:    "sequential",
		Timeout: 3600,
	}
	err = sequentialNoSeq.Validate()
	if err == nil {
		t.Error("Sequential coordination without sequence should fail validation")
	}

	// Conditional without conditions
	conditionalNoCond := &CoordinationConfig{
		Type:    "conditional",
		Timeout: 3600,
	}
	err = conditionalNoCond.Validate()
	if err == nil {
		t.Error("Conditional coordination without conditions should fail validation")
	}
}

func TestMessageSize(t *testing.T) {
	message := &Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	size := message.Size()
	if size <= 0 {
		t.Error("Message size should be positive")
	}

	// Test with larger payload
	largePayload := make(map[string]string)
	for i := 0; i < 1000; i++ {
		largePayload[string(rune(i))] = "test data"
	}
	payloadBytes, err := json.Marshal(largePayload)
	if err != nil {
		t.Fatalf("Failed to marshal large payload: %v", err)
	}
	message.Payload = json.RawMessage(payloadBytes)

	largeSize := message.Size()
	if largeSize <= size {
		t.Error("Message with larger payload should have larger size")
	}
}

func TestMessageJSONSerialization(t *testing.T) {
	originalMessage := &Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC().Truncate(time.Millisecond), // Truncate for comparison
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
		Headers: map[string]interface{}{
			"priority": "high",
			"custom":   "value",
		},
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(originalMessage)
	if err != nil {
		t.Fatalf("Failed to marshal message to JSON: %v", err)
	}

	// Deserialize from JSON
	var deserializedMessage Message
	err = json.Unmarshal(jsonBytes, &deserializedMessage)
	if err != nil {
		t.Fatalf("Failed to unmarshal message from JSON: %v", err)
	}

	// Compare key fields
	if deserializedMessage.Version != originalMessage.Version {
		t.Errorf("Version mismatch: got %s, want %s", deserializedMessage.Version, originalMessage.Version)
	}

	if deserializedMessage.MessageID != originalMessage.MessageID {
		t.Errorf("MessageID mismatch: got %s, want %s", deserializedMessage.MessageID, originalMessage.MessageID)
	}

	if deserializedMessage.Sender != originalMessage.Sender {
		t.Errorf("Sender mismatch: got %s, want %s", deserializedMessage.Sender, originalMessage.Sender)
	}

	if len(deserializedMessage.Recipients) != len(originalMessage.Recipients) {
		t.Errorf("Recipients length mismatch: got %d, want %d", len(deserializedMessage.Recipients), len(originalMessage.Recipients))
	}
}

func BenchmarkMessageValidation(b *testing.B) {
	message := &Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := message.Validate()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageSize(b *testing.B) {
	message := &Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@example.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"test": "data"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = message.Size()
	}
}
