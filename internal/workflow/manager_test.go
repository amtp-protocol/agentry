package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/pkg/uuid"
)

type mockDispatcher struct {
	dispatched []*types.Message
}

func (m *mockDispatcher) Dispatch(ctx context.Context, msg *types.Message) error {
	m.dispatched = append(m.dispatched, msg)
	return nil
}

type mockStorage struct {
	storage.Storage
	workflows map[string]*types.Workflow
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		workflows: make(map[string]*types.Workflow),
	}
}

func (m *mockStorage) StoreWorkflow(ctx context.Context, state *types.Workflow) error {
	m.workflows[state.WorkflowID] = state
	return nil
}

func (m *mockStorage) GetWorkflow(ctx context.Context, id string) (*types.Workflow, error) {
	w, ok := m.workflows[id]
	if !ok {
		return nil, errors.New("not found")
	}

	// Create a copy to prevent in-place mutation side-effects
	wCopy := *w
	if w.CoordinationConfig != nil {
		coordCopy := *w.CoordinationConfig
		wCopy.CoordinationConfig = &coordCopy
	}
	wCopy.Participants = make([]types.WorkflowParticipant, len(w.Participants))
	copy(wCopy.Participants, w.Participants)
	if w.OriginalRecipients != nil {
		wCopy.OriginalRecipients = make([]string, len(w.OriginalRecipients))
		copy(wCopy.OriginalRecipients, w.OriginalRecipients)
	}
	if w.Payload != nil {
		wCopy.Payload = make(json.RawMessage, len(w.Payload))
		copy(wCopy.Payload, w.Payload)
	}

	return &wCopy, nil
}

func (m *mockStorage) UpdateWorkflowStatus(ctx context.Context, id string, status types.WorkflowStatus) error {
	w, ok := m.workflows[id]
	if !ok {
		return errors.New("not found")
	}
	w.Status = status
	w.Version++
	return nil
}

func (m *mockStorage) UpdateWorkflowParticipant(ctx context.Context, id string, address string, status types.ParticipantStatus, responsePayload []byte) error {
	w, ok := m.workflows[id]
	if !ok {
		return errors.New("not found")
	}
	for i, p := range w.Participants {
		if p.Address == address {
			w.Participants[i].Status = status
			w.Participants[i].ResponsePayload = responsePayload
			w.Version++
			return nil
		}
	}
	return errors.New("participant not found")
}

func (m *mockStorage) ListTimedOutWorkflows(ctx context.Context) ([]*types.Workflow, error) {
	var timeouts []*types.Workflow
	now := time.Now()
	for _, w := range m.workflows {
		if w.Status == types.WorkflowStatusInProgress || w.Status == types.WorkflowStatusPending {
			timeoutTime := w.CreatedAt.Add(time.Duration(w.TimeoutSeconds) * time.Second)
			if now.After(timeoutTime) {
				timeouts = append(timeouts, w)
			}
		}
	}
	return timeouts, nil
}

func (m *mockStorage) UpdateWorkflowParticipantAtomic(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte, expectedVersion int) error {
	return m.UpdateWorkflowParticipant(ctx, workflowID, address, status, responsePayload)
}

func (m *mockStorage) UpdateWorkflowStatusAtomic(ctx context.Context, workflowID string, status types.WorkflowStatus, expectedVersion int) error {
	return m.UpdateWorkflowStatus(ctx, workflowID, status)
}

func TestManager_Initialize(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	ctx := context.Background()

	msg := &types.Message{
		MessageID: "msg-1",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"agent-1", "agent-2"},
		},
	}

	workflow, err := mgr.Initialize(ctx, msg)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	if workflow.WorkflowID == "" || workflow.WorkflowID == "msg-1" {
		t.Errorf("Expected a generated workflow ID, not the message ID")
	}

	w, err := st.GetWorkflow(ctx, workflow.WorkflowID)
	if err != nil {
		t.Fatalf("Workflow not saved")
	}

	if w.Status != types.WorkflowStatusInProgress {
		t.Errorf("Expected status in_progress, got %v", w.Status)
	}

	if len(w.Participants) != 2 {
		t.Errorf("Expected 2 participants")
	}

	if len(dp.dispatched) != 1 {
		t.Fatalf("Expected 1 dispatch call for parallel")
	}

	// Verify template fields are persisted
	if w.CoordinationConfig == nil {
		t.Errorf("CoordinationConfig should be persisted")
	}
	if w.Sender != msg.Sender {
		t.Errorf("Sender should be persisted")
	}
}

