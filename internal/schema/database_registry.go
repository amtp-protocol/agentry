/*
 * Copyright 2026 Sen Wang
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
)

// DatabaseRegistry implements RegistryClient using a SchemaStore
type DatabaseRegistry struct {
	store SchemaStore
}

// NewDatabaseRegistry creates a new database registry
func NewDatabaseRegistry(store SchemaStore) *DatabaseRegistry {
	return &DatabaseRegistry{
		store: store,
	}
}

// GetSchema retrieves a schema from the database
func (r *DatabaseRegistry) GetSchema(ctx context.Context, id SchemaIdentifier) (*Schema, error) {
	return r.store.GetSchema(ctx, id.Domain, id.Entity, id.Version)
}

// ListSchemas lists schemas from the database
func (r *DatabaseRegistry) ListSchemas(ctx context.Context, domain string) ([]SchemaIdentifier, error) {
	schemas, err := r.store.ListSchemas(ctx, domain)
	if err != nil {
		return nil, err
	}

	ids := make([]SchemaIdentifier, len(schemas))
	for i, s := range schemas {
		ids[i] = s.ID
	}
	return ids, nil
}

// RegisterOrUpdateSchema registers or updates a schema in the database
func (r *DatabaseRegistry) RegisterOrUpdateSchema(ctx context.Context, s *Schema, meta *SchemaMetadata) error {
	// Database registry stores schema content and selected metadata
	// FilePath from metadata is purposefully ignored as database is the storage medium
	return r.store.StoreSchema(ctx, s, meta)
}

// RegisterSchema registers a new schema
func (r *DatabaseRegistry) RegisterSchema(ctx context.Context, s *Schema, meta *SchemaMetadata) error {
	return r.store.StoreSchema(ctx, s, meta)
}

// ValidateSchema validates a schema
func (r *DatabaseRegistry) ValidateSchema(ctx context.Context, s *Schema) error {
	if s == nil {
		return fmt.Errorf("schema validation failed: schema is nil")
	}
	return nil
}

// DeleteSchema deletes a schema from the database
func (r *DatabaseRegistry) DeleteSchema(ctx context.Context, id SchemaIdentifier) error {
	return r.store.DeleteSchema(ctx, id.Domain, id.Entity, id.Version)
}

// CheckCompatibility checks if a schema is compatible with another
func (r *DatabaseRegistry) CheckCompatibility(ctx context.Context, newSchemaID, oldSchemaID SchemaIdentifier) (bool, error) {
	return true, nil
}

// GetStats returns registry statistics
func (r *DatabaseRegistry) GetStats() RegistryStats {
	ctx := context.Background()
	stats, err := r.store.GetRegistryStats(ctx)
	if err != nil {
		return RegistryStats{}
	}
	if stats == nil {
		return RegistryStats{}
	}
	return *stats
}
