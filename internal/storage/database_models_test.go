package storage

import (
	"reflect"
	"testing"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestMessage_BeforeCreate_SetsTimestamp(t *testing.T) {
	msg := &Message{}
	_ = msg.BeforeCreate(nil)
	if msg.Timestamp.IsZero() {
		t.Errorf("Timestamp should be set by BeforeCreate")
	}
}

func TestMessage_GetSetRecipients(t *testing.T) {
	msg := &Message{}
	recipients := []string{"a@example.com", "b@example.com"}
	err := msg.SetRecipients(recipients)
	if err != nil {
		t.Fatalf("SetRecipients failed: %v", err)
	}
	got, err := msg.GetRecipients()
	if err != nil {
		t.Fatalf("GetRecipients failed: %v", err)
	}
	if !reflect.DeepEqual(got, recipients) {
		t.Errorf("Recipients mismatch: got %v, want %v", got, recipients)
	}
}

func TestMessage_GetSetCoordination(t *testing.T) {
	msg := &Message{}
	coord := &types.CoordinationConfig{Type: "parallel", Timeout: 10, StopOnFailure: true}
	err := msg.SetCoordination(coord)
	if err != nil {
		t.Fatalf("SetCoordination failed: %v", err)
	}
	got, err := msg.GetCoordination()
	if err != nil {
		t.Fatalf("GetCoordination failed: %v", err)
	}
	if !reflect.DeepEqual(got, coord) {
		t.Errorf("Coordination mismatch: got %v, want %v", got, coord)
	}
}

func TestMessage_GetCoordination_Empty(t *testing.T) {
	msg := &Message{}
	got, err := msg.GetCoordination()
	if err != nil {
		t.Fatalf("GetCoordination failed: %v", err)
	}
	if got != nil {
		t.Errorf("Expected nil coordination, got %v", got)
	}
}

func TestMessage_SetCoordination_Nil(t *testing.T) {
	msg := &Message{}
	coord := &types.CoordinationConfig{Type: "test"}
	_ = msg.SetCoordination(coord)
	_ = msg.SetCoordination(nil)
	if len(msg.Coordination) != 0 {
		t.Errorf("Expected Coordination to be empty, got %v", msg.Coordination)
	}
}

func TestTableNames(t *testing.T) {
	var m Message
	var ms MessageStatus
	var rs RecipientStatus
	if m.TableName() != "messages" {
		t.Errorf("Message table name incorrect")
	}
	if ms.TableName() != "message_statuses" {
		t.Errorf("MessageStatus table name incorrect")
	}
	if rs.TableName() != "recipient_statuses" {
		t.Errorf("RecipientStatus table name incorrect")
	}
}

func TestMessage_ConversionRoundTrip_PreservesWorkflowID(t *testing.T) {
	ds := &DatabaseStorage{}

	original := &types.Message{
		Version:        "1.0",
		MessageID:      "0198c5b2-0000-7000-8000-000000000010",
		IdempotencyKey: "key-1",
		Sender:         "initiator@example.com",
		Recipients:     []string{"agent1@example.com"},
		WorkflowID:     "0198c5b2-0000-7000-8000-000000000011",
	}

	dbMsg, err := ds.convertToDBMessage(original)
	if err != nil {
		t.Fatalf("convertToDBMessage failed: %v", err)
	}

	restored, err := ds.convertToTypesMessage(dbMsg)
	if err != nil {
		t.Fatalf("convertToTypesMessage failed: %v", err)
	}

	if restored.WorkflowID != original.WorkflowID {
		t.Errorf("WorkflowID after round-trip = %q, want %q", restored.WorkflowID, original.WorkflowID)
	}
}
