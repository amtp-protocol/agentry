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
	"testing"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestNewStorage_Memory(t *testing.T) {
	config := StorageConfig{
		Type: "memory",
		Memory: &MemoryStorageConfig{
			MaxMessages: 1000,
			TTL:         24,
		},
	}

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	// Verify it's a memory storage
	memStorage, ok := storage.(*MemoryStorage)
	if !ok {
		t.Error("Expected MemoryStorage instance")
	}

	if memStorage.config.MaxMessages != 1000 {
		t.Errorf("Expected MaxMessages to be 1000, got %d", memStorage.config.MaxMessages)
	}

	if memStorage.config.TTL != 24 {
		t.Errorf("Expected TTL to be 24, got %d", memStorage.config.TTL)
	}
}

func TestNewStorage_Memory_DefaultConfig(t *testing.T) {
	config := StorageConfig{
		Type: "memory",
		// No Memory config provided
	}

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	// Verify it's a memory storage with default config
	memStorage, ok := storage.(*MemoryStorage)
	if !ok {
		t.Error("Expected MemoryStorage instance")
	}

	// Should use default values (0 for unlimited)
	if memStorage.config.MaxMessages != 0 {
		t.Errorf("Expected default MaxMessages to be 0, got %d", memStorage.config.MaxMessages)
	}
}

func TestNewStorage_DefaultType(t *testing.T) {
	config := StorageConfig{
		// No Type specified, should default to memory
	}

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	// Verify it's a memory storage
	_, ok := storage.(*MemoryStorage)
	if !ok {
		t.Error("Expected MemoryStorage instance for default type")
	}
}

func TestNewStorage_EmptyType(t *testing.T) {
	config := StorageConfig{
		Type: "",
	}

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	// Verify it's a memory storage
	_, ok := storage.(*MemoryStorage)
	if !ok {
		t.Error("Expected MemoryStorage instance for empty type")
	}
}

func TestNewStorage_CaseInsensitive(t *testing.T) {
	testCases := []struct {
		name string
		typ  string
	}{
		{"uppercase", "MEMORY"},
		{"mixed case", "Memory"},
		{"mixed case 2", "MeMoRy"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := StorageConfig{
				Type: tc.typ,
			}

			storage, err := NewStorage(config)
			if err != nil {
				t.Fatalf("Expected no error for type %s, got %v", tc.typ, err)
			}

			if storage == nil {
				t.Fatalf("Expected storage to be created for type %s", tc.typ)
			}

			// Verify it's a memory storage
			_, ok := storage.(*MemoryStorage)
			if !ok {
				t.Errorf("Expected MemoryStorage instance for type %s", tc.typ)
			}
		})
	}
}

func TestNewStorage_UnsupportedTypes(t *testing.T) {
	unsupportedTypes := []string{
		"postgres",
		"postgresql",
		"redis",
		"mysql",
		"sqlite",
		"unknown",
	}

	for _, typ := range unsupportedTypes {
		t.Run(typ, func(t *testing.T) {
			config := StorageConfig{
				Type: typ,
			}

			storage, err := NewStorage(config)
			if err == nil {
				t.Errorf("Expected error for unsupported type %s", typ)
			}

			if storage != nil {
				t.Errorf("Expected nil storage for unsupported type %s", typ)
			}

			// All unsupported types should return appropriate error messages
			// We don't test specific messages since these are placeholders for future implementations
		})
	}
}

func TestDefaultMemoryConfig(t *testing.T) {
	config := DefaultMemoryConfig()

	if config.MaxMessages != 0 {
		t.Errorf("Expected default MaxMessages to be 0 (unlimited), got %d", config.MaxMessages)
	}

	if config.TTL != 24 {
		t.Errorf("Expected default TTL to be 24 hours, got %d", config.TTL)
	}
}

func TestDefaultStorageConfig(t *testing.T) {
	config := DefaultStorageConfig()

	if config.Type != "memory" {
		t.Errorf("Expected default type to be 'memory', got %s", config.Type)
	}

	if config.Memory == nil {
		t.Fatal("Expected Memory config to be set")
	}

	if config.Memory.MaxMessages != 0 {
		t.Errorf("Expected default MaxMessages to be 0 (unlimited), got %d", config.Memory.MaxMessages)
	}

	if config.Memory.TTL != 24 {
		t.Errorf("Expected default TTL to be 24 hours, got %d", config.Memory.TTL)
	}

	// Other configs should be nil for default
	if config.Database != nil {
		t.Error("Expected Database config to be nil for default")
	}

	if config.Redis != nil {
		t.Error("Expected Redis config to be nil for default")
	}
}

func TestStorageConfig_MemoryConfigPrecedence(t *testing.T) {
	// Test that specific Memory config is used correctly
	config := StorageConfig{
		Type: "memory",
		Memory: &MemoryStorageConfig{
			MaxMessages: 5000,
			TTL:         48,
		},
	}

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	memStorage, ok := storage.(*MemoryStorage)
	if !ok {
		t.Fatal("Expected MemoryStorage instance")
	}

	if memStorage.config.MaxMessages != 5000 {
		t.Errorf("Expected MaxMessages to be 5000, got %d", memStorage.config.MaxMessages)
	}

	if memStorage.config.TTL != 48 {
		t.Errorf("Expected TTL to be 48, got %d", memStorage.config.TTL)
	}
}

func TestNewStorage_Integration(t *testing.T) {
	// Test that created storage actually works
	config := StorageConfig{
		Type: "memory",
		Memory: &MemoryStorageConfig{
			MaxMessages: 10,
			TTL:         1,
		},
	}

	storage, err := NewStorage(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Test basic operations work
	ctx := context.Background()

	// Health check
	err = storage.HealthCheck(ctx)
	if err != nil {
		t.Errorf("Expected healthy storage, got %v", err)
	}

	// Store and retrieve
	message := &types.Message{
		MessageID: "test-integration",
		Sender:    "test@example.com",
	}

	err = storage.StoreMessage(ctx, message)
	if err != nil {
		t.Errorf("Expected no error storing message, got %v", err)
	}

	retrieved, err := storage.GetMessage(ctx, "test-integration")
	if err != nil {
		t.Errorf("Expected no error retrieving message, got %v", err)
	}

	if retrieved.MessageID != message.MessageID {
		t.Errorf("Expected MessageID %s, got %s", message.MessageID, retrieved.MessageID)
	}

	// Close
	err = storage.Close()
	if err != nil {
		t.Errorf("Expected no error closing storage, got %v", err)
	}
}
