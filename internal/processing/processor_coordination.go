package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// Dispatch implements the workflow.Dispatcher interface
func (mp *MessageProcessor) Dispatch(ctx context.Context, msg *types.Message) error {
	_, err := mp.processImmediatePath(ctx, msg, &ProcessingResult{
		MessageID:  msg.MessageID,
		Status:     types.StatusQueued,
		Recipients: make([]types.RecipientStatus, len(msg.Recipients)),
	}, ProcessingOptions{ImmediatePath: true})
	return err
}

// processWithCoordination handles coordination-based message processing
// by delegating to the Workflow Engine.
func (mp *MessageProcessor) processWithCoordination(
	ctx context.Context,
	message *types.Message,
	result *ProcessingResult,
	_ ProcessingOptions,
) (*ProcessingResult, error) {
	if mp.workflow == nil {
		return nil, fmt.Errorf("workflow engine is not configured")
	}

	// Persist the workflow to DB and begin the state machine execution based on "type"
	_, err := mp.workflow.Initialize(ctx, message)
	if err != nil {
		// Update status as failed
		result.Status = types.StatusFailed
		result.ErrorCode = "WORKFLOW_INIT_FAILED"
		result.ErrorMessage = err.Error()

		// #nosec G104 - ignore err
		mp.storage.UpdateStatus(ctx, message.MessageID, func(status *types.MessageStatus) error {
			status.Status = result.Status
			status.Recipients = result.Recipients
			status.UpdatedAt = time.Now().UTC()
			return nil
		})
		return result, err
	}

	result.Status = types.StatusQueued
	return result, nil
}
