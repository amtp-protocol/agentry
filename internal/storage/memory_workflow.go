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
		return nil, fmt.Errorf("workflow not found")
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
			timeoutAt := state.CreatedAt.Add(time.Duration(state.TimeoutSeconds) * time.Second)
			if now.After(timeoutAt) {
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
