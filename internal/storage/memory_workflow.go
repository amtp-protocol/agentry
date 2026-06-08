package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

func (ms *MemoryStorage) StoreWorkflow(ctx context.Context, state *types.Workflow) error {
	ms.workflowsMux.Lock()
	defer ms.workflowsMux.Unlock()

	if _, exists := ms.workflows[state.WorkflowID]; exists {
		return fmt.Errorf("workflow %s already exists", state.WorkflowID)
	}

	// Make a deep copy to store
	stateCopy := *state
	if stateCopy.Version == 0 {
		stateCopy.Version = 1
	}
	stateCopy.Participants = make([]types.WorkflowParticipant, len(state.Participants))
	copy(stateCopy.Participants, state.Participants)

	ms.workflows[state.WorkflowID] = &stateCopy
	return nil
}

func (ms *MemoryStorage) GetWorkflow(ctx context.Context, workflowID string) (*types.Workflow, error) {
	ms.workflowsMux.RLock()
	defer ms.workflowsMux.RUnlock()

	state, exists := ms.workflows[workflowID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrWorkflowNotFound, workflowID)
	}

	// Deep copy to return
	stateCopy := *state
	stateCopy.Participants = make([]types.WorkflowParticipant, len(state.Participants))
	copy(stateCopy.Participants, state.Participants)

	return &stateCopy, nil
}

func (ms *MemoryStorage) UpdateWorkflowStatus(ctx context.Context, workflowID string, status types.WorkflowStatus) error {
	ms.workflowsMux.Lock()
	defer ms.workflowsMux.Unlock()

	state, exists := ms.workflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found")
	}

	state.Status = status
	state.UpdatedAt = time.Now()
	return nil
}

func (ms *MemoryStorage) UpdateWorkflowParticipant(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte) error {
	ms.workflowsMux.Lock()
	defer ms.workflowsMux.Unlock()

	state, exists := ms.workflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found")
	}

	updated := false
	for i := range state.Participants {
		if state.Participants[i].Address == address {
			state.Participants[i].Status = status
			if len(responsePayload) > 0 {
				state.Participants[i].ResponsePayload = responsePayload
			}
			state.Participants[i].UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		return fmt.Errorf("participant %s not found in workflow %s", address, workflowID)
	}

	state.UpdatedAt = time.Now()
	return nil
}

func (ms *MemoryStorage) ListTimedOutWorkflows(ctx context.Context) ([]*types.Workflow, error) {
	ms.workflowsMux.RLock()
	defer ms.workflowsMux.RUnlock()

	var results []*types.Workflow
	now := time.Now()

	for _, state := range ms.workflows {
		if state.Status == types.WorkflowStatusPending || state.Status == types.WorkflowStatusInProgress {
			timedOut := false
			if state.Deadline != nil {
				timedOut = now.After(*state.Deadline)
			} else {
				// Fallback for workflows stored without a deadline (backward compat)
				timeoutAt := state.CreatedAt.Add(time.Duration(state.TimeoutSeconds) * time.Second)
				timedOut = now.After(timeoutAt)
			}
			if timedOut {
				// Deep copy
				stateCopy := *state
				stateCopy.Participants = make([]types.WorkflowParticipant, len(state.Participants))
				copy(stateCopy.Participants, state.Participants)
				results = append(results, &stateCopy)
			}
		}
	}

	return results, nil
}

// UpdateWorkflowParticipantAtomic updates a participant only if the workflow
// version matches expectedVersion. On success the version is bumped.
func (ms *MemoryStorage) UpdateWorkflowParticipantAtomic(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte, expectedVersion int) error {
	ms.workflowsMux.Lock()
	defer ms.workflowsMux.Unlock()

	state, exists := ms.workflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found")
	}
	if state.Version != expectedVersion {
		return ErrVersionConflict
	}

	updated := false
	for i := range state.Participants {
		if state.Participants[i].Address == address {
			state.Participants[i].Status = status
			if len(responsePayload) > 0 {
				state.Participants[i].ResponsePayload = responsePayload
			}
			state.Participants[i].UpdatedAt = time.Now()
			updated = true
			break
		}
	}
	if !updated {
		return fmt.Errorf("participant %s not found in workflow %s", address, workflowID)
	}

	state.Version++
	state.UpdatedAt = time.Now()
	return nil
}

// UpdateWorkflowStatusAtomic updates the workflow status only if the workflow
// version matches expectedVersion. On success the version is bumped.
func (ms *MemoryStorage) UpdateWorkflowStatusAtomic(ctx context.Context, workflowID string, status types.WorkflowStatus, expectedVersion int) error {
	ms.workflowsMux.Lock()
	defer ms.workflowsMux.Unlock()

	state, exists := ms.workflows[workflowID]
	if !exists {
		return fmt.Errorf("workflow not found")
	}
	if state.Version != expectedVersion {
		return ErrVersionConflict
	}

	state.Status = status
	state.Version++
	state.UpdatedAt = time.Now()
	return nil
}