func TestManager_Initialize_Sequential(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID:  "msg-seq",
		Recipients: []string{"ignored"}, // in sequential it overrides recipients
		Coordination: &types.CoordinationConfig{
			Type:     "sequential",
			Sequence: []string{"agent-1", "agent-2"},
		},
	}

	_, err := mgr.Initialize(context.Background(), msg)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if len(dp.dispatched) != 1 {
		t.Fatalf("Expected 1 dispatch call")
	}

	dmsg := dp.dispatched[0]
	if len(dmsg.Recipients) != 1 || dmsg.Recipients[0] != "agent-1" {
		t.Errorf("Expected dispatch to agent-1, got %v", dmsg.Recipients)
	}
}

func TestManager_Initialize_Conditional(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID:  "msg-cond",
		Recipients: []string{"evaluator-agent"},
		Coordination: &types.CoordinationConfig{
			Type: "conditional",
			Conditions: []types.ConditionalRule{
				{
					If:   "result == 'ok'",
					Then: []string{"agent-succ"},
				},
			},
		},
	}

	_, err := mgr.Initialize(context.Background(), msg)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if len(dp.dispatched) != 1 {
		t.Fatalf("Expected 1 dispatch to evaluator")
	}
}

func TestManager_ProcessResponse_Parallel(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID: "msg-p",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1", "a2"},
		},
	}

	wf, _ := mgr.Initialize(context.Background(), msg)
	wfID := wf.WorkflowID
	dp.dispatched = nil // reset

	reply1 := &types.Message{
		Sender:    "a1",
		InReplyTo: wfID,
		Payload:   json.RawMessage(`{}`),
	}

	err := mgr.ProcessResponse(context.Background(), wfID, reply1)
	if err != nil {
		t.Fatalf("ProcessResponse failed: %v", err)
	}

	w, _ := st.GetWorkflow(context.Background(), wfID)
	if w.Status == types.WorkflowStatusCompleted {
		t.Errorf("Workflow should not be completed yet")
	}

	reply2 := &types.Message{
		Sender:    "a2",
		InReplyTo: wfID,
		Payload:   json.RawMessage(`{}`),
	}
	mgr.ProcessResponse(context.Background(), wfID, reply2)

	w, _ = st.GetWorkflow(context.Background(), wfID)
	if w.Status != types.WorkflowStatusCompleted {
		t.Errorf("Workflow should be completed now")
	}
}

func TestManager_ProcessResponse_Sequential(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID: "msg-s",
		Sender:    "test@localhost",
		Coordination: &types.CoordinationConfig{
			Type:     "sequential",
			Sequence: []string{"a1", "a2"},
		},
	}

	wf, _ := mgr.Initialize(context.Background(), msg)
	wfID := wf.WorkflowID
	dp.dispatched = nil

	reply1 := &types.Message{
		Sender:    "a1",
		InReplyTo: wfID,
	}

	err := mgr.ProcessResponse(context.Background(), wfID, reply1)
	if err != nil {
		t.Fatalf("ProcessResponse failed: %v", err)
	}

	if len(dp.dispatched) != 1 {
		t.Fatalf("Should dispatch to a2")
	}

	reply2 := &types.Message{
		Sender:    "a2",
		InReplyTo: wfID,
	}
	mgr.ProcessResponse(context.Background(), wfID, reply2)

	w, _ := st.GetWorkflow(context.Background(), wfID)
	if w.Status != types.WorkflowStatusCompleted {
		t.Errorf("Should be completed")
	}
}

func TestManager_ProcessResponse_Conditional(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID:  "msg-c",
		Recipients: []string{"eval"},
		Coordination: &types.CoordinationConfig{
			Type: "conditional",
			Conditions: []types.ConditionalRule{
				{
					If:   "status == \"ok\"",
					Then: []string{"a1"},
					Else: []string{"a2"},
				},
			},
		},
	}

	wf, _ := mgr.Initialize(context.Background(), msg)
	wfID := wf.WorkflowID
	dp.dispatched = nil

	reply1 := &types.Message{
		Sender:    "eval",
		InReplyTo: wfID,
		Payload:   json.RawMessage(`{"status":"ok"}`),
	}

	err := mgr.ProcessResponse(context.Background(), wfID, reply1)
	if err != nil {
		t.Fatalf("ProcessResponse failed: %v", err)
	}

	if len(dp.dispatched) != 1 {
		t.Fatalf("Should dispatch to a1, got %d dispatches", len(dp.dispatched))
	}
	if dp.dispatched[0].Recipients[0] != "a1" {
		t.Errorf("Expected dispatch to a1")
	}

	reply2 := &types.Message{
		Sender:    "a1",
		InReplyTo: wfID,
	}
	mgr.ProcessResponse(context.Background(), wfID, reply2)

	w, _ := st.GetWorkflow(context.Background(), wfID)
	if w.Status != types.WorkflowStatusCompleted {
		t.Errorf("Should be completed")
	}
}

