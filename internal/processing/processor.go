/*
 * Copyright 2025 Cong Wang
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package processing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/amtp-protocol/agentry/internal/storage"
	"github.com/amtp-protocol/agentry/internal/types"
)

// MessageProcessor handles message processing and routing
type MessageProcessor struct {
	discovery      DiscoveryService
	deliveryEngine DeliveryService
	storage        storage.MessageStorage
	idempotencyMap map[string]*ProcessingResult
	idempotencyMux sync.RWMutex
}

// ProcessingResult represents the result of message processing
type ProcessingResult struct {
	MessageID    string
	Status       types.DeliveryStatus
	Recipients   []types.RecipientStatus
	ProcessedAt  time.Time
	ExpiresAt    time.Time
	ErrorCode    string
	ErrorMessage string
}

// ProcessingOptions defines options for message processing
type ProcessingOptions struct {
	ImmediatePath bool
	Timeout       time.Duration
	MaxRetries    int
}

// NewMessageProcessor creates a new message processor
func NewMessageProcessor(discovery DiscoveryService, deliveryEngine DeliveryService, messageStorage storage.MessageStorage) *MessageProcessor {
	return &MessageProcessor{
		discovery:      discovery,
		deliveryEngine: deliveryEngine,
		storage:        messageStorage,
		idempotencyMap: make(map[string]*ProcessingResult),
	}
}

// ProcessMessage processes an incoming message
func (mp *MessageProcessor) ProcessMessage(ctx context.Context, message *types.Message, options ProcessingOptions) (*ProcessingResult, error) {
	// Check idempotency
	if result := mp.checkIdempotency(message.IdempotencyKey); result != nil {
		return result, nil
	}

	// Store message
	if err := mp.storage.StoreMessage(ctx, message); err != nil {
		return nil, fmt.Errorf("failed to store message: %w", err)
	}

	// Initialize processing result
	result := &ProcessingResult{
		MessageID:   message.MessageID,
		Status:      types.StatusQueued,
		Recipients:  make([]types.RecipientStatus, len(message.Recipients)),
		ProcessedAt: time.Now().UTC(),
		ExpiresAt:   time.Now().Add(24 * time.Hour), // 24-hour TTL for idempotency
	}

	// Initialize recipient statuses
	for i, recipient := range message.Recipients {
		result.Recipients[i] = types.RecipientStatus{
			Address:   recipient,
			Status:    types.StatusQueued,
			Timestamp: time.Now().UTC(),
			Attempts:  0,
		}
	}

	// Store initial status
	initialStatus := &types.MessageStatus{
		MessageID:  message.MessageID,
		Status:     types.StatusQueued,
		Recipients: result.Recipients,
		Attempts:   0,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := mp.storage.StoreStatus(ctx, message.MessageID, initialStatus); err != nil {
		return nil, fmt.Errorf("failed to store initial status: %w", err)
	}

	// Store idempotency result
	mp.storeIdempotencyResult(message.IdempotencyKey, result)

	// Process based on coordination type or immediate path
	if options.ImmediatePath || message.Coordination == nil {
		return mp.processImmediatePath(ctx, message, result, options)
	}

	// Handle coordination-based processing
	return mp.processWithCoordination(ctx, message, result, options)
}

// processImmediatePath handles immediate path message processing
func (mp *MessageProcessor) processImmediatePath(ctx context.Context, message *types.Message, result *ProcessingResult, options ProcessingOptions) (*ProcessingResult, error) {
	// Set timeout context
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	// Process recipients in parallel for immediate path
	var wg sync.WaitGroup
	resultChan := make(chan types.RecipientStatus, len(message.Recipients))

	for i, recipient := range message.Recipients {
		wg.Add(1)
		go func(index int, addr string) {
			defer wg.Done()

			// Update status to delivering
			recipientStatus := types.RecipientStatus{
				Address:   addr,
				Status:    types.StatusDelivering,
				Timestamp: time.Now().UTC(),
				Attempts:  1,
			}

			// Attempt delivery
			deliveryResult, err := mp.deliveryEngine.DeliverMessage(ctx, message, addr)
			if err != nil {
				recipientStatus.Status = types.StatusFailed
				recipientStatus.ErrorCode = "DELIVERY_FAILED"
				recipientStatus.ErrorMessage = err.Error()
			} else {
				recipientStatus.Status = deliveryResult.Status
				recipientStatus.DeliveryMode = deliveryResult.DeliveryMode
				recipientStatus.LocalDelivery = deliveryResult.LocalDelivery

				// For pull mode local delivery, mark as inbox delivered
				if deliveryResult.LocalDelivery && deliveryResult.DeliveryMode == "pull" && deliveryResult.Status == types.StatusDelivered {
					recipientStatus.InboxDelivered = true
				}

				if deliveryResult.ErrorCode != "" {
					recipientStatus.ErrorCode = deliveryResult.ErrorCode
					recipientStatus.ErrorMessage = deliveryResult.ErrorMessage
				}
			}

			recipientStatus.Timestamp = time.Now().UTC()
			resultChan <- recipientStatus
		}(i, recipient)
	}

	// Wait for all deliveries to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	recipientResults := make([]types.RecipientStatus, 0, len(message.Recipients))
	for recipientStatus := range resultChan {
		recipientResults = append(recipientResults, recipientStatus)
	}

	// Update result with recipient statuses
	result.Recipients = recipientResults

	// Determine overall status
	allDelivered := true
	anyFailed := false
	for _, rs := range recipientResults {
		if rs.Status != types.StatusDelivered {
			allDelivered = false
		}
		if rs.Status == types.StatusFailed {
			anyFailed = true
		}
	}

	if allDelivered {
		result.Status = types.StatusDelivered
	} else if anyFailed {
		result.Status = types.StatusFailed
	} else {
		result.Status = types.StatusDelivering
	}

	// Update stored status
	err := mp.storage.UpdateStatus(ctx, message.MessageID, func(status *types.MessageStatus) error {
		status.Status = result.Status
		status.Recipients = result.Recipients
		status.UpdatedAt = time.Now().UTC()
		if result.Status == types.StatusDelivered {
			now := time.Now().UTC()
			status.DeliveredAt = &now
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	return result, nil
}

// processWithCoordination handles coordination-based message processing
func (mp *MessageProcessor) processWithCoordination(ctx context.Context, message *types.Message, result *ProcessingResult, options ProcessingOptions) (*ProcessingResult, error) {
	coordination := message.Coordination

	switch coordination.Type {
	case "parallel":
		return mp.processParallelCoordination(ctx, message, result, options)
	case "sequential":
		return mp.processSequentialCoordination(ctx, message, result, options)
	case "conditional":
		return mp.processConditionalCoordination(ctx, message, result, options)
	default:
		result.Status = types.StatusFailed
		result.ErrorCode = "UNSUPPORTED_COORDINATION"
		result.ErrorMessage = fmt.Sprintf("unsupported coordination type: %s", coordination.Type)

		// Update status in storage
		// #nosec G104 -- ignore error
		mp.storage.UpdateStatus(ctx, message.MessageID, func(status *types.MessageStatus) error {
			status.Status = result.Status
			status.Recipients = result.Recipients
			status.UpdatedAt = time.Now().UTC()
			return nil
		})

		return result, fmt.Errorf("unsupported coordination type: %s", coordination.Type)
	}
}

// processParallelCoordination handles parallel coordination
func (mp *MessageProcessor) processParallelCoordination(ctx context.Context, message *types.Message, result *ProcessingResult, options ProcessingOptions) (*ProcessingResult, error) {
	coordination := message.Coordination

	// Set coordination timeout
	timeout := time.Duration(coordination.Timeout) * time.Second
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Process all recipients in parallel
	return mp.processImmediatePath(ctx, message, result, options)
}

// processSequentialCoordination handles sequential coordination
func (mp *MessageProcessor) processSequentialCoordination(ctx context.Context, message *types.Message, result *ProcessingResult, options ProcessingOptions) (*ProcessingResult, error) {
	coordination := message.Coordination

	// Process recipients in sequence order
	for _, recipient := range coordination.Sequence {
		recipientStatus := types.RecipientStatus{
			Address:   recipient,
			Status:    types.StatusDelivering,
			Timestamp: time.Now().UTC(),
			Attempts:  1,
		}

		// Attempt delivery
		deliveryResult, err := mp.deliveryEngine.DeliverMessage(ctx, message, recipient)
		if err != nil {
			recipientStatus.Status = types.StatusFailed
			recipientStatus.ErrorCode = "DELIVERY_FAILED"
			recipientStatus.ErrorMessage = err.Error()

			// Stop on failure if configured
			if coordination.StopOnFailure {
				// Update result and return
				for i, rs := range result.Recipients {
					if rs.Address == recipient {
						result.Recipients[i] = recipientStatus
						break
					}
				}
				result.Status = types.StatusFailed

				// Update status in storage
				// #nosec G104 -- ignore error
				mp.storage.UpdateStatus(ctx, message.MessageID, func(status *types.MessageStatus) error {
					status.Status = result.Status
					status.Recipients = result.Recipients
					status.UpdatedAt = time.Now().UTC()
					return nil
				})

				return result, fmt.Errorf("sequential delivery failed for %s: %w", recipient, err)
			}
		} else {
			recipientStatus.Status = deliveryResult.Status
			if deliveryResult.ErrorCode != "" {
				recipientStatus.ErrorCode = deliveryResult.ErrorCode
				recipientStatus.ErrorMessage = deliveryResult.ErrorMessage
			}
		}

		recipientStatus.Timestamp = time.Now().UTC()

		// Update recipient status in result
		for i, rs := range result.Recipients {
			if rs.Address == recipient {
				result.Recipients[i] = recipientStatus
				break
			}
		}
	}

	// Determine overall status
	allDelivered := true
	anyFailed := false
	for _, rs := range result.Recipients {
		if rs.Status != types.StatusDelivered {
			allDelivered = false
		}
		if rs.Status == types.StatusFailed {
			anyFailed = true
		}
	}

	if allDelivered {
		result.Status = types.StatusDelivered
	} else if anyFailed {
		result.Status = types.StatusFailed
	} else {
		result.Status = types.StatusDelivering
	}

	// Update status in storage
	err := mp.storage.UpdateStatus(ctx, message.MessageID, func(status *types.MessageStatus) error {
		status.Status = result.Status
		status.Recipients = result.Recipients
		status.UpdatedAt = time.Now().UTC()
		if result.Status == types.StatusDelivered {
			now := time.Now().UTC()
			status.DeliveredAt = &now
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	return result, nil
}

// processConditionalCoordination handles conditional coordination
func (mp *MessageProcessor) processConditionalCoordination(ctx context.Context, message *types.Message, result *ProcessingResult, options ProcessingOptions) (*ProcessingResult, error) {
	// For now, implement basic conditional logic
	// This would be expanded based on specific conditional requirements
	coordination := message.Coordination

	for _, condition := range coordination.Conditions {
		// Evaluate condition (simplified for now)
		// In a real implementation, this would parse and evaluate the condition expression
		shouldExecute := mp.evaluateCondition(condition.If, message)

		var recipients []string
		if shouldExecute {
			recipients = condition.Then
		} else {
			recipients = condition.Else
		}

		// Process selected recipients
		for _, recipient := range recipients {
			recipientStatus := types.RecipientStatus{
				Address:   recipient,
				Status:    types.StatusDelivering,
				Timestamp: time.Now().UTC(),
				Attempts:  1,
			}

			deliveryResult, err := mp.deliveryEngine.DeliverMessage(ctx, message, recipient)
			if err != nil {
				recipientStatus.Status = types.StatusFailed
				recipientStatus.ErrorCode = "DELIVERY_FAILED"
				recipientStatus.ErrorMessage = err.Error()
			} else {
				recipientStatus.Status = deliveryResult.Status
				if deliveryResult.ErrorCode != "" {
					recipientStatus.ErrorCode = deliveryResult.ErrorCode
					recipientStatus.ErrorMessage = deliveryResult.ErrorMessage
				}
			}

			recipientStatus.Timestamp = time.Now().UTC()

			// Update recipient status in result
			for i, rs := range result.Recipients {
				if rs.Address == recipient {
					result.Recipients[i] = recipientStatus
					break
				}
			}
		}
	}

	// Determine overall status
	result.Status = types.StatusDelivered

	// Update status in storage
	err := mp.storage.UpdateStatus(ctx, message.MessageID, func(status *types.MessageStatus) error {
		status.Status = result.Status
		status.Recipients = result.Recipients
		status.UpdatedAt = time.Now().UTC()
		if result.Status == types.StatusDelivered {
			now := time.Now().UTC()
			status.DeliveredAt = &now
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	return result, nil
}

// evaluateCondition evaluates a conditional expression (simplified)
func (mp *MessageProcessor) evaluateCondition(condition string, message *types.Message) bool {
	// Simplified condition evaluation
	// In a real implementation, this would parse complex expressions
	switch condition {
	case "always":
		return true
	case "never":
		return false
	default:
		// Default to true for unknown conditions
		return true
	}
}

// checkIdempotency checks if a message has already been processed
func (mp *MessageProcessor) checkIdempotency(idempotencyKey string) *ProcessingResult {
	mp.idempotencyMux.RLock()
	defer mp.idempotencyMux.RUnlock()

	result, exists := mp.idempotencyMap[idempotencyKey]
	if !exists {
		return nil
	}

	// Check if result has expired
	if time.Now().After(result.ExpiresAt) {
		// Clean up expired entry
		go func() {
			mp.idempotencyMux.Lock()
			delete(mp.idempotencyMap, idempotencyKey)
			mp.idempotencyMux.Unlock()
		}()
		return nil
	}

	return result
}

// storeIdempotencyResult stores the processing result for idempotency checking
func (mp *MessageProcessor) storeIdempotencyResult(idempotencyKey string, result *ProcessingResult) {
	mp.idempotencyMux.Lock()
	defer mp.idempotencyMux.Unlock()

	mp.idempotencyMap[idempotencyKey] = result
}

// CleanupExpiredEntries removes expired idempotency entries
func (mp *MessageProcessor) CleanupExpiredEntries() {
	mp.idempotencyMux.Lock()
	defer mp.idempotencyMux.Unlock()

	now := time.Now()
	for key, result := range mp.idempotencyMap {
		if now.After(result.ExpiresAt) {
			delete(mp.idempotencyMap, key)
		}
	}
}
