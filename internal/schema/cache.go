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
	"fmt"
	"sync"
	"time"
)

// CacheConfig holds configuration for schema caching
type CacheConfig struct {
	Type             string                 `yaml:"type" json:"type"` // "memory", "redis", "custom"
	DefaultTTL       time.Duration          `yaml:"default_ttl" json:"default_ttl"`
	MaxSize          int                    `yaml:"max_size" json:"max_size"`
	CleanupInterval  time.Duration          `yaml:"cleanup_interval" json:"cleanup_interval"`
	ConnectionString string                 `yaml:"connection_string" json:"connection_string"`
	Options          map[string]interface{} `yaml:"options" json:"options"`
}

// CacheEntry represents a cached schema with metadata
type CacheEntry struct {
	Schema      *Schema
	ExpiresAt   time.Time
	CreatedAt   time.Time
	AccessCount int64
}

// IsExpired checks if the cache entry has expired
func (ce *CacheEntry) IsExpired() bool {
	return time.Now().After(ce.ExpiresAt)
}

// MemoryCache implements Cache interface using in-memory storage
type MemoryCache struct {
	schemas         map[string]*CacheEntry
	mu              sync.RWMutex
	defaultTTL      time.Duration
	maxSize         int
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(config CacheConfig) *MemoryCache {
	defaultTTL := config.DefaultTTL
	if defaultTTL == 0 {
		defaultTTL = 1 * time.Hour
	}

	maxSize := config.MaxSize
	if maxSize == 0 {
		maxSize = 1000
	}

	cleanupInterval := config.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 5 * time.Minute
	}

	cache := &MemoryCache{
		schemas:         make(map[string]*CacheEntry),
		defaultTTL:      defaultTTL,
		maxSize:         maxSize,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine
	go cache.cleanupLoop()

	return cache
}

// Get retrieves a schema from cache
func (c *MemoryCache) Get(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.schemas[id.String()]
	if !exists {
		return nil, fmt.Errorf("schema not found in cache: %s", id.String())
	}

	if entry.IsExpired() {
		// Remove expired entry
		delete(c.schemas, id.String())
		return nil, fmt.Errorf("schema expired in cache: %s", id.String())
	}

	// Update access count
	entry.AccessCount++

	// Return a copy to prevent modification
	schemaCopy := *entry.Schema
	return &schemaCopy, nil
}

// Set stores a schema in cache with TTL
func (c *MemoryCache) Set(ctx context.Context, schema *Schema, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ttl == 0 {
		ttl = c.defaultTTL
	}

	// Check if we need to evict entries to make room
	if len(c.schemas) >= c.maxSize {
		c.evictLRU()
	}

	entry := &CacheEntry{
		Schema:      schema,
		ExpiresAt:   time.Now().Add(ttl),
		CreatedAt:   time.Now(),
		AccessCount: 0,
	}

	c.schemas[schema.ID.String()] = entry
	return nil
}

// Delete removes a schema from cache
func (c *MemoryCache) Delete(ctx context.Context, id SchemaIdentifier) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.schemas, id.String())
	return nil
}

// Clear clears all cached schemas
func (c *MemoryCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.schemas = make(map[string]*CacheEntry)
	return nil
}

// evictLRU evicts the least recently used entry
func (c *MemoryCache) evictLRU() {
	var oldestKey string
	var oldestTime time.Time
	var lowestAccess int64 = -1

	for key, entry := range c.schemas {
		if lowestAccess == -1 || entry.AccessCount < lowestAccess ||
			(entry.AccessCount == lowestAccess && entry.CreatedAt.Before(oldestTime)) {
			oldestKey = key
			oldestTime = entry.CreatedAt
			lowestAccess = entry.AccessCount
		}
	}

	if oldestKey != "" {
		delete(c.schemas, oldestKey)
	}
}

// cleanupLoop periodically removes expired entries
func (c *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCleanup:
			return
		}
	}
}

// cleanup removes expired entries
func (c *MemoryCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.schemas {
		if now.After(entry.ExpiresAt) {
			delete(c.schemas, key)
		}
	}
}

// Stop stops the cleanup goroutine
func (c *MemoryCache) Stop() {
	close(c.stopCleanup)
}

// GetStats returns cache statistics
func (c *MemoryCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalAccess int64
	expired := 0
	now := time.Now()

	for _, entry := range c.schemas {
		totalAccess += entry.AccessCount
		if now.After(entry.ExpiresAt) {
			expired++
		}
	}

	return CacheStats{
		Size:         len(c.schemas),
		MaxSize:      c.maxSize,
		TotalAccess:  totalAccess,
		ExpiredCount: expired,
	}
}

// CacheStats represents cache statistics
type CacheStats struct {
	Size         int   `json:"size"`
	MaxSize      int   `json:"max_size"`
	TotalAccess  int64 `json:"total_access"`
	ExpiredCount int   `json:"expired_count"`
}

// CachedRegistryClient wraps a RegistryClient with caching
type CachedRegistryClient struct {
	client RegistryClient
	cache  Cache
}

// NewCachedRegistryClient creates a new cached registry client
func NewCachedRegistryClient(client RegistryClient, cache Cache) *CachedRegistryClient {
	return &CachedRegistryClient{
		client: client,
		cache:  cache,
	}
}

