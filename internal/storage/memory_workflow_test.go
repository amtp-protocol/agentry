package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStorage_Workflow(t *testing.T) {
	storage := NewMemoryStorage(MemoryStorageConfig{})
	ctx := context.Background()

	// 1. Create Workflow
	workflowID := "wf-12345"
	state := &types.Workflow{
		WorkflowID:       workflowID,
		Status:           types.WorkflowStatusPending,
		CoordinationType: "parallel",
		TimeoutSeconds:   60,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		Participants: []types.WorkflowParticipant{
			{
				WorkflowID: workflowID,
				Address:    "agent1@domain.com",
				Status:     types.ParticipantStatusPending,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		},
	}

	err := storage.StoreWorkflow(ctx, state)
	require.NoError(t, err)

	// 2. Get Workflow
	fetched, err := storage.GetWorkflow(ctx, workflowID)
	require.NoError(t, err)
	assert.Equal(t, types.WorkflowStatusPending, fetched.Status)
	assert.Len(t, fetched.Participants, 1)

	// 3. Update Workflow Status
	err = storage.UpdateWorkflowStatus(ctx, workflowID, types.WorkflowStatusInProgress)
	require.NoError(t, err)

	// 4. Update Participant
	payload := json.RawMessage(`{"status":"ok"}`)
	err = storage.UpdateWorkflowParticipant(ctx, workflowID, "agent1@domain.com", types.ParticipantStatusCompleted, payload)
	require.NoError(t, err)

	// Verify updates
	fetched, err = storage.GetWorkflow(ctx, workflowID)
	require.NoError(t, err)
	assert.Equal(t, types.WorkflowStatusInProgress, fetched.Status)
	assert.Equal(t, types.ParticipantStatusCompleted, fetched.Participants[0].Status)
	assert.JSONEq(t, `{"status":"ok"}`, string(fetched.Participants[0].ResponsePayload))

	// 5. Test Timed out list
	stateTimeout := &types.Workflow{
		WorkflowID:       "wf-timeout",
		Status:           types.WorkflowStatusPending,
		CoordinationType: "sequential",
		TimeoutSeconds:   -1, // instantly timeout
		CreatedAt:        time.Now().Add(-10 * time.Second),
		UpdatedAt:        time.Now().Add(-10 * time.Second),
	}
	err = storage.StoreWorkflow(ctx, stateTimeout)
	require.NoError(t, err)

	timeouts, err := storage.ListTimedOutWorkflows(ctx)
	require.NoError(t, err)
	require.Len(t, timeouts, 1)
	assert.Equal(t, "wf-timeout", timeouts[0].WorkflowID)
}