func TestManager_TimeoutSweeper(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID: "msg-t",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1"},
			Timeout:           1, // 1 sec
		},
	}

	wf, err := mgr.Initialize(context.Background(), msg)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	wfID := wf.WorkflowID

	// artificially backdate the workflow
	st.workflows[wfID].CreatedAt = time.Now().Add(-2 * time.Second)

	// Start sweeper in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// manually invoke to avoid timing issues in tests
	mgr.(*managerImpl).sweepTimeouts(ctx)

	w, _ := st.GetWorkflow(context.Background(), wfID)
	if w.Status != types.WorkflowStatusTimeout {
		t.Errorf("Expected status to be timeout, got %v", w.Status)
	}

	mgr.Stop()
}

func TestManager_ProcessResponse_WorkflowNotFound(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	err := mgr.ProcessResponse(context.Background(), "unknown", &types.Message{})
	if err == nil {
		t.Errorf("Expected error for missing workflow")
	}
}

func TestManager_ProcessResponse_StopOnFailure(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID: "msg-fail",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1", "a2"},
			StopOnFailure:     true,
		},
	}

	wf, _ := mgr.Initialize(context.Background(), msg)
	wfID := wf.WorkflowID

	reply1 := &types.Message{
		Sender:       "a1",
		InReplyTo:    wfID,
		ResponseType: "error", // Triggers failure
	}

	mgr.ProcessResponse(context.Background(), wfID, reply1)

	w, _ := st.GetWorkflow(context.Background(), wfID)
	if w.Status != types.WorkflowStatusFailed {
		t.Errorf("Expected failure state due to StopOnFailure")
	}
}

func TestManager_NotifySenderOnCompletion(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID: "msg-notify",
		Sender:    "initiator@localhost",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1"},
		},
	}

	wf, _ := mgr.Initialize(context.Background(), msg)
	wfID := wf.WorkflowID

	// Reset dispatch counter after Initialize
	dp.dispatched = nil

	// Send the sole participant response → terminal transition
	mgr.ProcessResponse(context.Background(), wfID, &types.Message{
		Sender:    "a1",
		InReplyTo: wfID,
		Payload:   json.RawMessage(`{"status":"ok"}`),
	})

	// Should have exactly one dispatch: the notification to the initiator
	if len(dp.dispatched) != 1 {
		t.Fatalf("Expected 1 notification dispatch, got %d", len(dp.dispatched))
	}
	notif := dp.dispatched[0]
	if notif.Sender != "workflow@localhost" {
		t.Errorf("Notification sender should be the configured system sender, got %q", notif.Sender)
	}
	if len(notif.Recipients) != 1 || notif.Recipients[0] != "initiator@localhost" {
		t.Errorf("Notification should go to initiator@localhost, got %v", notif.Recipients)
	}
	if notif.InReplyTo != wfID {
		t.Errorf("Notification InReplyTo should match workflow ID, got %q", notif.InReplyTo)
	}
	// Verify aggregated payload contains results
	var payload map[string]interface{}
	if err := json.Unmarshal(notif.Payload, &payload); err != nil {
		t.Fatalf("Notification payload should be valid JSON: %v", err)
	}
	if payload["workflow_id"] != wfID {
		t.Errorf("Payload workflow_id mismatch")
	}
	results, ok := payload["results"].([]interface{})
	if !ok || len(results) != 1 {
		t.Errorf("Payload results should have 1 entry, got %v", results)
	}
}

