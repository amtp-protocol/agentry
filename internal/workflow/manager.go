package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
)

type managerImpl struct {
	storage    storage.Storage
	dispatcher Dispatcher
	doneChan   chan struct{}
}

func NewManager(s storage.Storage, d Dispatcher) Manager {
	return &managerImpl{
		storage:    s,
		dispatcher: d,
		doneChan:   make(chan struct{}),
	}
}

func (m *managerImpl) Initialize(ctx context.Context, msg *types.Message) (*types.Workflow, error) {
	if msg.Coordination == nil {
		return nil, fmt.Errorf("message does not contain coordination config")
	}

	workflow := &types.Workflow{
		WorkflowID:       msg.MessageID,
		Status:           types.WorkflowStatusPending,
		CoordinationType: msg.Coordination.Type,
		TimeoutSeconds:   msg.Coordination.Timeout,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if workflow.TimeoutSeconds <= 0 {
		workflow.TimeoutSeconds = 3600 // Default 1 hour if not specified
	}

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

	err := m.storage.StoreWorkflow(ctx, workflow)
	if err != nil {
		return nil, fmt.Errorf("failed to store workflow state: %w", err)
	}

	// Begin execution based on type
	err = m.startExecution(ctx, workflow, msg)
	if err != nil {
		m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusFailed)
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
		return m.executeSequentialNext(ctx, workflow, msg, 0)
	case "conditional":
		return m.executeConditional(ctx, workflow, msg)
	}
	return nil
}

// executeParallel sends message to all participants at once
func (m *managerImpl) executeParallel(ctx context.Context, _ *types.Workflow, msg *types.Message) error {
	msgCopy := *msg
	msgCopy.Recipients = append(msg.Coordination.RequiredResponses, msg.Coordination.OptionalResponses...)

	// We pass down to the dispatcher. The dispatcher should route the message properly.
	return m.dispatcher.Dispatch(ctx, &msgCopy)
}

// executeSequentialNext dispatches to the N-th participant in the sequence
func (m *managerImpl) executeSequentialNext(ctx context.Context, workflow *types.Workflow, msg *types.Message, index int) error {
	if index >= len(msg.Coordination.Sequence) {
		return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusCompleted)
	}

	nextAgent := msg.Coordination.Sequence[index]
	msgCopy := *msg
	msgCopy.Recipients = []string{nextAgent}

	return m.dispatcher.Dispatch(ctx, &msgCopy)
}

func (m *managerImpl) executeConditional(ctx context.Context, _ *types.Workflow, msg *types.Message) error {
	return m.dispatcher.Dispatch(ctx, msg)
}

func (m *managerImpl) ProcessResponse(ctx context.Context, workflowID string, replyMsg *types.Message) error {
	workflow, err := m.storage.GetWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("workflow not found: %w", err)
	}

	if workflow.Status == types.WorkflowStatusCompleted || workflow.Status == types.WorkflowStatusFailed || workflow.Status == types.WorkflowStatusTimeout {
		log.Printf("Workflow %s already finished, ignoring response", workflowID)
		return nil
	}

	// Determine success or failure from message type
	participantStatus := types.ParticipantStatusCompleted
	if replyMsg.ResponseType == "workflow_error" || replyMsg.ResponseType == "error" {
		participantStatus = types.ParticipantStatusFailed
	}

	// Update participant status
	err = m.storage.UpdateWorkflowParticipant(ctx, workflowID, replyMsg.Sender, participantStatus, replyMsg.Payload)
	if err != nil {
		return fmt.Errorf("failed to update participant status: %w", err)
	}

	// Re-evaluate state
	return m.evaluateWorkflow(ctx, workflow, replyMsg)
}

