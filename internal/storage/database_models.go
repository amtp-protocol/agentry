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
	"encoding/json"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// DeliveryStatus enum type
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusQueued     DeliveryStatus = "queued"
	StatusDelivering DeliveryStatus = "delivering"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusFailed     DeliveryStatus = "failed"
	StatusRetrying   DeliveryStatus = "retrying"
)

// Message model
type Message struct {
	ID             uint      `gorm:"primarykey" json:"-"`
	Version        string    `gorm:"size:10;not null;default:1.0" json:"version" validate:"required,eq=1.0"`
	MessageID      string    `gorm:"type:uuid;uniqueIndex;not null" json:"message_id" validate:"required,uuidv7"`
	IdempotencyKey string    `gorm:"type:uuid;uniqueIndex;not null" json:"idempotency_key" validate:"required,uuid4"`
	Timestamp      time.Time `gorm:"type:timestamptz;not null" json:"timestamp" validate:"required"`
	Sender         string    `gorm:"size:255;not null" json:"sender" validate:"required,email"`
	Subject        string    `gorm:"type:text" json:"subject,omitempty"`
	Schema         string    `gorm:"type:text" json:"schema,omitempty"`
	InReplyTo      *string   `gorm:"type:uuid" json:"in_reply_to,omitempty" validate:"omitempty,uuid"`
	ResponseType   string    `gorm:"size:50" json:"response_type,omitempty"`

	// JSON fields
	Recipients   datatypes.JSON `gorm:"type:jsonb;not null" json:"recipients" validate:"required"`
	Coordination datatypes.JSON `gorm:"type:jsonb" json:"coordination,omitempty"`
	Headers      datatypes.JSON `gorm:"type:jsonb" json:"headers,omitempty"`
	Payload      datatypes.JSON `gorm:"type:jsonb" json:"payload,omitempty"`
	Attachments  datatypes.JSON `gorm:"type:jsonb" json:"attachments,omitempty"`
	Signature    datatypes.JSON `gorm:"type:jsonb" json:"signature,omitempty"`

	// Relationships
	MessageStatus   MessageStatus     `gorm:"foreignKey:MessageID;references:MessageID" json:"status,omitempty"`
	RecipientStatus []RecipientStatus `gorm:"foreignKey:MessageID;references:MessageID" json:"recipient_statuses,omitempty"`
}

// MessageStatus message status model
type MessageStatus struct {
	ID          uint           `gorm:"primarykey" json:"-"`
	MessageID   string         `gorm:"type:uuid;uniqueIndex;not null" json:"message_id"`
	Status      DeliveryStatus `gorm:"type:delivery_status;not null;default:'pending'" json:"status"`
	Attempts    int            `gorm:"not null;default:0" json:"attempts"`
	NextRetry   *time.Time     `gorm:"type:timestamptz" json:"next_retry,omitempty"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
	DeliveredAt *time.Time     `gorm:"type:timestamptz" json:"delivered_at,omitempty"`
}

// RecipientStatus recipient status model
type RecipientStatus struct {
	ID             uint           `gorm:"primarykey" json:"-"`
	MessageID      string         `gorm:"type:uuid;index;not null" json:"message_id"`
	Address        string         `gorm:"size:255;not null" json:"address" validate:"email"`
	Status         DeliveryStatus `gorm:"type:delivery_status;not null;default:'pending'" json:"status"`
	Timestamp      time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"timestamp"`
	Attempts       int            `gorm:"not null;default:0" json:"attempts"`
	ErrorCode      string         `gorm:"size:100" json:"error_code,omitempty"`
	ErrorMessage   string         `gorm:"type:text" json:"error_message,omitempty"`
	DeliveryMode   string         `gorm:"size:10;default:'push'" json:"delivery_mode,omitempty"`
	LocalDelivery  bool           `gorm:"default:false" json:"local_delivery,omitempty"`
	InboxDelivered bool           `gorm:"default:false" json:"inbox_delivered,omitempty"`
	Acknowledged   bool           `gorm:"default:false" json:"acknowledged,omitempty"`
	AcknowledgedAt *time.Time     `gorm:"type:timestamptz" json:"acknowledged_at,omitempty"`
}

// Agent model
type Agent struct {
	ID               uint           `gorm:"primarykey" json:"-"`
	Address          string         `gorm:"size:255;uniqueIndex;not null" json:"address" validate:"required,email"`
	DeliveryMode     string         `gorm:"size:10;not null;default:'push'" json:"delivery_mode" validate:"required,oneof=push pull"`
	PushTarget       *string        `gorm:"type:text" json:"push_target,omitempty" validate:"omitempty,url"`
	Headers          datatypes.JSON `gorm:"type:jsonb" json:"headers,omitempty"`
	APIKey           string         `gorm:"size:64;not null" json:"api_key" validate:"required"`
	SupportedSchemas datatypes.JSON `gorm:"type:jsonb;not null" json:"supported_schemas" validate:"required"`
	RequiresSchema   bool           `gorm:"not null;default:false" json:"requires_schema"`
	CreatedAt        time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	LastAccess       *time.Time     `gorm:"type:timestamptz" json:"last_access,omitempty"`
}

// Custom Gorm hooks and utility methods

// BeforeCreate hook before creation
func (m *Message) BeforeCreate(tx *gorm.DB) error {
	// Ensure timestamp is set correctly
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}
	return nil
}

// GetRecipients get recipients list
func (m *Message) GetRecipients() ([]string, error) {
	var recipients []string
	err := json.Unmarshal(m.Recipients, &recipients)
	return recipients, err
}

// SetRecipients set recipients list
func (m *Message) SetRecipients(recipients []string) error {
	data, err := json.Marshal(recipients)
	if err != nil {
		return err
	}
	m.Recipients = datatypes.JSON(data)
	return nil
}

// GetCoordination get coordination configuration
func (m *Message) GetCoordination() (*types.CoordinationConfig, error) {
	if len(m.Coordination) == 0 {
		return nil, nil
	}
	var coordination types.CoordinationConfig
	err := json.Unmarshal(m.Coordination, &coordination)
	return &coordination, err
}

// SetCoordination set coordination configuration
func (m *Message) SetCoordination(coordination *types.CoordinationConfig) error {
	if coordination == nil {
		m.Coordination = nil
		return nil
	}
	data, err := json.Marshal(coordination)
	if err != nil {
		return err
	}
	m.Coordination = datatypes.JSON(data)
	return nil
}

// TableName specify table name
func (Message) TableName() string {
	return "messages"
}

func (MessageStatus) TableName() string {
	return "message_statuses"
}

func (RecipientStatus) TableName() string {
	return "recipient_statuses"
}
