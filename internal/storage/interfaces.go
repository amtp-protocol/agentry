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

	"github.com/amtp-protocol/agentry/internal/types"
)

// MessageStorage defines the interface for message storage operations
type MessageStorage interface {
	// Message operations
	StoreMessage(ctx context.Context, message *types.Message) error
	GetMessage(ctx context.Context, messageID string) (*types.Message, error)
	DeleteMessage(ctx context.Context, messageID string) error
	ListMessages(ctx context.Context, filter MessageFilter) ([]*types.Message, error)

	// Status operations
	StoreStatus(ctx context.Context, messageID string, status *types.MessageStatus) error
	GetStatus(ctx context.Context, messageID string) (*types.MessageStatus, error)
	UpdateStatus(ctx context.Context, messageID string, updater StatusUpdater) error
	DeleteStatus(ctx context.Context, messageID string) error

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
	Since      *int64 // Unix timestamp
	Limit      int
	Offset     int
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

	// Database storage config (for future use)
	Database *DatabaseStorageConfig `yaml:"database,omitempty" json:"database,omitempty"`

	// Redis storage config (for future use)
	Redis *RedisStorageConfig `yaml:"redis,omitempty" json:"redis,omitempty"`
}

// MemoryStorageConfig configures in-memory storage
type MemoryStorageConfig struct {
	MaxMessages int `yaml:"max_messages" json:"max_messages"` // 0 = unlimited
	TTL         int `yaml:"ttl_hours" json:"ttl_hours"`       // 0 = no expiration
}

// DatabaseStorageConfig configures database storage (placeholder for future)
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
