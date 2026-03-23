package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
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
	messages  map[string]*types.Message
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		workflows: make(map[string]*types.Workflow),
		messages:  make(map[string]*types.Message),
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
	return w, nil
}

func (m *mockStorage) UpdateWorkflowStatus(ctx context.Context, id string, status types.WorkflowStatus) error {
	w, ok := m.workflows[id]
	if !ok {
		return errors.New("not found")
	}
	w.Status = status
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

func (m *mockStorage) GetMessage(ctx context.Context, id string) (*types.Message, error) {
	msg, ok := m.messages[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return msg, nil
}

func (m *mockStorage) SaveMessage(ctx context.Context, msg *types.Message) error {
	m.messages[msg.MessageID] = msg
	return nil
}

func TestManager_Initialize(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

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

	if workflow.WorkflowID != "msg-1" {
		t.Errorf("Expected workflow ID to be msg-1")
	}

	w, err := st.GetWorkflow(ctx, "msg-1")
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
}

func TestManager_Initialize_Sequential(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

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
	mgr := NewManager(st, dp)

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
	mgr := NewManager(st, dp)

	msg := &types.Message{
		MessageID: "msg-p",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1", "a2"},
		},
	}

	st.SaveMessage(context.Background(), msg)
	mgr.Initialize(context.Background(), msg)
	dp.dispatched = nil // reset

	reply1 := &types.Message{
		Sender:    "a1",
		InReplyTo: "msg-p",
		Payload:   json.RawMessage(`{}`),
	}

	err := mgr.ProcessResponse(context.Background(), "msg-p", reply1)
	if err != nil {
		t.Fatalf("ProcessResponse failed: %v", err)
	}

	w, _ := st.GetWorkflow(context.Background(), "msg-p")
	if w.Status == types.WorkflowStatusCompleted {
		t.Errorf("Workflow should not be completed yet")
	}

	reply2 := &types.Message{
		Sender:    "a2",
		InReplyTo: "msg-p",
		Payload:   json.RawMessage(`{}`),
	}
	mgr.ProcessResponse(context.Background(), "msg-p", reply2)

	w, _ = st.GetWorkflow(context.Background(), "msg-p")
	if w.Status != types.WorkflowStatusCompleted {
		t.Errorf("Workflow should be completed now")
	}
}

func TestManager_ProcessResponse_Sequential(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

	msg := &types.Message{
		MessageID: "msg-s",
		Coordination: &types.CoordinationConfig{
			Type:     "sequential",
			Sequence: []string{"a1", "a2"},
		},
	}

	st.SaveMessage(context.Background(), msg)
	mgr.Initialize(context.Background(), msg)
	dp.dispatched = nil

	reply1 := &types.Message{
		Sender:    "a1",
		InReplyTo: "msg-s",
	}

	err := mgr.ProcessResponse(context.Background(), "msg-s", reply1)
	if err != nil {
		t.Fatalf("ProcessResponse failed: %v", err)
	}

	if len(dp.dispatched) != 1 {
		t.Fatalf("Should dispatch to a2")
	}

	reply2 := &types.Message{
		Sender:    "a2",
		InReplyTo: "msg-s",
	}
	mgr.ProcessResponse(context.Background(), "msg-s", reply2)

	w, _ := st.GetWorkflow(context.Background(), "msg-s")
	if w.Status != types.WorkflowStatusCompleted {
		t.Errorf("Should be completed")
	}
}

func TestManager_ProcessResponse_Conditional(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

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

	st.SaveMessage(context.Background(), msg)
	mgr.Initialize(context.Background(), msg)
	dp.dispatched = nil

	reply1 := &types.Message{
		Sender:    "eval",
		InReplyTo: "msg-c",
		Payload:   json.RawMessage(`{"status":"ok"}`),
	}

	err := mgr.ProcessResponse(context.Background(), "msg-c", reply1)
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
		InReplyTo: "msg-c",
	}
	mgr.ProcessResponse(context.Background(), "msg-c", reply2)

	w, _ := st.GetWorkflow(context.Background(), "msg-c")
	if w.Status != types.WorkflowStatusCompleted {
		t.Errorf("Should be completed")
	}
}

func TestManager_TimeoutSweeper(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

	msg := &types.Message{
		MessageID: "msg-t",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1"},
			Timeout:           1, // 1 sec
		},
	}

	mgr.Initialize(context.Background(), msg)

	// artificially backdate the workflow
	w, _ := st.GetWorkflow(context.Background(), "msg-t")
	w.CreatedAt = time.Now().Add(-2 * time.Second)

	// Start sweeper in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// manually invoke to avoid timing issues in tests
	mgr.(*managerImpl).sweepTimeouts(ctx)

	w, _ = st.GetWorkflow(context.Background(), "msg-t")
	if w.Status != types.WorkflowStatusTimeout {
		t.Errorf("Expected status to be timeout, got %v", w.Status)
	}

	mgr.Stop()
}

func TestManager_ProcessResponse_WorkflowNotFound(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

	err := mgr.ProcessResponse(context.Background(), "unknown", &types.Message{})
	if err == nil {
		t.Errorf("Expected error for missing workflow")
	}
}

func TestManager_ProcessResponse_StopOnFailure(t *testing.T) {
	st := newMockStorage()
	dp := &mockDispatcher{}
	mgr := NewManager(st, dp)

	msg := &types.Message{
		MessageID: "msg-fail",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			RequiredResponses: []string{"a1", "a2"},
			StopOnFailure:     true,
		},
	}

	st.SaveMessage(context.Background(), msg)
	mgr.Initialize(context.Background(), msg)

	reply1 := &types.Message{
		Sender:       "a1",
		InReplyTo:    "msg-fail",
		ResponseType: "error", // Triggers failure
	}

	mgr.ProcessResponse(context.Background(), "msg-fail", reply1)

	w, _ := st.GetWorkflow(context.Background(), "msg-fail")
	if w.Status != types.WorkflowStatusFailed {
		t.Errorf("Expected failure state due to StopOnFailure")
	}
}