func (m *managerImpl) evaluateWorkflow(ctx context.Context, workflow *types.Workflow, replyMsg *types.Message) error {
	// Re-fetch to get up-to-date participants
	workflow, err := m.storage.GetWorkflow(ctx, workflow.WorkflowID)
	if err != nil {
		return err
	}

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

	origMsg, err := m.storage.GetMessage(ctx, workflow.WorkflowID)
	if err != nil {
		// Log but do not fail hard, it might be purged
		log.Printf("Warning: failed to get original message for workflow %s: %v", workflow.WorkflowID, err)
	}

	// Basic failure handling
	stopOnFailure := false
	if origMsg != nil && origMsg.Coordination != nil {
		stopOnFailure = origMsg.Coordination.StopOnFailure
	}

	if anyFailed && stopOnFailure {
		// Stop the workflow immediately
		return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusFailed)
	}

	if workflow.CoordinationType == "parallel" {
		if allDone {
			finalStatus := types.WorkflowStatusCompleted
			if anyFailed {
				finalStatus = types.WorkflowStatusFailed
			}
			return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, finalStatus)
		}
	} else if workflow.CoordinationType == "sequential" {
		if origMsg == nil {
			return fmt.Errorf("missing original message for sequential workflow")
		}

		if origMsg.Coordination == nil || len(origMsg.Coordination.Sequence) == 0 {
			return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusCompleted)
		}

		// Find the first participant in the sequence that is still pending
		statusMap := make(map[string]types.ParticipantStatus)
		for _, p := range workflow.Participants {
			statusMap[p.Address] = p.Status
		}

		nextIndex := -1
		for i, seqAddr := range origMsg.Coordination.Sequence {
			if statusMap[seqAddr] == types.ParticipantStatusPending {
				nextIndex = i
				break
			}
		}

		if nextIndex != -1 {
			// dispatch to the next agent
			return m.executeSequentialNext(ctx, workflow, origMsg, nextIndex)
		}

		// all finished
		finalStatus := types.WorkflowStatusCompleted
		if anyFailed {
			finalStatus = types.WorkflowStatusFailed // Or a partial success state
		}
		return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, finalStatus)
	} else if workflow.CoordinationType == "conditional" {
		if origMsg == nil {
			return fmt.Errorf("missing original message for conditional workflow")
		}

		if origMsg.Coordination == nil {
			return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, types.WorkflowStatusCompleted)
		}

		// Only evaluate condition if the reply is from an initial recipient
		if replyMsg != nil {
			isInitial := false
			for _, rec := range origMsg.Recipients {
				if rec == replyMsg.Sender {
					isInitial = true
					break
				}
			}

			if isInitial && len(replyMsg.Payload) > 0 {
				var payload map[string]interface{}
				_ = json.Unmarshal(replyMsg.Payload, &payload)

				for _, condition := range origMsg.Coordination.Conditions {
					matched := false
					// Simple expression evaluation like "status == 'approved'"
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
						// Fallback: substring matching
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

					// Mark skipped branch as completed
					for _, s := range skipped {
						_ = m.storage.UpdateWorkflowParticipant(ctx, workflow.WorkflowID, s, types.ParticipantStatusCompleted, []byte(`{"status":"skipped"}`))
					}

					// Dispatch to target branch
					if len(targets) > 0 {
						msgCopy := *origMsg
						msgCopy.Recipients = targets
						_ = m.dispatcher.Dispatch(ctx, &msgCopy)
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
			return m.storage.UpdateWorkflowStatus(ctx, workflow.WorkflowID, finalStatus)
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
	close(m.doneChan)
	return nil
}

func (m *managerImpl) sweepTimeouts(ctx context.Context) {
	timeouts, err := m.storage.ListTimedOutWorkflows(ctx)
	if err != nil {
		log.Printf("Error checking timed out workflows: %v", err)
		return
	}

	for _, w := range timeouts {
		log.Printf("Workflow %s timed out", w.WorkflowID)
		m.storage.UpdateWorkflowStatus(ctx, w.WorkflowID, types.WorkflowStatusTimeout)
		for _, p := range w.Participants {
			if p.Status == types.ParticipantStatusPending {
				m.storage.UpdateWorkflowParticipant(ctx, w.WorkflowID, p.Address, types.ParticipantStatusTimeout, nil)
			}
		}
	}
}
