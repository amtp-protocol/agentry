package storage

import (
	"encoding/json"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
	"gorm.io/datatypes"
)

// Workflow represents the database model for workflow tracking
type Workflow struct {
	ID               uint                  `gorm:"primarykey"`
	WorkflowID       string                `gorm:"type:uuid;uniqueIndex;not null" json:"workflow_id"`
	Status           types.WorkflowStatus  `gorm:"type:workflow_status;not null;default:'pending'" json:"status"`
	CoordinationType string                `gorm:"size:50;not null" json:"coordination_type"`
	TimeoutSeconds   int                   `gorm:"not null" json:"timeout_seconds"`
	Deadline         *time.Time            `gorm:"type:timestamptz" json:"deadline,omitempty"`
	CreatedAt        time.Time             `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	UpdatedAt        time.Time             `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
	Participants     []WorkflowParticipant `gorm:"foreignKey:WorkflowID;references:WorkflowID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (Workflow) TableName() string {
	return "workflow"
}

// WorkflowParticipant represents the database model for workflow participants
type WorkflowParticipant struct {
	ID              uint                    `gorm:"primarykey"`
	WorkflowID      string                  `gorm:"type:uuid;not null;uniqueIndex:idx_workflow_participants_workflow_address"`
	Address         string                  `gorm:"size:255;not null;uniqueIndex:idx_workflow_participants_workflow_address"`
	Status          types.ParticipantStatus `gorm:"size:50;not null;default:'pending'" json:"status"`
	ResponsePayload datatypes.JSON          `gorm:"type:jsonb" json:"response_payload,omitempty"`
	Deadline        *time.Time              `gorm:"type:timestamptz" json:"deadline,omitempty"`
	CreatedAt       time.Time               `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	UpdatedAt       time.Time               `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
}

func (WorkflowParticipant) TableName() string {
	return "workflow_participants"
}

// toDomainModel converts Workflow to types.Workflow
func (w *Workflow) toDomainModel() *types.Workflow {
	if w == nil {
		return nil
	}

	state := &types.Workflow{
		WorkflowID:       w.WorkflowID,
		Status:           w.Status,
		CoordinationType: w.CoordinationType,
		TimeoutSeconds:   w.TimeoutSeconds,
		Deadline:         w.Deadline,
		Participants:     make([]types.WorkflowParticipant, 0, len(w.Participants)),
		CreatedAt:        w.CreatedAt,
		UpdatedAt:        w.UpdatedAt,
	}

	for _, p := range w.Participants {
		participant := types.WorkflowParticipant{
			WorkflowID: p.WorkflowID,
			Address:    p.Address,
			Status:     p.Status,
			Deadline:   p.Deadline,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		}
		if len(p.ResponsePayload) > 0 {
			participant.ResponsePayload = json.RawMessage(p.ResponsePayload)
		}
		state.Participants = append(state.Participants, participant)
	}

	return state
}
