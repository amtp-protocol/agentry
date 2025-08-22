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

package schema

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCacheEntry_IsExpired(t *testing.T) {
	tests := []struct {
		name     string
		entry    CacheEntry
		expected bool
	}{
		{
			name: "not expired",
			entry: CacheEntry{
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			expected: false,
		},
		{
			name: "expired",
			entry: CacheEntry{
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
			expected: true,
		},
		{
			name: "just expired",
			entry: CacheEntry{
				ExpiresAt: time.Now().Add(-1 * time.Millisecond),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.IsExpired()
			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestNewMemoryCache(t *testing.T) {
	tests := []struct {
		name            string
		config          CacheConfig
		expectedTTL     time.Duration
		expectedMaxSize int
		expectedCleanup time.Duration
	}{
		{
			name:            "default values",
			config:          CacheConfig{},
			expectedTTL:     1 * time.Hour,
			expectedMaxSize: 1000,
			expectedCleanup: 5 * time.Minute,
		},
		{
			name: "custom values",
			config: CacheConfig{
				DefaultTTL:      30 * time.Minute,
				MaxSize:         500,
				CleanupInterval: 2 * time.Minute,
			},
			expectedTTL:     30 * time.Minute,
			expectedMaxSize: 500,
			expectedCleanup: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMemoryCache(tt.config)

			if cache == nil {
				t.Errorf("expected cache to be created")
				return
			}

			if cache.defaultTTL != tt.expectedTTL {
				t.Errorf("expected default TTL %v, got %v", tt.expectedTTL, cache.defaultTTL)
			}

			if cache.maxSize != tt.expectedMaxSize {
				t.Errorf("expected max size %d, got %d", tt.expectedMaxSize, cache.maxSize)
			}

			if cache.cleanupInterval != tt.expectedCleanup {
				t.Errorf("expected cleanup interval %v, got %v", tt.expectedCleanup, cache.cleanupInterval)
			}

			// Clean up
			cache.Stop()
		})
	}
}

func TestMemoryCache_SetAndGet(t *testing.T) {
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	// Test Set
	err := cache.Set(ctx, schema, 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error setting schema: %v", err)
	}

	// Test Get
	retrieved, err := cache.Get(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema: %v", err)
	}

	if retrieved == nil {
		t.Errorf("expected schema to be retrieved")
		return
	}

	if retrieved.ID.String() != schemaID.String() {
		t.Errorf("expected schema ID %s, got %s", schemaID.String(), retrieved.ID.String())
	}

	if string(retrieved.Definition) != string(schema.Definition) {
		t.Errorf("expected definition %s, got %s", string(schema.Definition), string(retrieved.Definition))
	}
}

func TestMemoryCache_GetNonExistent(t *testing.T) {
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "nonexistent",
		Entity:  "schema",
		Version: "v1",
		Raw:     "agntcy:nonexistent.schema.v1",
	}

	_, err := cache.Get(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error for non-existent schema")
	}
}

func TestMemoryCache_GetExpired(t *testing.T) {
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	// Set with very short TTL
	err := cache.Set(ctx, schema, 1*time.Millisecond)
	if err != nil {
		t.Errorf("unexpected error setting schema: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Try to get expired schema
	_, err = cache.Get(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error for expired schema")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	// Set schema
	err := cache.Set(ctx, schema, 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error setting schema: %v", err)
	}

	// Verify it exists
	_, err = cache.Get(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema before delete: %v", err)
	}

	// Delete schema
	err = cache.Delete(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error deleting schema: %v", err)
	}

	// Verify it's gone
	_, err = cache.Get(ctx, schemaID)
	if err == nil {
		t.Errorf("expected error after deleting schema")
	}
}

func TestMemoryCache_Clear(t *testing.T) {
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	ctx := context.Background()

	// Add multiple schemas
	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition:  json.RawMessage(`{"type": "object"}`),
			PublishedAt: time.Now(),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			Definition:  json.RawMessage(`{"type": "object"}`),
			PublishedAt: time.Now(),
		},
	}

	for _, schema := range schemas {
		err := cache.Set(ctx, schema, 1*time.Hour)
		if err != nil {
			t.Errorf("unexpected error setting schema: %v", err)
		}
	}

	// Clear cache
	err := cache.Clear(ctx)
	if err != nil {
		t.Errorf("unexpected error clearing cache: %v", err)
	}

	// Verify all schemas are gone
	for _, schema := range schemas {
		_, err = cache.Get(ctx, schema.ID)
		if err == nil {
			t.Errorf("expected error after clearing cache for schema %s", schema.ID.String())
		}
	}
}

func TestMemoryCache_Eviction(t *testing.T) {
	// Create cache with small max size
	cache := NewMemoryCache(CacheConfig{MaxSize: 2})
	defer cache.Stop()

	ctx := context.Background()

	schemas := []*Schema{
		{
			ID: SchemaIdentifier{
				Domain:  "commerce",
				Entity:  "order",
				Version: "v1",
				Raw:     "agntcy:commerce.order.v1",
			},
			Definition:  json.RawMessage(`{"type": "object"}`),
			PublishedAt: time.Now(),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "messaging",
				Entity:  "notification",
				Version: "v1",
				Raw:     "agntcy:messaging.notification.v1",
			},
			Definition:  json.RawMessage(`{"type": "object"}`),
			PublishedAt: time.Now(),
		},
		{
			ID: SchemaIdentifier{
				Domain:  "analytics",
				Entity:  "event",
				Version: "v1",
				Raw:     "agntcy:analytics.event.v1",
			},
			Definition:  json.RawMessage(`{"type": "object"}`),
			PublishedAt: time.Now(),
		},
	}

	// Add first two schemas
	for i := 0; i < 2; i++ {
		err := cache.Set(ctx, schemas[i], 1*time.Hour)
		if err != nil {
			t.Errorf("unexpected error setting schema %d: %v", i, err)
		}
	}

	// Access first schema to increase its access count
	_, err := cache.Get(ctx, schemas[0].ID)
	if err != nil {
		t.Errorf("unexpected error getting first schema: %v", err)
	}

	// Add third schema, should trigger eviction
	err = cache.Set(ctx, schemas[2], 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error setting third schema: %v", err)
	}

	// First schema should still exist (higher access count)
	_, err = cache.Get(ctx, schemas[0].ID)
	if err != nil {
		t.Errorf("first schema should not have been evicted: %v", err)
	}

	// Second schema should have been evicted (lower access count)
	_, err = cache.Get(ctx, schemas[1].ID)
	if err == nil {
		t.Errorf("second schema should have been evicted")
	}

	// Third schema should exist
	_, err = cache.Get(ctx, schemas[2].ID)
	if err != nil {
		t.Errorf("third schema should exist: %v", err)
	}
}

func TestMemoryCache_GetStats(t *testing.T) {
	cache := NewMemoryCache(CacheConfig{MaxSize: 10})
	defer cache.Stop()

	ctx := context.Background()

	// Initially empty
	stats := cache.GetStats()
	if stats.Size != 0 {
		t.Errorf("expected size 0, got %d", stats.Size)
	}
	if stats.MaxSize != 10 {
		t.Errorf("expected max size 10, got %d", stats.MaxSize)
	}

	// Add a schema
	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "commerce",
			Entity:  "order",
			Version: "v1",
			Raw:     "agntcy:commerce.order.v1",
		},
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	err := cache.Set(ctx, schema, 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error setting schema: %v", err)
	}

	// Access the schema multiple times
	for i := 0; i < 5; i++ {
		_, err = cache.Get(ctx, schema.ID)
		if err != nil {
			t.Errorf("unexpected error getting schema: %v", err)
		}
	}

	stats = cache.GetStats()
	if stats.Size != 1 {
		t.Errorf("expected size 1, got %d", stats.Size)
	}
	if stats.TotalAccess != 5 {
		t.Errorf("expected total access 5, got %d", stats.TotalAccess)
	}
}

func TestDefaultCacheFactory_CreateCache(t *testing.T) {
	factory := &DefaultCacheFactory{}

	tests := []struct {
		name        string
		config      CacheConfig
		expectError bool
	}{
		{
			name: "memory cache",
			config: CacheConfig{
				Type: "memory",
			},
			expectError: false,
		},
		{
			name: "empty type defaults to memory",
			config: CacheConfig{
				Type: "",
			},
			expectError: false,
		},
		{
			name: "redis cache not implemented",
			config: CacheConfig{
				Type: "redis",
			},
			expectError: true,
		},
		{
			name: "unsupported cache type",
			config: CacheConfig{
				Type: "unsupported",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, err := factory.CreateCache(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if cache == nil {
				t.Errorf("expected cache to be created")
				return
			}

			// Clean up if it's a memory cache
			if memCache, ok := cache.(*MemoryCache); ok {
				memCache.Stop()
			}
		})
	}
}

func TestCacheBuilder(t *testing.T) {
	builder := NewCacheBuilder()

	if builder == nil {
		t.Errorf("expected builder to be created")
		return
	}

	config := CacheConfig{
		Type:       "memory",
		DefaultTTL: 30 * time.Minute,
		MaxSize:    100,
	}

	cache, err := builder.WithConfig(config).Build()
	if err != nil {
		t.Errorf("unexpected error building cache: %v", err)
		return
	}

	if cache == nil {
		t.Errorf("expected cache to be created")
		return
	}

	// Clean up
	if memCache, ok := cache.(*MemoryCache); ok {
		memCache.Stop()
	}
}

func TestCachedRegistryClient(t *testing.T) {
	// Create mock registry client
	mockRegistry := NewMockRegistryClient()

	// Create memory cache
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	// Create cached registry client
	cachedClient := NewCachedRegistryClient(mockRegistry, cache)

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	// Add schema to mock registry
	mockRegistry.AddSchema(schema)

	// First call should hit registry and cache result
	retrieved1, err := cachedClient.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting schema: %v", err)
	}

	if retrieved1 == nil {
		t.Errorf("expected schema to be retrieved")
		return
	}

	// Second call should hit cache
	retrieved2, err := cachedClient.GetSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error getting cached schema: %v", err)
	}

	if retrieved2 == nil {
		t.Errorf("expected cached schema to be retrieved")
		return
	}

	// Verify both results are the same
	if retrieved1.ID.String() != retrieved2.ID.String() {
		t.Errorf("cached result differs from original")
	}
}

func TestCachedRegistryClient_RegisterSchema(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	cachedClient := NewCachedRegistryClient(mockRegistry, cache)

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	metadata := &SchemaMetadata{
		ID:        schemaID,
		Version:   "v1",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		FilePath:  "commerce/order/v1.json",
		Size:      100,
		Checksum:  "abc123",
	}

	// Register schema
	err := cachedClient.RegisterSchema(ctx, schema, metadata)
	if err != nil {
		t.Errorf("unexpected error registering schema: %v", err)
	}

	// Verify schema is cached
	cached, err := cache.Get(ctx, schemaID)
	if err != nil {
		t.Errorf("expected schema to be cached after registration: %v", err)
	}

	if cached == nil {
		t.Errorf("expected cached schema after registration")
	}
}

func TestCachedRegistryClient_DeleteSchema(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	cachedClient := NewCachedRegistryClient(mockRegistry, cache)

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	// Add to mock registry and cache
	mockRegistry.AddSchema(schema)
	err := cache.Set(ctx, schema, 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error caching schema: %v", err)
	}

	// Delete schema
	err = cachedClient.DeleteSchema(ctx, schemaID)
	if err != nil {
		t.Errorf("unexpected error deleting schema: %v", err)
	}

	// Verify schema is removed from cache
	_, err = cache.Get(ctx, schemaID)
	if err == nil {
		t.Errorf("expected schema to be removed from cache after deletion")
	}
}

func TestCachedRegistryClient_ClearCache(t *testing.T) {
	mockRegistry := NewMockRegistryClient()
	cache := NewMemoryCache(CacheConfig{})
	defer cache.Stop()

	cachedClient := NewCachedRegistryClient(mockRegistry, cache)

	ctx := context.Background()
	schemaID := SchemaIdentifier{
		Domain:  "commerce",
		Entity:  "order",
		Version: "v1",
		Raw:     "agntcy:commerce.order.v1",
	}

	schema := &Schema{
		ID:          schemaID,
		Definition:  json.RawMessage(`{"type": "object"}`),
		PublishedAt: time.Now(),
	}

	// Cache schema
	err := cache.Set(ctx, schema, 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error caching schema: %v", err)
	}

	// Clear cache
	err = cachedClient.ClearCache(ctx)
	if err != nil {
		t.Errorf("unexpected error clearing cache: %v", err)
	}

	// Verify cache is empty
	_, err = cache.Get(ctx, schemaID)
	if err == nil {
		t.Errorf("expected cache to be empty after clearing")
	}
}
