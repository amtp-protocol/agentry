package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
	"gorm.io/datatypes"
)

func (db *DatabaseStorage) StoreWorkflow(ctx context.Context, state *types.Workflow) error {
	participants := make([]WorkflowParticipant, len(state.Participants))
	for i, p := range state.Participants {
		participants[i] = WorkflowParticipant{
			WorkflowID:      p.WorkflowID,
			Address:         p.Address,
			Status:          p.Status,
			ResponsePayload: datatypes.JSON(p.ResponsePayload),
			Deadline:        p.Deadline,
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		}
	}

	workState := &Workflow{
		WorkflowID:       state.WorkflowID,
		Status:           state.Status,
		CoordinationType: state.CoordinationType,
		TimeoutSeconds:   state.TimeoutSeconds,
		CreatedAt:        state.CreatedAt,
		UpdatedAt:        state.UpdatedAt,
		Participants:     participants,
	}

	return db.db.WithContext(ctx).Create(workState).Error
}

func (db *DatabaseStorage) GetWorkflow(ctx context.Context, workflowID string) (*types.Workflow, error) {
	var state Workflow
	err := db.db.WithContext(ctx).
		Preload("Participants").
		Where("workflow_id = ?", workflowID).
		First(&state).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	return state.toDomainModel(), nil
}

func (db *DatabaseStorage) UpdateWorkflowStatus(ctx context.Context, workflowID string, status types.WorkflowStatus) error {
	return db.db.WithContext(ctx).
		Model(&Workflow{}).
		Where("workflow_id = ?", workflowID).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

func (db *DatabaseStorage) UpdateWorkflowParticipant(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if len(responsePayload) > 0 {
		updates["response_payload"] = datatypes.JSON(responsePayload)
	}

	return db.db.WithContext(ctx).
		Model(&WorkflowParticipant{}).
		Where("workflow_id = ? AND address = ?", workflowID, address).
		Updates(updates).Error
}

func (db *DatabaseStorage) ListTimedOutWorkflows(ctx context.Context) ([]*types.Workflow, error) {
	var states []Workflow
	// Find pending/in_progress workflows where created_at + timeout_seconds < now
	err := db.db.WithContext(ctx).
		Preload("Participants").
		Where("status IN (?)", []types.WorkflowStatus{types.WorkflowStatusPending, types.WorkflowStatusInProgress}).
		Where("created_at + timeout_seconds * interval '1 second' < NOW()").
		Find(&states).Error

	if err != nil {
		return nil, fmt.Errorf("failed to list timed out workflows: %w", err)
	}

	var results []*types.Workflow
	for _, ws := range states {
		results = append(results, ws.toDomainModel())
	}
	return results, nil
}
