package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/logging"
	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/pkg/uuid"
)

type managerImpl struct {
	storage    storage.Storage
	dispatcher Dispatcher
	logger     *logging.Logger
	doneChan   chan struct{}
	stopOnce   sync.Once
}

func NewManager(s storage.Storage, d Dispatcher, logger *logging.Logger) Manager {
	if logger == nil {
		logger = logging.NewNoopLogger()
	}
	return &managerImpl{
		storage:    s,
		dispatcher: d,
		logger:     logger,
		doneChan:   make(chan struct{}),
	}
}

func (m *managerImpl) Initialize(ctx context.Context, msg *types.Message) (*types.Workflow, error) {
	if msg.Coordination == nil {
		return nil, fmt.Errorf("message does not contain coordination config")
	}
	workflowID, err := uuid.GenerateV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate workflow ID: %w", err)
	}

	now := time.Now()
	workflow := &types.Workflow{
		WorkflowID:       workflowID,
		Status:           types.WorkflowStatusPending,
		CoordinationType: msg.Coordination.Type,
		TimeoutSeconds:   msg.Coordination.Timeout,
		CreatedAt:        now,
		UpdatedAt:        now,
		// Persist coordination + template so evaluateWorkflow never needs GetMessage
		CoordinationConfig: msg.Coordination,
		OriginalRecipients: msg.Recipients,
		Sender:             msg.Sender,
		Subject:            msg.Subject,
		Schema:             msg.Schema,
		Payload:            msg.Payload,
	}

	if workflow.TimeoutSeconds <= 0 {
		workflow.TimeoutSeconds = 3600 // Default 1 hour if not specified
	}

	deadline := workflow.CreatedAt.Add(time.Duration(workflow.TimeoutSeconds) * time.Second)
	workflow.Deadline = &deadline

	// Calculate participants based on coordination type
	var participants []string
	switch msg.Coordination.Type {
	case "parallel":
		participants = append(participants, msg.Coordination.RequiredResponses...)
		participants = append(participants, msg.Coordination.OptionalResponses...)
	case "sequential":
		participants = append(participants, msg.Coordination.Sequence...)
	case "conditional":
		// Add initial participants and all conditional branches
		participants = append(participants, msg.Recipients...)
		for _, condition := range msg.Coordination.Conditions {
			participants = append(participants, condition.Then...)
			participants = append(participants, condition.Else...)
		}
	default:
		return nil, fmt.Errorf("unsupported coordination type: %s", msg.Coordination.Type)
	}

	workflow.Participants = make([]types.WorkflowParticipant, 0)
	dedup := make(map[string]bool)
	for _, p := range participants { // Simple deduplication for state tracking
		if _, ok := dedup[p]; !ok {
			dedup[p] = true
			workflow.Participants = append(workflow.Participants, types.WorkflowParticipant{
				WorkflowID: workflow.WorkflowID,
				Address:    p,
				Status:     types.ParticipantStatusPending,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			})
		}
	}

	err = m.storage.StoreWorkflow(ctx, workflow)
	if err != nil {
		return nil, fmt.Errorf("failed to store workflow state: %w", err)
	}

	// Begin execution based on type
	err = m.startExecution(ctx, workflow, msg)
	if err != nil {
		updateErr := m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusFailed)
		if updateErr != nil {
			m.logger.Error("Failed to gracefully update workflow status tracking failure", updateErr)
		}
		return workflow, err
	}

	return workflow, nil
}

func (m *managerImpl) startExecution(ctx context.Context, workflow *types.Workflow, msg *types.Message) error {
	err := m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusInProgress)
	if err != nil {
		return err
	}

	// Dispatch mechanics
	switch workflow.CoordinationType {
	case "parallel":
		return m.executeParallel(ctx, workflow, msg)
	case "sequential":
		return m.executeSequentialNext(ctx, workflow, workflow.CoordinationConfig, 0)
	case "conditional":
		return m.executeConditional(ctx, workflow, msg)
	}
	return nil
}

