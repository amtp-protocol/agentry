package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
	"gorm.io/datatypes"
	"gorm.io/gorm"
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
		Version:          state.Version,
		Deadline:         state.Deadline,
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
	err := db.db.WithContext(ctx).
		Preload("Participants").
		Where("status IN (?)", []types.WorkflowStatus{types.WorkflowStatusPending, types.WorkflowStatusInProgress}).
		Where("deadline < NOW()").
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

// UpdateWorkflowParticipantAtomic updates a participant and atomically bumps the
// workflow version. If the expectedVersion does not match the stored version,
// ErrVersionConflict is returned and no changes are made.
func (db *DatabaseStorage) UpdateWorkflowParticipantAtomic(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte, expectedVersion int) error {
	return db.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update the participant row
		updates := map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}
		if len(responsePayload) > 0 {
			updates["response_payload"] = datatypes.JSON(responsePayload)
		}
		if err := tx.
			Model(&WorkflowParticipant{}).
			Where("workflow_id = ? AND address = ?", workflowID, address).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update participant: %w", err)
		}

		// Bump version atomically — if version mismatches, this updates 0 rows
		result := tx.
			Model(&Workflow{}).
			Where("workflow_id = ? AND version = ?", workflowID, expectedVersion).
			Updates(map[string]interface{}{
				"version":    gorm.Expr("version + 1"),
				"updated_at": time.Now(),
			})
		if result.Error != nil {
			return fmt.Errorf("failed to bump workflow version: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		return nil
	})
}

// UpdateWorkflowStatusAtomic updates the workflow status and atomically bumps the
// version. If the expectedVersion does not match, ErrVersionConflict is returned.
func (db *DatabaseStorage) UpdateWorkflowStatusAtomic(ctx context.Context, workflowID string, status types.WorkflowStatus, expectedVersion int) error {
	result := db.db.WithContext(ctx).
		Model(&Workflow{}).
		Where("workflow_id = ? AND version = ?", workflowID, expectedVersion).
		Updates(map[string]interface{}{
			"status":     status,
			"version":    gorm.Expr("version + 1"),
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return fmt.Errorf("failed to update workflow status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}
