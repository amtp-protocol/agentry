/*
 * Copyright 2025 Sen Wang
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

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/types"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type DatabaseStorage struct {
	config DatabaseStorageConfig
	db     *gorm.DB
}

// NewDatabaseStorage creates a new database storage instance. If dbOverride is non-nil, it is used (for testing).
func NewDatabaseStorage(config DatabaseStorageConfig, dbOverride ...*gorm.DB) (*DatabaseStorage, error) {
	var db *gorm.DB
	var err error
	if len(dbOverride) > 0 && dbOverride[0] != nil {
		db = dbOverride[0]
	} else {
		db, err = gorm.Open(
			postgres.New(postgres.Config{
				DriverName: config.Driver,
				DSN:        config.ConnectionString,
			}),
			&gorm.Config{},
		)
		if err != nil {
			return nil, err
		}

		// Set connection pool settings
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		if config.MaxConnections > 0 {
			sqlDB.SetMaxOpenConns(config.MaxConnections)
		}
		if config.MaxIdleTime > 0 {
			sqlDB.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime) * time.Second)
		}
	}
	return &DatabaseStorage{
		config: config,
		db:     db,
	}, nil
}

// StoreMessage stores a message in the database
func (ds *DatabaseStorage) StoreMessage(ctx context.Context, message *types.Message) error {
	if message == nil {
		return fmt.Errorf("message cannot be nil")
	}
	if message.MessageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	// Convert types.Message to Message
	dbMessage, err := ds.convertToDBMessage(message)
	if err != nil {
		return fmt.Errorf("failed to convert message: %w", err)
	}

	// Use transaction to ensure data consistency
	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Store the main message
		if err := tx.Create(dbMessage).Error; err != nil {
			return fmt.Errorf("failed to create message in database: %w", err)
		}

		// Create initial message status
		messageStatus := MessageStatus{
			MessageID: message.MessageID,
			Status:    StatusPending,
			Attempts:  0,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}

		if err := tx.Create(&messageStatus).Error; err != nil {
			return fmt.Errorf("failed to create message status: %w", err)
		}

		// Create recipient statuses
		var recipientStatuses []RecipientStatus
		for _, recipient := range message.Recipients {
			recipientStatus := RecipientStatus{
				MessageID: message.MessageID,
				Address:   recipient,
				Status:    StatusPending,
				Timestamp: time.Now().UTC(),
				Attempts:  0,
			}
			recipientStatuses = append(recipientStatuses, recipientStatus)
		}

		if len(recipientStatuses) > 0 {
			if err := tx.Create(&recipientStatuses).Error; err != nil {
				return fmt.Errorf("failed to create recipient statuses: %w", err)
			}
		}

		return nil
	})
}

// GetMessage retrieves a message by ID
func (ds *DatabaseStorage) GetMessage(ctx context.Context, messageID string) (*types.Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID cannot be empty")
	}

	var dbMessage Message
	if err := ds.db.WithContext(ctx).
		Where("message_id = ?", messageID).
		First(&dbMessage).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("message not found: %s", messageID)
		}
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	return ds.convertToTypesMessage(&dbMessage)
}

// DeleteMessage removes a message from storage
func (ds *DatabaseStorage) DeleteMessage(ctx context.Context, messageID string) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	// Use transaction to ensure all related data is deleted
	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Check if message exists
		var count int64
		if err := tx.Model(&Message{}).
			Where("message_id = ?", messageID).
			Count(&count).Error; err != nil {
			return fmt.Errorf("failed to check message existence: %w", err)
		}

		if count == 0 {
			return fmt.Errorf("message not found: %s", messageID)
		}

		// Delete related recipient statuses
		if err := tx.Where("message_id = ?", messageID).
			Delete(&RecipientStatus{}).Error; err != nil {
			return fmt.Errorf("failed to delete recipient statuses: %w", err)
		}

		// Delete related message status
		if err := tx.Where("message_id = ?", messageID).
			Delete(&MessageStatus{}).Error; err != nil {
			return fmt.Errorf("failed to delete message status: %w", err)
		}

		// Delete the message
		if err := tx.Where("message_id = ?", messageID).
			Delete(&Message{}).Error; err != nil {
			return fmt.Errorf("failed to delete message: %w", err)
		}

		return nil
	})
}

// ListMessages returns messages matching the filter criteria
func (ds *DatabaseStorage) ListMessages(ctx context.Context, filter MessageFilter) ([]*types.Message, error) {
	query := ds.db.WithContext(ctx).Model(&Message{})

	// Apply filters
	if filter.Sender != "" {
		query = query.Where("sender = ?", filter.Sender)
	}

	if len(filter.Recipients) > 0 {
		// Use JSONB containment operator to check if recipients array contains any of the filter recipients
		recipientsJSON, err := json.Marshal(filter.Recipients)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal recipients filter: %w", err)
		}
		query = query.Where("recipients @> ?", string(recipientsJSON))
	}

	if filter.Status != "" {
		// Join with message_statuses table to filter by status
		query = query.Joins("JOIN message_statuses ON messages.message_id = message_statuses.message_id").
			Where("message_statuses.status = ?", filter.Status)
	}

	if filter.Since != nil {
		query = query.Where("timestamp >= ?", time.Unix(*filter.Since, 0))
	}

	// Apply ordering and pagination
	query = query.Order("created_at DESC")

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	var dbMessages []Message
	if err := query.Find(&dbMessages).Error; err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	// Convert to types.Message
	var messages []*types.Message
	for i := range dbMessages {
		message, err := ds.convertToTypesMessage(&dbMessages[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, nil
}

// StoreStatus stores message status
func (ds *DatabaseStorage) StoreStatus(ctx context.Context, messageID string, status *types.MessageStatus) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}
	if status == nil {
		return fmt.Errorf("status cannot be nil")
	}

	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Update message status
		messageStatus := MessageStatus{
			MessageID:   messageID,
			Status:      DeliveryStatus(status.Status),
			Attempts:    status.Attempts,
			NextRetry:   status.NextRetry,
			DeliveredAt: status.DeliveredAt,
			UpdatedAt:   time.Now().UTC(),
		}

		if err := tx.Where("message_id = ?", messageID).
			Assign(messageStatus).
			FirstOrCreate(&MessageStatus{}).Error; err != nil {
			return fmt.Errorf("failed to store message status: %w", err)
		}

		// Update recipient statuses
		for _, recipientStatus := range status.Recipients {
			rs := RecipientStatus{
				MessageID:      messageID,
				Address:        recipientStatus.Address,
				Status:         DeliveryStatus(recipientStatus.Status),
				Timestamp:      recipientStatus.Timestamp,
				Attempts:       recipientStatus.Attempts,
				ErrorCode:      recipientStatus.ErrorCode,
				ErrorMessage:   recipientStatus.ErrorMessage,
				DeliveryMode:   recipientStatus.DeliveryMode,
				LocalDelivery:  recipientStatus.LocalDelivery,
				InboxDelivered: recipientStatus.InboxDelivered,
				Acknowledged:   recipientStatus.Acknowledged,
				AcknowledgedAt: recipientStatus.AcknowledgedAt,
			}

			if err := tx.Where("message_id = ? AND address = ?", messageID, recipientStatus.Address).
				Assign(rs).
				FirstOrCreate(&RecipientStatus{}).Error; err != nil {
				return fmt.Errorf("failed to store recipient status: %w", err)
			}
		}

		return nil
	})
}

// GetStatus retrieves a message status from the database
func (ds *DatabaseStorage) GetStatus(ctx context.Context, messageID string) (*types.MessageStatus, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID cannot be empty")
	}

	// Get message status
	var messageStatus MessageStatus
	if err := ds.db.WithContext(ctx).
		Where("message_id = ?", messageID).
		First(&messageStatus).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("message status not found: %s", messageID)
		}
		return nil, fmt.Errorf("failed to get message status: %w", err)
	}

	// Get recipient statuses
	var recipientStatuses []RecipientStatus
	if err := ds.db.WithContext(ctx).
		Where("message_id = ?", messageID).
		Find(&recipientStatuses).Error; err != nil {
		return nil, fmt.Errorf("failed to get recipient statuses: %w", err)
	}

	return ds.convertToTypesMessageStatus(&messageStatus, recipientStatuses)
}

// UpdateStatus updates message status using the provided updater function
func (ds *DatabaseStorage) UpdateStatus(ctx context.Context, messageID string, updater StatusUpdater) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}
	if updater == nil {
		return fmt.Errorf("updater function cannot be nil")
	}

	// Get current status
	currentStatus, err := ds.GetStatus(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to get current status: %w", err)
	}

	// Apply updates
	if err := updater(currentStatus); err != nil {
		return fmt.Errorf("updater function failed: %w", err)
	}

	// Store updated status
	return ds.StoreStatus(ctx, messageID, currentStatus)
}

// DeleteStatus deletes a message status from the database
func (ds *DatabaseStorage) DeleteStatus(ctx context.Context, messageID string) error {
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete recipient statuses
		if err := tx.Where("message_id = ?", messageID).
			Delete(&RecipientStatus{}).Error; err != nil {
			return fmt.Errorf("failed to delete recipient statuses: %w", err)
		}

		// Delete message status
		result := tx.Where("message_id = ?", messageID).
			Delete(&MessageStatus{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete message status: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("message status not found: %s", messageID)
		}

		return nil
	})
}

// GetInboxMessages retrieves messages for a recipient from the database
func (ds *DatabaseStorage) GetInboxMessages(ctx context.Context, recipient string) ([]*types.Message, error) {
	if recipient == "" {
		return nil, fmt.Errorf("recipient cannot be empty")
	}

	var dbMessages []Message
	err := ds.db.WithContext(ctx).
		Joins("JOIN recipient_statuses ON messages.message_id = recipient_statuses.message_id").
		Where("recipient_statuses.address = ?", recipient).
		Where("recipient_statuses.local_delivery = ?", true).
		Where("recipient_statuses.inbox_delivered = ?", true).
		Where("recipient_statuses.acknowledged = ?", false).
		Find(&dbMessages).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get inbox messages: %w", err)
	}

	// Convert to types.Message
	var messages []*types.Message
	for i := range dbMessages {
		message, err := ds.convertToTypesMessage(&dbMessages[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, nil
}

// AcknowledgeMessage marks a message as acknowledged for a specific recipient
func (ds *DatabaseStorage) AcknowledgeMessage(ctx context.Context, recipient, messageID string) error {
	if recipient == "" {
		return fmt.Errorf("recipient cannot be empty")
	}
	if messageID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Check if message exists and is deliverable
		var recipientStatus RecipientStatus
		if err := tx.Where("message_id = ? AND address = ?", messageID, recipient).
			First(&recipientStatus).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("message not found for recipient: %s", recipient)
			}
			return fmt.Errorf("failed to get recipient status: %w", err)
		}

		if !recipientStatus.LocalDelivery || !recipientStatus.InboxDelivered {
			return fmt.Errorf("message not available in inbox for recipient: %s", recipient)
		}

		if recipientStatus.Acknowledged {
			return fmt.Errorf("message already acknowledged: %s", messageID)
		}

		// Update acknowledgment
		now := time.Now().UTC()
		if err := tx.Model(&RecipientStatus{}).
			Where("message_id = ? AND address = ?", messageID, recipient).
			Updates(map[string]interface{}{
				"acknowledged":    true,
				"acknowledged_at": now,
			}).Error; err != nil {
			return fmt.Errorf("failed to acknowledge message: %w", err)
		}

		// Update message status updated_at
		if err := tx.Model(&MessageStatus{}).
			Where("message_id = ?", messageID).
			Update("updated_at", now).Error; err != nil {
			return fmt.Errorf("failed to update message status: %w", err)
		}

		return nil
	})
}

// Close closes the database connection
func (ds *DatabaseStorage) Close() error {
	if ds.db == nil {
		return fmt.Errorf("database instance is nil")
	}
	db, err := ds.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}
	return db.Close()
}

// HealthCheck performs a health check on the database connection
func (ds *DatabaseStorage) HealthCheck(ctx context.Context) error {
	if ds.db == nil {
		return fmt.Errorf("database instance is nil")
	}
	db, err := ds.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	return nil
}

// GetStats returns storage statistics
func (ds *DatabaseStorage) GetStats(ctx context.Context) (StorageStats, error) {
	stats := StorageStats{}

	// Get total messages count
	if err := ds.db.WithContext(ctx).Model(&Message{}).Count(&stats.TotalMessages).Error; err != nil {
		return stats, fmt.Errorf("failed to count total messages: %w", err)
	}

	// Get total statuses count
	if err := ds.db.WithContext(ctx).Model(&MessageStatus{}).Count(&stats.TotalStatuses).Error; err != nil {
		return stats, fmt.Errorf("failed to count total statuses: %w", err)
	}

	// Count messages by status
	var statusCounts []struct {
		Status DeliveryStatus
		Count  int64
	}
	if err := ds.db.WithContext(ctx).Model(&MessageStatus{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Find(&statusCounts).Error; err != nil {
		return stats, fmt.Errorf("failed to count messages by status: %w", err)
	}

	for _, sc := range statusCounts {
		switch sc.Status {
		case StatusPending, StatusQueued, StatusDelivering:
			stats.PendingMessages += sc.Count
		case StatusDelivered:
			stats.DeliveredMessages += sc.Count
		case StatusFailed:
			stats.FailedMessages += sc.Count
		}
	}

	// Count inbox and acknowledged messages
	var inboxStats []struct {
		InboxDelivered bool
		Acknowledged   bool
		Count          int64
	}
	if err := ds.db.WithContext(ctx).Model(&RecipientStatus{}).
		Select("inbox_delivered, acknowledged, COUNT(*) as count").
		Where("local_delivery = ?", true).
		Group("inbox_delivered, acknowledged").
		Find(&inboxStats).Error; err != nil {
		return stats, fmt.Errorf("failed to count inbox messages: %w", err)
	}

	for _, is := range inboxStats {
		if is.InboxDelivered {
			if is.Acknowledged {
				stats.AcknowledgedMessages += is.Count
			} else {
				stats.InboxMessages += is.Count
			}
		}
	}

	return stats, nil
}

// Agents operations

// CreateAgent creates a new agent in the database
func (ds *DatabaseStorage) CreateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}

	dbAgent, err := ds.convertToDBAgent(agent)
	if err != nil {
		return fmt.Errorf("failed to convert agent: %w", err)
	}

	if err := ds.db.WithContext(ctx).Create(dbAgent).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("agent already exists: %s", agent.Address)
		}
		return fmt.Errorf("failed to create agent: %w", err)
	}

	return nil
}

// GetAgent retrieves an agent by address
func (ds *DatabaseStorage) GetAgent(ctx context.Context, agentAddress string) (*agents.LocalAgent, error) {
	if agentAddress == "" {
		return nil, fmt.Errorf("agent address cannot be empty")
	}

	var dbAgent Agent
	if err := ds.db.WithContext(ctx).
		Where("address = ?", agentAddress).
		First(&dbAgent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("agent not found: %s", agentAddress)
		}
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	agent, err := ds.convertToLocalAgent(&dbAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to convert agent: %w", err)
	}

	return agent, nil
}

// UpdateAgent updates an existing agent in the database
func (ds *DatabaseStorage) UpdateAgent(ctx context.Context, agent *agents.LocalAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}

	updates, err := ds.agentToUpdateMap(agent)
	if err != nil {
		return fmt.Errorf("failed to prepare agent update: %w", err)
	}

	result := ds.db.WithContext(ctx).
		Model(&Agent{}).
		Where("address = ?", agent.Address).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update agent: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("agent not found: %s", agent.Address)
	}

	return nil
}

// DeleteAgent deletes an agent from the database
func (ds *DatabaseStorage) DeleteAgent(ctx context.Context, agentAddress string) error {
	if agentAddress == "" {
		return fmt.Errorf("agent address cannot be empty")
	}

	result := ds.db.WithContext(ctx).
		Where("address = ?", agentAddress).
		Delete(&Agent{})

	if result.Error != nil {
		return fmt.Errorf("failed to delete agent: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("agent not found: %s", agentAddress)
	}

	return nil
}

// ListAgents lists all agents in the database
func (ds *DatabaseStorage) ListAgents(ctx context.Context) ([]*agents.LocalAgent, error) {
	var dbAgents []Agent
	if err := ds.db.WithContext(ctx).Find(&dbAgents).Error; err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	var agentsList []*agents.LocalAgent
	for i := range dbAgents {
		agent, err := ds.convertToLocalAgent(&dbAgents[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert agent: %w", err)
		}
		agentsList = append(agentsList, agent)
	}

	return agentsList, nil
}

// GetSupportedSchemas retrieves all unique supported schema IDs across agents
func (ds *DatabaseStorage) GetSupportedSchemas(ctx context.Context) ([]string, error) {
	var dbAgents []Agent
	if err := ds.db.WithContext(ctx).
		Select("supported_schemas").
		Find(&dbAgents).Error; err != nil {
		return nil, fmt.Errorf("failed to get supported schemas: %w", err)
	}

	schemaSet := make(map[string]struct{})
	for i := range dbAgents {
		var schemas []string
		if len(dbAgents[i].SupportedSchemas) == 0 {
			continue
		}
		if err := json.Unmarshal(dbAgents[i].SupportedSchemas, &schemas); err != nil {
			return nil, fmt.Errorf("failed to parse supported schemas: %w", err)
		}
		for _, schemaID := range schemas {
			if schemaID == "" {
				continue
			}
			schemaSet[schemaID] = struct{}{}
		}
	}

	var schemas []string
	for schemaID := range schemaSet {
		schemas = append(schemas, schemaID)
	}

	return schemas, nil
}

// Helper functions for conversion between types and models

func (ds *DatabaseStorage) convertToDBMessage(message *types.Message) (*Message, error) {
	var inReplyToStr *string
	if message.InReplyTo != "" {
		inReplyToStr = &message.InReplyTo
	}

	dbMessage := &Message{
		Version:        message.Version,
		MessageID:      message.MessageID,
		IdempotencyKey: message.IdempotencyKey,
		Timestamp:      message.Timestamp,
		Sender:         message.Sender,
		Subject:        message.Subject,
		Schema:         message.Schema,
		InReplyTo:      inReplyToStr,
		ResponseType:   message.ResponseType,
	}

	// Convert recipients
	if err := dbMessage.SetRecipients(message.Recipients); err != nil {
		return nil, fmt.Errorf("failed to set recipients: %w", err)
	}

	// Convert coordination
	if message.Coordination != nil {
		coordination := &types.CoordinationConfig{
			Type:              message.Coordination.Type,
			Timeout:           message.Coordination.Timeout,
			RequiredResponses: message.Coordination.RequiredResponses,
			OptionalResponses: message.Coordination.OptionalResponses,
			Sequence:          message.Coordination.Sequence,
			StopOnFailure:     message.Coordination.StopOnFailure,
		}

		// Convert conditions
		for _, condition := range message.Coordination.Conditions {
			coordination.Conditions = append(coordination.Conditions, types.ConditionalRule{
				If:   condition.If,
				Then: condition.Then,
				Else: condition.Else,
			})
		}

		if err := dbMessage.SetCoordination(coordination); err != nil {
			return nil, fmt.Errorf("failed to set coordination: %w", err)
		}
	}

	// Convert headers
	if message.Headers != nil {
		headersJSON, err := json.Marshal(message.Headers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal headers: %w", err)
		}
		dbMessage.Headers = datatypes.JSON(headersJSON)
	}

	// Convert payload
	if message.Payload != nil {
		dbMessage.Payload = datatypes.JSON(message.Payload)
	}

	// Convert attachments
	if len(message.Attachments) > 0 {
		var attachments []types.Attachment
		for _, attachment := range message.Attachments {
			attachments = append(attachments, types.Attachment{
				Filename:    attachment.Filename,
				ContentType: attachment.ContentType,
				Size:        attachment.Size,
				Hash:        attachment.Hash,
				URL:         attachment.URL,
			})
		}
		attachmentsJSON, err := json.Marshal(attachments)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal attachments: %w", err)
		}
		dbMessage.Attachments = datatypes.JSON(attachmentsJSON)
	}

	// Convert signature
	if message.Signature != nil {
		signature := &types.MessageSignature{
			Algorithm: message.Signature.Algorithm,
			KeyID:     message.Signature.KeyID,
			Value:     message.Signature.Value,
		}
		signatureJSON, err := json.Marshal(signature)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal signature: %w", err)
		}
		dbMessage.Signature = datatypes.JSON(signatureJSON)
	}

	return dbMessage, nil
}

func (ds *DatabaseStorage) convertToTypesMessage(dbMessage *Message) (*types.Message, error) {
	var inReplyToStr string
	if dbMessage.InReplyTo != nil {
		inReplyToStr = *dbMessage.InReplyTo
	}

	message := &types.Message{
		Version:        dbMessage.Version,
		MessageID:      dbMessage.MessageID,
		IdempotencyKey: dbMessage.IdempotencyKey,
		Timestamp:      dbMessage.Timestamp,
		Sender:         dbMessage.Sender,
		Subject:        dbMessage.Subject,
		Schema:         dbMessage.Schema,
		InReplyTo:      inReplyToStr,
		ResponseType:   dbMessage.ResponseType,
	}

	// Convert recipients
	recipients, err := dbMessage.GetRecipients()
	if err != nil {
		return nil, fmt.Errorf("failed to get recipients: %w", err)
	}
	message.Recipients = recipients

	// Convert coordination
	coordination, err := dbMessage.GetCoordination()
	if err != nil {
		return nil, fmt.Errorf("failed to get coordination: %w", err)
	}
	if coordination != nil {
		message.Coordination = &types.CoordinationConfig{
			Type:              coordination.Type,
			Timeout:           coordination.Timeout,
			RequiredResponses: coordination.RequiredResponses,
			OptionalResponses: coordination.OptionalResponses,
			Sequence:          coordination.Sequence,
			StopOnFailure:     coordination.StopOnFailure,
		}

		for _, condition := range coordination.Conditions {
			message.Coordination.Conditions = append(message.Coordination.Conditions, types.ConditionalRule{
				If:   condition.If,
				Then: condition.Then,
				Else: condition.Else,
			})
		}
	}

	// Convert headers
	if len(dbMessage.Headers) > 0 {
		var headers map[string]interface{}
		if err := json.Unmarshal(dbMessage.Headers, &headers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal headers: %w", err)
		}
		message.Headers = headers
	}

	// Convert payload
	if len(dbMessage.Payload) > 0 {
		message.Payload = json.RawMessage(dbMessage.Payload)
	}

	// Convert attachments
	if len(dbMessage.Attachments) > 0 {
		var attachments []types.Attachment
		if err := json.Unmarshal(dbMessage.Attachments, &attachments); err != nil {
			return nil, fmt.Errorf("failed to unmarshal attachments: %w", err)
		}
		for _, attachment := range attachments {
			message.Attachments = append(message.Attachments, types.Attachment{
				Filename:    attachment.Filename,
				ContentType: attachment.ContentType,
				Size:        attachment.Size,
				Hash:        attachment.Hash,
				URL:         attachment.URL,
			})
		}
	}

	// Convert signature
	if len(dbMessage.Signature) > 0 {
		var signature types.MessageSignature
		if err := json.Unmarshal(dbMessage.Signature, &signature); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signature: %w", err)
		}
		message.Signature = &types.MessageSignature{
			Algorithm: signature.Algorithm,
			KeyID:     signature.KeyID,
			Value:     signature.Value,
		}
	}

	return message, nil
}

func (ds *DatabaseStorage) convertToTypesMessageStatus(messageStatus *MessageStatus, recipientStatuses []RecipientStatus) (*types.MessageStatus, error) {
	status := &types.MessageStatus{
		MessageID:   messageStatus.MessageID,
		Status:      types.DeliveryStatus(messageStatus.Status),
		Attempts:    messageStatus.Attempts,
		NextRetry:   messageStatus.NextRetry,
		CreatedAt:   messageStatus.CreatedAt,
		UpdatedAt:   messageStatus.UpdatedAt,
		DeliveredAt: messageStatus.DeliveredAt,
	}

	// Convert recipient statuses
	for _, rs := range recipientStatuses {
		status.Recipients = append(status.Recipients, types.RecipientStatus{
			Address:        rs.Address,
			Status:         types.DeliveryStatus(rs.Status),
			Timestamp:      rs.Timestamp,
			Attempts:       rs.Attempts,
			ErrorCode:      rs.ErrorCode,
			ErrorMessage:   rs.ErrorMessage,
			DeliveryMode:   rs.DeliveryMode,
			LocalDelivery:  rs.LocalDelivery,
			InboxDelivered: rs.InboxDelivered,
			Acknowledged:   rs.Acknowledged,
			AcknowledgedAt: rs.AcknowledgedAt,
		})
	}

	return status, nil
}

// convertToDBAgent converts a LocalAgent to Agent model
func (ds *DatabaseStorage) convertToDBAgent(agent *agents.LocalAgent) (*Agent, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent cannot be nil")
	}

	dbAgent := &Agent{
		Address:        agent.Address,
		DeliveryMode:   agent.DeliveryMode,
		APIKey:         agent.APIKey,
		RequiresSchema: agent.RequiresSchema,
	}

	if agent.PushTarget != "" {
		pushTarget := agent.PushTarget
		dbAgent.PushTarget = &pushTarget
	}

	if headersJSON, err := json.Marshal(agent.Headers); err != nil {
		return nil, fmt.Errorf("failed to marshal headers: %w", err)
	} else if string(headersJSON) != "null" {
		dbAgent.Headers = datatypes.JSON(headersJSON)
	}

	schemasJSON, err := json.Marshal(agent.SupportedSchemas)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal supported schemas: %w", err)
	}
	if len(schemasJSON) == 0 || string(schemasJSON) == "null" {
		schemasJSON = []byte("[]")
	}
	dbAgent.SupportedSchemas = datatypes.JSON(schemasJSON)

	if agent.CreatedAt.IsZero() {
		dbAgent.CreatedAt = time.Now().UTC()
	} else {
		dbAgent.CreatedAt = agent.CreatedAt
	}

	if !agent.LastAccess.IsZero() {
		lastAccess := agent.LastAccess
		dbAgent.LastAccess = &lastAccess
	}

	return dbAgent, nil
}

// convertToLocalAgent converts an Agent model to LocalAgent
func (ds *DatabaseStorage) convertToLocalAgent(dbAgent *Agent) (*agents.LocalAgent, error) {
	if dbAgent == nil {
		return nil, fmt.Errorf("database agent cannot be nil")
	}

	var headers map[string]string
	if len(dbAgent.Headers) > 0 {
		if err := json.Unmarshal(dbAgent.Headers, &headers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal headers: %w", err)
		}
	}

	var supportedSchemas []string
	if len(dbAgent.SupportedSchemas) > 0 {
		if err := json.Unmarshal(dbAgent.SupportedSchemas, &supportedSchemas); err != nil {
			return nil, fmt.Errorf("failed to unmarshal supported schemas: %w", err)
		}
	}

	localAgent := &agents.LocalAgent{
		Address:          dbAgent.Address,
		DeliveryMode:     dbAgent.DeliveryMode,
		Headers:          headers,
		APIKey:           dbAgent.APIKey,
		SupportedSchemas: supportedSchemas,
		RequiresSchema:   dbAgent.RequiresSchema,
		CreatedAt:        dbAgent.CreatedAt,
	}

	if dbAgent.PushTarget != nil {
		localAgent.PushTarget = *dbAgent.PushTarget
	}

	if dbAgent.LastAccess != nil {
		localAgent.LastAccess = *dbAgent.LastAccess
	}

	return localAgent, nil
}

// agentToUpdateMap prepares a map of fields to update for an agent
func (ds *DatabaseStorage) agentToUpdateMap(agent *agents.LocalAgent) (map[string]interface{}, error) {
	updates := map[string]interface{}{
		"delivery_mode":   agent.DeliveryMode,
		"api_key":         agent.APIKey,
		"requires_schema": agent.RequiresSchema,
		"push_target":     nil,
		"last_access":     nil,
	}

	if agent.PushTarget != "" {
		updates["push_target"] = agent.PushTarget
	}

	if !agent.LastAccess.IsZero() {
		updates["last_access"] = agent.LastAccess
	}

	headersJSON, err := json.Marshal(agent.Headers)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal headers: %w", err)
	}
	if string(headersJSON) != "null" {
		updates["headers"] = datatypes.JSON(headersJSON)
	} else {
		updates["headers"] = datatypes.JSON([]byte("null"))
	}

	schemasJSON, err := json.Marshal(agent.SupportedSchemas)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal supported schemas: %w", err)
	}
	if len(schemasJSON) == 0 || string(schemasJSON) == "null" {
		schemasJSON = []byte("[]")
	}
	updates["supported_schemas"] = datatypes.JSON(schemasJSON)

	return updates, nil
}