// executeParallel sends message to all participants at once
func (m *managerImpl) executeParallel(ctx context.Context, _ *types.Workflow, msg *types.Message) error {
	msgCopy := msg.Clone()
	msgCopy.Recipients = append(msg.Coordination.RequiredResponses, msg.Coordination.OptionalResponses...)

	// We pass down to the dispatcher. The dispatcher should route the message properly.
	return m.dispatcher.Dispatch(ctx, msgCopy)
}

// executeSequentialNext dispatches to the N-th participant in the sequence
func (m *managerImpl) executeSequentialNext(ctx context.Context, workflow *types.Workflow, coord *types.CoordinationConfig, index int) error {
	if index >= len(coord.Sequence) {
		return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusCompleted)
	}

	nextAgent := coord.Sequence[index]
	msgCopy := m.buildTemplateMessage(workflow)
	msgCopy.Recipients = []string{nextAgent}

	return m.dispatcher.Dispatch(ctx, msgCopy)
}

func (m *managerImpl) executeConditional(ctx context.Context, _ *types.Workflow, msg *types.Message) error {
	return m.dispatcher.Dispatch(ctx, msg)
}

func (m *managerImpl) ProcessResponse(ctx context.Context, workflowID string, replyMsg *types.Message) error {
	for {
		workflow, err := m.storage.GetWorkflow(ctx, workflowID)
		if err != nil {
			if errors.Is(err, storage.ErrWorkflowNotFound) {
				return err
			}
			return fmt.Errorf("failed to get workflow: %w", err)
		}

		// Terminal state — nothing to do
		if workflow.Status == types.WorkflowStatusCompleted ||
			workflow.Status == types.WorkflowStatusFailed ||
			workflow.Status == types.WorkflowStatusTimeout {
			return nil
		}

		// Duplicate response — sender already in a final state
		if !m.isParticipantPending(workflow, replyMsg.Sender) {
			return nil
		}

		participantStatus := types.ParticipantStatusCompleted
		if replyMsg.ResponseType == "workflow_error" || replyMsg.ResponseType == "error" {
			participantStatus = types.ParticipantStatusFailed
		}

		// Atomic update: only succeeds if no concurrent write bumped the version
		err = m.storage.UpdateWorkflowParticipantAtomic(ctx, workflowID, replyMsg.Sender, participantStatus, replyMsg.Payload, workflow.Version)
		if errors.Is(err, storage.ErrVersionConflict) {
			continue // concurrent write — retry
		}
		if err != nil {
			return fmt.Errorf("failed to update participant status: %w", err)
		}

		err = m.evaluateWorkflow(ctx, workflowID, replyMsg)
		if errors.Is(err, storage.ErrVersionConflict) {
			continue
		}
		return err
	}
}

// isParticipantPending returns true if the participant is in the workflow and
// still pending.
func (m *managerImpl) isParticipantPending(wf *types.Workflow, address string) bool {
	for _, p := range wf.Participants {
		if p.Address == address {
			return p.Status == types.ParticipantStatusPending
		}
	}
	return false
}

// buildTemplateMessage constructs a minimal Message from the workflow's stored
// sender/subject/schema/payload, suitable as a dispatch template for sequential/conditional
// branching. Recipients and coordination are set by the caller.
func (m *managerImpl) buildTemplateMessage(wf *types.Workflow) *types.Message {
	return &types.Message{
		Sender:  wf.Sender,
		Subject: wf.Subject,
		Schema:  wf.Schema,
		Payload: wf.Payload,
	}
}

// notifySender dispatches an aggregated completion/failure notification back to
// the workflow's original sender so they can observe the outcome without polling
// the storage database.
func (m *managerImpl) notifySender(ctx context.Context, wf *types.Workflow, finalStatus types.WorkflowStatus) {
	if wf.Sender == "" {
		return
	}

	results := make([]map[string]interface{}, 0, len(wf.Participants))
	for _, p := range wf.Participants {
		e := map[string]interface{}{
			"address": p.Address,
			"status":  string(p.Status),
		}
		if len(p.ResponsePayload) > 0 {
			var v interface{}
			if err := json.Unmarshal(p.ResponsePayload, &v); err == nil {
				e["payload"] = v
			}
		}
		results = append(results, e)
	}
	aggPayload, _ := json.Marshal(map[string]interface{}{
		"workflow_id":       wf.WorkflowID,
		"coordination_type": wf.CoordinationType,
		"status":            string(finalStatus),
		"results":           results,
	})

	notif := &types.Message{
		Sender:     "", // system-generated
		Recipients: []string{wf.Sender},
		Subject:    fmt.Sprintf("Workflow %s: %s", wf.WorkflowID, finalStatus),
		InReplyTo:  wf.WorkflowID,
		Payload:    json.RawMessage(aggPayload),
	}
	if err := m.dispatcher.Dispatch(ctx, notif); err != nil {
		m.logger.Errorf(err, "Failed to notify workflow sender %s of %s", wf.Sender, finalStatus)
	}
}

