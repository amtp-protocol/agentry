package processing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
)

// Dispatch implements the workflow.Dispatcher interface. Engine-generated
// messages (sequential steps, conditional branches, notifications) arrive
// here with fresh identities that no storage row knows about, so the message
// and its initial status must be persisted before delivery — otherwise the
// status update at the end of the immediate path fails with "message status
// not found" and the dispatched message never becomes visible in the
// recipient's inbox. Parallel and conditional coordination instead re-dispatch
// the original message, which the ingest path already stored; re-storing it
// would violate the message ID uniqueness constraint, so only messages that
// are not yet known to storage get persisted here.
func (mp *MessageProcessor) Dispatch(ctx context.Context, msg *types.Message) error {
	recipients := make([]types.RecipientStatus, len(msg.Recipients))
	for i, addr := range msg.Recipients {
		recipients[i] = types.RecipientStatus{
			Address:   addr,
			Status:    types.StatusQueued,
			Timestamp: time.Now().UTC(),
		}
	}

	// Only engine-generated messages (fresh identities) need persisting;
	// re-dispatched originals were already stored by the ingest path.
	if _, err := mp.storage.GetMessage(ctx, msg.MessageID); err != nil {
		if !errors.Is(err, storage.ErrMessageNotFound) {
			return fmt.Errorf("failed to look up dispatched message: %w", err)
		}
		now := time.Now().UTC()
		if err := mp.storage.StoreMessageWithStatus(ctx, msg, &types.MessageStatus{
			MessageID:  msg.MessageID,
			Status:     types.StatusQueued,
			Recipients: recipients,
			Attempts:   0,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			return fmt.Errorf("failed to store dispatched message: %w", err)
		}
	}

	_, err := mp.processImmediatePath(ctx, msg, &ProcessingResult{
		MessageID:  msg.MessageID,
		Status:     types.StatusQueued,
		Recipients: recipients,
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
	wf, err := mp.workflow.Initialize(ctx, message)
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

	result.WorkflowID = wf.WorkflowID
	result.Status = types.StatusQueued
	return result, nil
}
