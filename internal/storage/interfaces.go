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

package storage

import (
	"context"
	"errors"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/types"
)

// ErrVersionConflict is returned by atomic workflow update methods when the
// expected version does not match the stored version, indicating a concurrent
// modification. The caller should re-read the workflow and retry.
var ErrVersionConflict = errors.New("version conflict: workflow was modified concurrently")

// ErrWorkflowNotFound is returned when a workflow does not exist in storage.
// In a multi-gateway deployment, callers use this sentinel to distinguish
// "this replica does not own the workflow" (benign) from other failures.
var ErrWorkflowNotFound = errors.New("workflow not found")

// ErrMessageNotFound is returned (wrapped with the message ID for context) by
// GetMessage and GetStatus when the requested message or its status does not
// exist. Callers use errors.Is to distinguish a genuine not-found (404) from
// transient storage failures (5xx), which must not be flattened to 404 — a
// sender polling status would otherwise conclude the message is lost and
// re-send it, producing duplicate delivery.
var ErrMessageNotFound = errors.New("message not found")

// ErrAgentNotFound is returned (wrapped with the address for context) by
// agent lookups and updates when the addressed agent does not exist. Callers
// use errors.Is to distinguish a genuine not-found (404) from transient
// storage failures (5xx), so an operator's retry script can tell "you asked
// for an agent that does not exist" (do not retry) from "the gateway's DB is
// down" (retry).
var ErrAgentNotFound = errors.New("agent not found")

// Storage defines the interface for message storage operations
type Storage interface {
	agents.AgentStore

	// Message operations
	StoreMessage(ctx context.Context, message *types.Message) error
	GetMessage(ctx context.Context, messageID string) (*types.Message, error)
	DeleteMessage(ctx context.Context, messageID string) error
	ListMessages(ctx context.Context, filter MessageFilter) ([]*types.Message, error)
	// CountMessages returns the number of messages matching the filter
	// criteria without materializing the result set. Limit and Offset are
	// ignored: the count always covers the full filtered set.
	CountMessages(ctx context.Context, filter MessageFilter) (int64, error)

	// Status operations
	StoreStatus(ctx context.Context, messageID string, status *types.MessageStatus) error
	GetStatus(ctx context.Context, messageID string) (*types.MessageStatus, error)
	// GetStatuses returns the delivery statuses for the given message IDs in
	// one batch operation. IDs without a stored status are omitted from the
	// result, keyed by message ID.
	GetStatuses(ctx context.Context, messageIDs []string) (map[string]*types.MessageStatus, error)
	UpdateStatus(ctx context.Context, messageID string, updater StatusUpdater) error
	DeleteStatus(ctx context.Context, messageID string) error

	// Workflow operations
	StoreWorkflow(ctx context.Context, state *types.Workflow) error
	GetWorkflow(ctx context.Context, workflowID string) (*types.Workflow, error)
	UpdateWorkflowStatus(ctx context.Context, workflowID string, status types.WorkflowStatus) error
	UpdateWorkflowParticipant(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte) error
	ListTimedOutWorkflows(ctx context.Context) ([]*types.Workflow, error)

	// Optimistic-concurrency workflow operations.
	// These fail with ErrVersionConflict when the expected version does not match.
	UpdateWorkflowParticipantAtomic(ctx context.Context, workflowID string, address string, status types.ParticipantStatus, responsePayload []byte, expectedVersion int) error
	UpdateWorkflowStatusAtomic(ctx context.Context, workflowID string, status types.WorkflowStatus, expectedVersion int) error

	// Inbox operations (view-based queries)
	GetInboxMessages(ctx context.Context, recipient string) ([]*types.Message, error)
	AcknowledgeMessage(ctx context.Context, recipient, messageID string) error

	// Maintenance operations
	Close() error
	HealthCheck(ctx context.Context) error
	GetStats(ctx context.Context) (StorageStats, error)
}

// MessageFilter defines filtering criteria for message queries
type MessageFilter struct {
	Sender     string
	Recipients []string
	Status     types.DeliveryStatus
	// Since is an inclusive lower bound on the message timestamp: messages
	// with Timestamp >= Since are returned. It is kept as a full-precision
	// time.Time end to end so cursor-style polling with sub-second cursors
	// (e.g. ?since=2026-08-09T12:00:00.999Z) does not re-receive messages
	// stamped within the same second.
	Since  *time.Time
	Limit  int
	Offset int
	Or     bool
}

// StatusUpdater is a function that updates message status
type StatusUpdater func(status *types.MessageStatus) error

// StorageStats provides storage statistics
type StorageStats struct {
	TotalMessages        int64 `json:"total_messages"`
	TotalStatuses        int64 `json:"total_statuses"`
	PendingMessages      int64 `json:"pending_messages"`
	DeliveredMessages    int64 `json:"delivered_messages"`
	FailedMessages       int64 `json:"failed_messages"`
	InboxMessages        int64 `json:"inbox_messages"`
	AcknowledgedMessages int64 `json:"acknowledged_messages"`
}

// StorageConfig defines configuration for storage implementations
type StorageConfig struct {
	Type string `yaml:"type" json:"type"` // "memory", "postgres", "redis", etc.

	// Memory storage config
	Memory *MemoryStorageConfig `yaml:"memory,omitempty" json:"memory,omitempty"`

	// Database storage config
	Database *DatabaseStorageConfig `yaml:"database,omitempty" json:"database,omitempty"`

	// Redis storage config (for future use)
	Redis *RedisStorageConfig `yaml:"redis,omitempty" json:"redis,omitempty"`
}

// MemoryStorageConfig configures in-memory storage
type MemoryStorageConfig struct {
	MaxMessages int `yaml:"max_messages" json:"max_messages"` // 0 = unlimited
	TTL         int `yaml:"ttl_hours" json:"ttl_hours"`       // 0 = no expiration
}

// DatabaseStorageConfig configures database storage
type DatabaseStorageConfig struct {
	Driver           string `yaml:"driver" json:"driver"`
	ConnectionString string `yaml:"connection_string" json:"connection_string"`
	MaxConnections   int    `yaml:"max_connections" json:"max_connections"`
	MaxIdleTime      int    `yaml:"max_idle_time" json:"max_idle_time"`
}

// RedisStorageConfig configures Redis storage (placeholder for future)
type RedisStorageConfig struct {
	Address  string `yaml:"address" json:"address"`
	Password string `yaml:"password" json:"password"`
	Database int    `yaml:"database" json:"database"`
	TTL      int    `yaml:"ttl_hours" json:"ttl_hours"`
}