func (m *managerImpl) evaluateWorkflow(ctx context.Context, workflowID string, replyMsg *types.Message) error {
	workflow, err := m.storage.GetWorkflow(ctx, workflowID)
	if err != nil {
		return err
	}
	coord := workflow.CoordinationConfig

	allDone := true
	anyFailed := false
	for _, p := range workflow.Participants {
		if p.Status == types.ParticipantStatusPending {
			allDone = false
		}
		if p.Status == types.ParticipantStatusFailed {
			anyFailed = true
		}
	}

	// Basic failure handling
	stopOnFailure := coord != nil && coord.StopOnFailure
	if anyFailed && stopOnFailure {
		err := m.storage.UpdateWorkflowStatusAtomic(ctx, workflow.WorkflowID, types.WorkflowStatusFailed, workflow.Version)
		if errors.Is(err, storage.ErrVersionConflict) {
			return err
		}
		if err == nil {
			m.notifySender(ctx, workflow, types.WorkflowStatusFailed)
		}
		return err
	}

	if workflow.CoordinationType == "parallel" {
		if allDone {
			finalStatus := types.WorkflowStatusCompleted
			if anyFailed {
				finalStatus = types.WorkflowStatusFailed
			}
			err := m.storage.UpdateWorkflowStatusAtomic(ctx, workflow.WorkflowID, finalStatus, workflow.Version)
			if errors.Is(err, storage.ErrVersionConflict) {
				return err
			}
			if err == nil {
				m.notifySender(ctx, workflow, finalStatus)
			}
			return err
		}
	} else if workflow.CoordinationType == "sequential" {
		if coord == nil || len(coord.Sequence) == 0 {
			err := m.storage.UpdateWorkflowStatusAtomic(ctx, workflow.WorkflowID, types.WorkflowStatusCompleted, workflow.Version)
			if errors.Is(err, storage.ErrVersionConflict) {
				return err
			}
			if err == nil {
				m.notifySender(ctx, workflow, types.WorkflowStatusCompleted)
			}
			return err
		}

		// Find the first participant in the sequence that is still pending
		statusMap := make(map[string]types.ParticipantStatus)
		for _, p := range workflow.Participants {
			statusMap[p.Address] = p.Status
		}

		nextIndex := -1
		for i, seqAddr := range coord.Sequence {
			if statusMap[seqAddr] == types.ParticipantStatusPending {
				nextIndex = i
				break
			}
		}

		if nextIndex != -1 {
			return m.executeSequentialNext(ctx, workflow, coord, nextIndex)
		}

		// all finished
		finalStatus := types.WorkflowStatusCompleted
		if anyFailed {
			finalStatus = types.WorkflowStatusFailed
		}
		err := m.storage.UpdateWorkflowStatusAtomic(ctx, workflow.WorkflowID, finalStatus, workflow.Version)
		if errors.Is(err, storage.ErrVersionConflict) {
			return err
		}
		if err == nil {
			m.notifySender(ctx, workflow, finalStatus)
		}
		return err
	} else if workflow.CoordinationType == "conditional" {
		if coord == nil {
			err := m.storage.UpdateWorkflowStatusAtomic(ctx, workflow.WorkflowID, types.WorkflowStatusCompleted, workflow.Version)
			if errors.Is(err, storage.ErrVersionConflict) {
				return err
			}
			if err == nil {
				m.notifySender(ctx, workflow, types.WorkflowStatusCompleted)
			}
			return err
		}

		// Only evaluate condition if the reply is from an initial recipient
		if replyMsg != nil {
			isInitial := false
			for _, rec := range workflow.OriginalRecipients {
				if rec == replyMsg.Sender {
					isInitial = true
					break
				}
			}

			if isInitial && len(replyMsg.Payload) > 0 {
				var payload map[string]interface{}
				if err := json.Unmarshal(replyMsg.Payload, &payload); err != nil {
					m.logger.Error("Failed to unmarshal payload for conditional evaluation", err)
				}

				for _, condition := range coord.Conditions {
					matched := false
					if strings.Contains(condition.If, " == ") {
						parts := strings.Split(condition.If, " == ")
						if len(parts) == 2 {
							key := strings.TrimSpace(parts[0])
							val := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
							if v, ok := payload[key]; ok {
								matched = (fmt.Sprintf("%v", v) == val)
							}
						}
					} else {
						matched = strings.Contains(string(replyMsg.Payload), condition.If)
					}

					var targets []string
					var skipped []string
					if matched {
						targets = condition.Then
						skipped = condition.Else
					} else {
						targets = condition.Else
						skipped = condition.Then
					}

					for _, s := range skipped {
						if err := m.storage.UpdateWorkflowParticipant(ctx, workflow.WorkflowID, s, types.ParticipantStatusCompleted, []byte(`{"status":"skipped"}`)); err != nil {
							m.logger.Errorf(err, "Failed to update status for skipped participant %s", s)
						}
					}

					if len(targets) > 0 {
						msgCopy := m.buildTemplateMessage(workflow)
						msgCopy.Recipients = targets
						if err := m.dispatcher.Dispatch(ctx, msgCopy); err != nil {
							m.logger.Error("Failed to dispatch conditional branch messages", err)
						}
					}
				}
			}
		}

		// Re-fetch to evaluate final status, in case participants got skipped
		workflow, err = m.storage.GetWorkflow(ctx, workflow.WorkflowID)
		if err != nil {
			return err
		}

		allCondDone := true
		anyCondFailed := false
		for _, p := range workflow.Participants {
			if p.Status == types.ParticipantStatusPending {
				allCondDone = false
			}
			if p.Status == types.ParticipantStatusFailed {
				anyCondFailed = true
			}
		}

		if allCondDone {
			finalStatus := types.WorkflowStatusCompleted
			if anyCondFailed {
				finalStatus = types.WorkflowStatusFailed
			}
			err := m.storage.UpdateWorkflowStatusAtomic(ctx, workflow.WorkflowID, finalStatus, workflow.Version)
			if errors.Is(err, storage.ErrVersionConflict) {
				return err
			}
			if err == nil {
				m.notifySender(ctx, workflow, finalStatus)
			}
			return err
		}
	}

	return nil
}

