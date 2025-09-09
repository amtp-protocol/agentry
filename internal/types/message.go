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

package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message represents an AMTP message according to the protocol specification
type Message struct {
	Version        string                 `json:"version" validate:"required,eq=1.0"`
	MessageID      string                 `json:"message_id" validate:"required,uuidv7"`
	IdempotencyKey string                 `json:"idempotency_key" validate:"required,uuid4"`
	Timestamp      time.Time              `json:"timestamp" validate:"required"`
	Sender         string                 `json:"sender" validate:"required,email"`
	Recipients     []string               `json:"recipients" validate:"required,min=1,dive,email"`
	Subject        string                 `json:"subject,omitempty"`
	Schema         string                 `json:"schema,omitempty"`
	Coordination   *CoordinationConfig    `json:"coordination,omitempty"`
	Headers        map[string]interface{} `json:"headers,omitempty"`
	Payload        json.RawMessage        `json:"payload,omitempty"`
	Attachments    []Attachment           `json:"attachments,omitempty"`
	Signature      *MessageSignature      `json:"signature,omitempty"`
	InReplyTo      string                 `json:"in_reply_to,omitempty" validate:"omitempty,uuidv7"`
	ResponseType   string                 `json:"response_type,omitempty"`
}

// CoordinationConfig defines multi-agent coordination parameters
type CoordinationConfig struct {
	Type              string            `json:"type" validate:"required,oneof=parallel sequential conditional"`
	Timeout           int               `json:"timeout" validate:"min=1"` // seconds
	RequiredResponses []string          `json:"required_responses,omitempty" validate:"dive,email"`
	OptionalResponses []string          `json:"optional_responses,omitempty" validate:"dive,email"`
	Sequence          []string          `json:"sequence,omitempty" validate:"dive,email"`
	StopOnFailure     bool              `json:"stop_on_failure,omitempty"`
	Conditions        []ConditionalRule `json:"conditions,omitempty"`
}

// ConditionalRule defines conditional execution logic
type ConditionalRule struct {
	If   string   `json:"if" validate:"required"`
	Then []string `json:"then" validate:"required,min=1,dive,email"`
	Else []string `json:"else,omitempty" validate:"dive,email"`
}

// Attachment represents a file attachment reference
type Attachment struct {
	Filename    string `json:"filename" validate:"required"`
	ContentType string `json:"content_type" validate:"required"`
	Size        int64  `json:"size" validate:"min=0"`
	Hash        string `json:"hash" validate:"required"`
	URL         string `json:"url" validate:"required,url"`
}

// MessageSignature represents a digital signature
type MessageSignature struct {
	Algorithm string `json:"algorithm" validate:"required,oneof=RS256 ES256"`
	KeyID     string `json:"keyid,omitempty"`
	Value     string `json:"value" validate:"required"`
}

// MessageStatus represents the delivery status of a message
type MessageStatus struct {
	MessageID   string            `json:"message_id"`
	Status      DeliveryStatus    `json:"status"`
	Recipients  []RecipientStatus `json:"recipients"`
	Attempts    int               `json:"attempts"`
	NextRetry   *time.Time        `json:"next_retry,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	DeliveredAt *time.Time        `json:"delivered_at,omitempty"`
}

// RecipientStatus represents the delivery status for a specific recipient
type RecipientStatus struct {
	Address        string         `json:"address"`
	Status         DeliveryStatus `json:"status"`
	Timestamp      time.Time      `json:"timestamp"`
	Attempts       int            `json:"attempts"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	DeliveryMode   string         `json:"delivery_mode,omitempty"`   // "push" or "pull"
	LocalDelivery  bool           `json:"local_delivery,omitempty"`  // true if delivered locally
	InboxDelivered bool           `json:"inbox_delivered,omitempty"` // true if available in inbox
	Acknowledged   bool           `json:"acknowledged,omitempty"`    // true if acknowledged by recipient
	AcknowledgedAt *time.Time     `json:"acknowledged_at,omitempty"` // when acknowledged
}

// DeliveryStatus represents possible message delivery states
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusQueued     DeliveryStatus = "queued"
	StatusDelivering DeliveryStatus = "delivering"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusFailed     DeliveryStatus = "failed"
	StatusRetrying   DeliveryStatus = "retrying"
)

// SendMessageRequest represents the API request to send a message
type SendMessageRequest struct {
	Sender       string                 `json:"sender" validate:"required,email"`
	Recipients   []string               `json:"recipients" validate:"required,min=1,dive,email"`
	Subject      string                 `json:"subject,omitempty"`
	Schema       string                 `json:"schema,omitempty"`
	Coordination *CoordinationConfig    `json:"coordination,omitempty"`
	Headers      map[string]interface{} `json:"headers,omitempty"`
	Payload      json.RawMessage        `json:"payload,omitempty"`
	Attachments  []Attachment           `json:"attachments,omitempty"`
}

// SendMessageResponse represents the API response for sending a message
type SendMessageResponse struct {
	MessageID  string            `json:"message_id"`
	Status     string            `json:"status"`
	Recipients []RecipientStatus `json:"recipients"`
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail provides detailed error information
type ErrorDetail struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id,omitempty"`
}

// Validate validates the message structure
func (m *Message) Validate() error {
	if m.Version != "1.0" {
		return fmt.Errorf("unsupported protocol version: %s", m.Version)
	}

	if m.MessageID == "" {
		return fmt.Errorf("message_id is required")
	}

	if m.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key is required")
	}

	if m.Sender == "" {
		return fmt.Errorf("sender is required")
	}

	if len(m.Recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	// Validate coordination if present
	if m.Coordination != nil {
		if err := m.Coordination.Validate(); err != nil {
			return fmt.Errorf("invalid coordination config: %w", err)
		}
	}

	return nil
}

// Validate validates the coordination configuration
func (c *CoordinationConfig) Validate() error {
	switch c.Type {
	case "parallel":
		if c.Timeout <= 0 {
			return fmt.Errorf("timeout must be positive for parallel coordination")
		}
	case "sequential":
		if len(c.Sequence) == 0 {
			return fmt.Errorf("sequence is required for sequential coordination")
		}
	case "conditional":
		if len(c.Conditions) == 0 {
			return fmt.Errorf("conditions are required for conditional coordination")
		}
	default:
		return fmt.Errorf("unsupported coordination type: %s", c.Type)
	}

	return nil
}

// Size returns the approximate size of the message in bytes
func (m *Message) Size() int64 {
	data, err := json.Marshal(m)
	if err != nil {
		return 0
	}
	return int64(len(data))
}