// GetSchema retrieves a schema, checking cache first
func (c *CachedRegistryClient) GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	// Try cache first
	if schema, err := c.cache.Get(ctx, id); err == nil {
		return schema, nil
	}

	// Cache miss, get from registry
	schema, err := c.client.GetSchema(ctx, id)
	if err != nil {
		return nil, err
	}

	// Cache the result
	c.cache.Set(ctx, schema, 0) // Use default TTL

	return schema, nil
}

// ListSchemas lists schemas from the registry (not cached)
func (c *CachedRegistryClient) ListSchemas(ctx context.Context, pattern string) ([]SchemaIdentifier, error) {
	return c.client.ListSchemas(ctx, pattern)
}

// ValidateSchema validates a schema using the registry
func (c *CachedRegistryClient) ValidateSchema(ctx context.Context, schema *Schema) error {
	return c.client.ValidateSchema(ctx, schema)
}

// CheckCompatibility checks schema compatibility using the registry
func (c *CachedRegistryClient) CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error) {
	return c.client.CheckCompatibility(ctx, current, new)
}

// RegisterSchema registers a new schema
func (c *CachedRegistryClient) RegisterSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	err := c.client.RegisterSchema(ctx, schema, metadata)
	if err == nil {
		// Cache the newly registered schema with default TTL
		c.cache.Set(ctx, schema, 0) // 0 means use default TTL
	}
	return err
}

// RegisterOrUpdateSchema registers or updates a schema
func (c *CachedRegistryClient) RegisterOrUpdateSchema(ctx context.Context, schema *Schema, metadata *SchemaMetadata) error {
	err := c.client.RegisterOrUpdateSchema(ctx, schema, metadata)
	if err == nil {
		// Update cache with the new/updated schema with default TTL
		c.cache.Set(ctx, schema, 0) // 0 means use default TTL
	}
	return err
}

// DeleteSchema deletes a schema
func (c *CachedRegistryClient) DeleteSchema(ctx context.Context, id SchemaIdentifier) error {
	err := c.client.DeleteSchema(ctx, id)
	if err == nil {
		// Remove from cache
		c.cache.Delete(ctx, id)
	}
	return err
}

// GetStats returns registry statistics
func (c *CachedRegistryClient) GetStats() RegistryStats {
	return c.client.GetStats()
}

// InvalidateCache removes a schema from cache
func (c *CachedRegistryClient) InvalidateCache(ctx context.Context, id SchemaIdentifier) error {
	return c.cache.Delete(ctx, id)
}

// ClearCache clears all cached schemas
func (c *CachedRegistryClient) ClearCache(ctx context.Context) error {
	return c.cache.Clear(ctx)
}

// CacheFactory creates cache instances based on configuration
type CacheFactory interface {
	CreateCache(config CacheConfig) (Cache, error)
}

// DefaultCacheFactory provides default cache implementations
type DefaultCacheFactory struct{}

// CreateCache creates a cache instance based on the configuration type
func (f *DefaultCacheFactory) CreateCache(config CacheConfig) (Cache, error) {
	switch config.Type {
	case "memory", "":
		return NewMemoryCache(config), nil
	case "redis":
		return nil, fmt.Errorf("redis cache not implemented yet - use memory cache for now")
	default:
		return nil, fmt.Errorf("unsupported cache type: %s", config.Type)
	}
}

// CustomCacheAdapter allows using custom cache implementations
type CustomCacheAdapter struct {
	cache interface{}
}

// NewCustomCacheAdapter creates an adapter for custom cache implementations
func NewCustomCacheAdapter(cache interface{}) *CustomCacheAdapter {
	return &CustomCacheAdapter{cache: cache}
}

// Get retrieves a schema from the custom cache
func (c *CustomCacheAdapter) Get(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	// This would need to be implemented based on the specific cache interface
	return nil, fmt.Errorf("custom cache adapter not implemented")
}

// Set stores a schema in the custom cache
func (c *CustomCacheAdapter) Set(ctx context.Context, schema *Schema, ttl time.Duration) error {
	// This would need to be implemented based on the specific cache interface
	return fmt.Errorf("custom cache adapter not implemented")
}

// Delete removes a schema from the custom cache
func (c *CustomCacheAdapter) Delete(ctx context.Context, id SchemaIdentifier) error {
	// This would need to be implemented based on the specific cache interface
	return fmt.Errorf("custom cache adapter not implemented")
}

// Clear clears all cached schemas from the custom cache
func (c *CustomCacheAdapter) Clear(ctx context.Context) error {
	// This would need to be implemented based on the specific cache interface
	return fmt.Errorf("custom cache adapter not implemented")
}

// CacheBuilder provides a fluent interface for building caches
type CacheBuilder struct {
	config  CacheConfig
	factory CacheFactory
}

// NewCacheBuilder creates a new cache builder
func NewCacheBuilder() *CacheBuilder {
	return &CacheBuilder{
		factory: &DefaultCacheFactory{},
	}
}

// WithConfig sets the cache configuration
func (b *CacheBuilder) WithConfig(config CacheConfig) *CacheBuilder {
	b.config = config
	return b
}

// WithFactory sets a custom cache factory
func (b *CacheBuilder) WithFactory(factory CacheFactory) *CacheBuilder {
	b.factory = factory
	return b
}

// Build creates the cache instance
func (b *CacheBuilder) Build() (Cache, error) {
	return b.factory.CreateCache(b.config)
}