func (m *managerImpl) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-m.doneChan:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.sweepTimeouts(ctx)
			}
		}
	}()
}

func (m *managerImpl) Stop() error {
	m.stopOnce.Do(func() {
		close(m.doneChan)
	})
	return nil
}

func (m *managerImpl) sweepTimeouts(ctx context.Context) {
	timeouts, err := m.storage.ListTimedOutWorkflows(ctx)
	if err != nil {
		m.logger.Error("Error checking timed out workflows", err)
		return
	}

	for _, w := range timeouts {
		m.logger.WithField("workflow_id", w.WorkflowID).Info("Workflow timed out")
		if updateErr := m.storage.UpdateWorkflowStatus(ctx, w.WorkflowID, types.WorkflowStatusTimeout); updateErr != nil {
			m.logger.Error("Failed to update timed out workflow status", updateErr)
			continue
		}
		m.notifySender(ctx, w, types.WorkflowStatusTimeout)
		for _, p := range w.Participants {
			if p.Status == types.ParticipantStatusPending {
				if updateErr := m.storage.UpdateWorkflowParticipant(ctx, w.WorkflowID, p.Address, types.ParticipantStatusTimeout, nil); updateErr != nil {
					m.logger.Errorf(updateErr, "Failed to update participant %s to timeout status", p.Address)
				}
			}
		}
	}
}