func TestManager_NotifySenderOnTimeout(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp, nil, "workflow@localhost")

	msg := &types.Message{
		MessageID: "msg-timeout-notify",
		Sender:    "initiator@localhost",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1"},
			Timeout:           1,
		},
	}

	wf, _ := mgr.Initialize(context.Background(), msg)
	wfID := wf.WorkflowID
	dp.dispatched = nil

	// Backdate so it's past deadline
	st.workflows[wfID].CreatedAt = time.Now().Add(-2 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.(*managerImpl).sweepTimeouts(ctx)

	// Should have one notification dispatch
	if len(dp.dispatched) != 1 {
		t.Fatalf("Expected 1 timeout notification dispatch, got %d", len(dp.dispatched))
	}
	notif := dp.dispatched[0]
	if notif.InReplyTo != wfID {
		t.Errorf("Notification InReplyTo should match workflow ID, got %q", notif.InReplyTo)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(notif.Payload, &payload); err != nil {
		t.Fatalf("Notification payload should be valid JSON: %v", err)
	}
	if payload["status"] != "timeout" {
		t.Errorf("Expected timeout status in notification, got %v", payload["status"])
	}

	mgr.Stop()
}

func TestManager_DispatchedMessagesCarryWorkflowID(t *testing.T) {
	t.Run("parallel initial dispatch", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID: "msg-wfid-p",
			Coordination: &types.CoordinationConfig{
				Type:              "parallel",
				RequiredResponses: []string{"a1", "a2"},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected 1 dispatch, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].WorkflowID != wf.WorkflowID {
			t.Errorf("Dispatched message WorkflowID = %q, want %q", dp.dispatched[0].WorkflowID, wf.WorkflowID)
		}
	})

	t.Run("sequential initial and next-step dispatch", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID: "msg-wfid-s",
			Coordination: &types.CoordinationConfig{
				Type:     "sequential",
				Sequence: []string{"a1", "a2"},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected 1 dispatch, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].WorkflowID != wf.WorkflowID {
			t.Errorf("Initial dispatch WorkflowID = %q, want %q", dp.dispatched[0].WorkflowID, wf.WorkflowID)
		}

		dp.dispatched = nil
		reply := &types.Message{Sender: "a1", WorkflowID: wf.WorkflowID}
		if err := mgr.ProcessResponse(context.Background(), wf.WorkflowID, reply); err != nil {
			t.Fatalf("ProcessResponse failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected next-step dispatch to a2, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].WorkflowID != wf.WorkflowID {
			t.Errorf("Next-step dispatch WorkflowID = %q, want %q", dp.dispatched[0].WorkflowID, wf.WorkflowID)
		}
	})

	t.Run("conditional initial and branch dispatch", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID:  "msg-wfid-c",
			Recipients: []string{"eval"},
			Coordination: &types.CoordinationConfig{
				Type: "conditional",
				Conditions: []types.ConditionalRule{
					{
						If:   "status == \"ok\"",
						Then: []string{"a1"},
					},
				},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected 1 dispatch to evaluator, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].WorkflowID != wf.WorkflowID {
			t.Errorf("Evaluator dispatch WorkflowID = %q, want %q", dp.dispatched[0].WorkflowID, wf.WorkflowID)
		}

		dp.dispatched = nil
		reply := &types.Message{
			Sender:     "eval",
			WorkflowID: wf.WorkflowID,
			Payload:    json.RawMessage(`{"status":"ok"}`),
		}
		if err := mgr.ProcessResponse(context.Background(), wf.WorkflowID, reply); err != nil {
			t.Fatalf("ProcessResponse failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected branch dispatch to a1, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].WorkflowID != wf.WorkflowID {
			t.Errorf("Branch dispatch WorkflowID = %q, want %q", dp.dispatched[0].WorkflowID, wf.WorkflowID)
		}
	})

	t.Run("completion notification", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID: "msg-wfid-n",
			Sender:    "initiator@localhost",
			Coordination: &types.CoordinationConfig{
				Type:              "parallel",
				RequiredResponses: []string{"a1"},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		dp.dispatched = nil

		reply := &types.Message{Sender: "a1", WorkflowID: wf.WorkflowID}
		if err := mgr.ProcessResponse(context.Background(), wf.WorkflowID, reply); err != nil {
			t.Fatalf("ProcessResponse failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected 1 notification dispatch, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].WorkflowID != wf.WorkflowID {
			t.Errorf("Notification WorkflowID = %q, want %q", dp.dispatched[0].WorkflowID, wf.WorkflowID)
		}
	})
}

func TestManager_EngineDispatchedMessagesAreValid(t *testing.T) {
	assertValidDispatch := func(t *testing.T, m *types.Message) {
		t.Helper()
		if err := m.Validate(); err != nil {
			t.Errorf("Dispatched message is not a valid protocol message: %v", err)
		}
		if !uuid.IsValidV7(m.MessageID) {
			t.Errorf("Dispatched message ID %q is not a valid UUIDv7", m.MessageID)
		}
		if m.Version != "1.0" {
			t.Errorf("Dispatched message version = %q, want 1.0", m.Version)
		}
		if m.Timestamp.IsZero() {
			t.Errorf("Dispatched message timestamp is zero")
		}
	}

	t.Run("sequential steps get fresh identities", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID: "msg-valid-s",
			Sender:    "initiator@localhost",
			Coordination: &types.CoordinationConfig{
				Type:     "sequential",
				Sequence: []string{"a1", "a2"},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected 1 dispatch, got %d", len(dp.dispatched))
		}
		first := dp.dispatched[0]
		assertValidDispatch(t, first)
		if first.Sender != "initiator@localhost" {
			t.Errorf("Step sender = %q, want the workflow initiator", first.Sender)
		}

		dp.dispatched = nil
		reply := &types.Message{Sender: "a1", WorkflowID: wf.WorkflowID}
		if err := mgr.ProcessResponse(context.Background(), wf.WorkflowID, reply); err != nil {
			t.Fatalf("ProcessResponse failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected next-step dispatch, got %d", len(dp.dispatched))
		}
		second := dp.dispatched[0]
		assertValidDispatch(t, second)
		if second.MessageID == first.MessageID {
			t.Errorf("Each step dispatch should get its own message ID")
		}
		if second.IdempotencyKey == first.IdempotencyKey {
			t.Errorf("Each step dispatch should get its own idempotency key")
		}
	})

	t.Run("sequential step idempotency key is stable across re-dispatch", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID: "msg-valid-s2",
			Sender:    "initiator@localhost",
			Coordination: &types.CoordinationConfig{
				Type:     "sequential",
				Sequence: []string{"a1"},
			},
		}

		wf, _ := mgr.Initialize(context.Background(), msg)

		// Re-dispatching the same step of the same workflow must reuse the
		// idempotency key so receiving gateways can deduplicate.
		if err := mgr.(*managerImpl).executeSequentialNext(context.Background(), wf, wf.CoordinationConfig, 0); err != nil {
			t.Fatalf("re-dispatch failed: %v", err)
		}
		if len(dp.dispatched) != 2 {
			t.Fatalf("Expected 2 dispatches, got %d", len(dp.dispatched))
		}
		if dp.dispatched[0].IdempotencyKey == "" {
			t.Fatalf("Step dispatch should carry an idempotency key")
		}
		if dp.dispatched[0].IdempotencyKey != dp.dispatched[1].IdempotencyKey {
			t.Errorf("Same step re-dispatch should reuse idempotency key: %q vs %q",
				dp.dispatched[0].IdempotencyKey, dp.dispatched[1].IdempotencyKey)
		}
	})

	t.Run("conditional branch dispatch is valid", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID:  "msg-valid-c",
			Sender:     "initiator@localhost",
			Recipients: []string{"eval"},
			Coordination: &types.CoordinationConfig{
				Type: "conditional",
				Conditions: []types.ConditionalRule{
					{
						If:   "status == \"ok\"",
						Then: []string{"a1"},
					},
				},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		dp.dispatched = nil

		reply := &types.Message{
			Sender:     "eval",
			WorkflowID: wf.WorkflowID,
			Payload:    json.RawMessage(`{"status":"ok"}`),
		}
		if err := mgr.ProcessResponse(context.Background(), wf.WorkflowID, reply); err != nil {
			t.Fatalf("ProcessResponse failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected branch dispatch, got %d", len(dp.dispatched))
		}
		assertValidDispatch(t, dp.dispatched[0])
	})

	t.Run("notification has system sender and full identity", func(t *testing.T) {
		st := newMockStorage()
		dp := &mockDispatcher{}
		mgr := NewManager(st, dp, nil, "workflow@localhost")

		msg := &types.Message{
			MessageID: "msg-valid-n",
			Sender:    "initiator@localhost",
			Coordination: &types.CoordinationConfig{
				Type:              "parallel",
				RequiredResponses: []string{"a1"},
			},
		}

		wf, err := mgr.Initialize(context.Background(), msg)
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		dp.dispatched = nil

		reply := &types.Message{Sender: "a1", WorkflowID: wf.WorkflowID}
		if err := mgr.ProcessResponse(context.Background(), wf.WorkflowID, reply); err != nil {
			t.Fatalf("ProcessResponse failed: %v", err)
		}
		if len(dp.dispatched) != 1 {
			t.Fatalf("Expected 1 notification dispatch, got %d", len(dp.dispatched))
		}
		notif := dp.dispatched[0]
		assertValidDispatch(t, notif)
		if notif.Sender != "workflow@localhost" {
			t.Errorf("Notification sender = %q, want the configured system sender", notif.Sender)
		}
	})
}
