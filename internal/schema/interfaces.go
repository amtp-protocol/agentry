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
	"errors"
)

var (
	// ErrSchemaNotFound is returned when a requested schema is not found
	ErrSchemaNotFound = errors.New("schema not found")
)

// SchemaStore defines the interface for schema storage operations
type SchemaStore interface {
	// StoreSchema stores a schema in the registry
	StoreSchema(ctx context.Context, schema *Schema, meta *SchemaMetadata) error

	// GetSchema retrieves a schema from the registry
	GetSchema(ctx context.Context, domain, entity, version string) (*Schema, error)

	// ListSchemas lists schemas for a domain
	ListSchemas(ctx context.Context, domain string) ([]*Schema, error)

	// DeleteSchema deletes a schema from the registry
	DeleteSchema(ctx context.Context, domain, entity, version string) error

	// GetRegistryStats retrieves registry statistics
	GetRegistryStats(ctx context.Context) (*RegistryStats, error)
}
