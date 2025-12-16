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
	"fmt"
	"strings"
)

// NewStorage creates a new storage instance based on the configuration
func NewStorage(config StorageConfig) (Storage, error) {
	storageType := strings.ToLower(config.Type)
	if storageType == "" {
		storageType = "memory" // Default to memory storage
	}

	switch storageType {
	case "memory":
		memConfig := MemoryStorageConfig{}
		if config.Memory != nil {
			memConfig = *config.Memory
		}
		return NewMemoryStorage(memConfig), nil

	case "database":
		dbConfig := DatabaseStorageConfig{}
		if config.Database != nil {
			dbConfig = *config.Database
		}
		return NewDatabaseStorage(dbConfig)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}

// DefaultMemoryConfig returns a default memory storage configuration
func DefaultMemoryConfig() MemoryStorageConfig {
	return MemoryStorageConfig{
		MaxMessages: 0,  // Unlimited
		TTL:         24, // 24 hours
	}
}

// DefaultStorageConfig returns a default storage configuration
func DefaultStorageConfig() StorageConfig {
	return StorageConfig{
		Type: "memory",
		Memory: &MemoryStorageConfig{
			MaxMessages: 0,  // Unlimited
			TTL:         24, // 24 hours
		},
	}
}
