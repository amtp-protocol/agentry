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
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// MockSchemaStore implements SchemaStore for testing
type MockSchemaStore struct {
	schemas map[string]*Schema
	err     error // Force an error if set
}

func NewMockSchemaStore() *MockSchemaStore {
	return &MockSchemaStore{
		schemas: make(map[string]*Schema),
	}
}

func (m *MockSchemaStore) StoreSchema(ctx context.Context, schema *Schema, meta *SchemaMetadata) error {
	if m.err != nil {
		return m.err
	}
	// Use string representation as key
	m.schemas[schema.ID.String()] = schema
	return nil
}

func (m *MockSchemaStore) GetSchema(ctx context.Context, domain, entity, version string) (*Schema, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Simple lookup based on matching fields since we don't have the full ID string
	for _, s := range m.schemas {
		if s.ID.Domain == domain && s.ID.Entity == entity && s.ID.Version == version {
			return s, nil
		}
	}
	return nil, ErrSchemaNotFound
}

func (m *MockSchemaStore) ListSchemas(ctx context.Context, domain string) ([]*Schema, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []*Schema
	for _, s := range m.schemas {
		// Treat empty domain as a wildcard to return all schemas
		if domain == "" || s.ID.Domain == domain {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *MockSchemaStore) DeleteSchema(ctx context.Context, domain, entity, version string) error {
	if m.err != nil {
		return m.err
	}
	for key, s := range m.schemas {
		if s.ID.Domain == domain && s.ID.Entity == entity && s.ID.Version == version {
			delete(m.schemas, key)
			return nil
		}
	}
	return ErrSchemaNotFound
}

func (m *MockSchemaStore) GetRegistryStats(ctx context.Context) (*RegistryStats, error) {
	if m.err != nil {
		return nil, m.err
	}

	domains := make(map[string]int)
	entities := make(map[string]int)

	for _, s := range m.schemas {
		domains[s.ID.Domain]++
		entities[s.ID.Entity]++
	}

	return &RegistryStats{
		TotalSchemas: len(m.schemas),
		Domains:      domains,
		Entities:     entities,
	}, nil
}

func TestDatabaseRegistry_RegisterAndGet(t *testing.T) {
	mockStore := NewMockSchemaStore()
	registry := NewDatabaseRegistry(mockStore)
	ctx := context.Background()

	schema := &Schema{
		ID: SchemaIdentifier{
			Domain:  "test",
			Entity:  "user",
			Version: "v1",
			Raw:     "agntcy:test.user.v1",
		},
		Definition:  json.RawMessage(`{"type":"object"}`),
		PublishedAt: time.Now(),
	}

	// Test RegisterSchema
	err := registry.RegisterSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("RegisterSchema failed: %v", err)
	}

	// Test GetSchema
	retrieved, err := registry.GetSchema(ctx, schema.ID)
	if err != nil {
		t.Errorf("GetSchema failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("retrieved schema is nil")
	}
	if retrieved.ID.String() != schema.ID.String() {
		t.Errorf("expected ID %s, got %s", schema.ID.String(), retrieved.ID.String())
	}

	// Test RegisterOrUpdateSchema
	schema.Definition = json.RawMessage(`{"type":"string"}`)
	err = registry.RegisterOrUpdateSchema(ctx, schema, nil)
	if err != nil {
		t.Errorf("RegisterOrUpdateSchema failed: %v", err)
	}

	updated, err := registry.GetSchema(ctx, schema.ID)
	if err != nil {
		t.Errorf("GetSchema failed after update: %v", err)
	}
	if string(updated.Definition) != string(schema.Definition) {
		t.Errorf("schema definition mismatch after update")
	}
}

func TestDatabaseRegistry_ListSchemas(t *testing.T) {
	mockStore := NewMockSchemaStore()
	registry := NewDatabaseRegistry(mockStore)
	ctx := context.Background()

	s1 := &Schema{ID: SchemaIdentifier{Domain: "d1", Entity: "e1", Version: "v1", Raw: "agntcy:d1.e1.v1"}}
	s2 := &Schema{ID: SchemaIdentifier{Domain: "d1", Entity: "e2", Version: "v1", Raw: "agntcy:d1.e2.v1"}}
	s3 := &Schema{ID: SchemaIdentifier{Domain: "d2", Entity: "e1", Version: "v1", Raw: "agntcy:d2.e1.v1"}}

	registry.RegisterSchema(ctx, s1, nil)
	registry.RegisterSchema(ctx, s2, nil)
	registry.RegisterSchema(ctx, s3, nil)

	list, err := registry.ListSchemas(ctx, "d1")
	if err != nil {
		t.Fatalf("ListSchemas failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 schemas for domain d1, got %d", len(list))
	}
}

func TestDatabaseRegistry_DeleteSchema(t *testing.T) {
	mockStore := NewMockSchemaStore()
	registry := NewDatabaseRegistry(mockStore)
	ctx := context.Background()

	id := SchemaIdentifier{Domain: "d1", Entity: "e1", Version: "v1", Raw: "agntcy:d1.e1.v1"}
	s1 := &Schema{ID: id}
	registry.RegisterSchema(ctx, s1, nil)

	if err := registry.DeleteSchema(ctx, id); err != nil {
		t.Errorf("DeleteSchema failed: %v", err)
	}

	_, err := registry.GetSchema(ctx, id)
	if err != ErrSchemaNotFound {
		t.Errorf("expected ErrSchemaNotFound after delete, got %v", err)
	}
}

func TestDatabaseRegistry_StoreErrors(t *testing.T) {
	mockStore := NewMockSchemaStore()
	mockErr := errors.New("db error")
	mockStore.err = mockErr

	registry := NewDatabaseRegistry(mockStore)
	ctx := context.Background()

	s1 := &Schema{ID: SchemaIdentifier{Domain: "d1", Entity: "e1", Version: "v1", Raw: "agntcy:d1.e1.v1"}}

	if err := registry.RegisterSchema(ctx, s1, nil); err != mockErr {
		t.Errorf("expected error %v, got %v", mockErr, err)
	}

	if _, err := registry.GetSchema(ctx, s1.ID); err != mockErr {
		t.Errorf("expected error %v, got %v", mockErr, err)
	}

	if _, err := registry.ListSchemas(ctx, "d1"); err != mockErr {
		t.Errorf("expected error %v, got %v", mockErr, err)
	}

	if err := registry.DeleteSchema(ctx, s1.ID); err != mockErr {
		t.Errorf("expected error %v, got %v", mockErr, err)
	}
}

func TestDatabaseRegistry_OtherMethods(t *testing.T) {
	registry := NewDatabaseRegistry(NewMockSchemaStore())
	ctx := context.Background()

	// CheckCompatibility
	compat, err := registry.CheckCompatibility(ctx, SchemaIdentifier{}, SchemaIdentifier{})
	if err != nil {
		t.Errorf("CheckCompatibility returned error: %v", err)
	}
	if !compat {
		t.Errorf("CheckCompatibility expected true by default")
	}

	// ValidateSchema (nil)
	if err := registry.ValidateSchema(ctx, nil); err == nil {
		t.Error("ValidateSchema(nil) expected error")
	}

	// ValidateSchema (non-nil)
	if err := registry.ValidateSchema(ctx, &Schema{}); err != nil {
		t.Errorf("ValidateSchema(struct) unexpected error: %v", err)
	}

	// GetStats
	stats := registry.GetStats()
	if stats.TotalSchemas != 0 {
		t.Errorf("GetStats expected 0 TotalSchemas, got %d", stats.TotalSchemas)
	}
}

func TestDatabaseRegistry_GetStatsCounts(t *testing.T) {
	mockStore := NewMockSchemaStore()
	registry := NewDatabaseRegistry(mockStore)
	ctx := context.Background()

	s1 := &Schema{ID: SchemaIdentifier{Domain: "d1", Entity: "e1", Version: "v1", Raw: "agntcy:d1.e1.v1"}}
	s2 := &Schema{ID: SchemaIdentifier{Domain: "d1", Entity: "e2", Version: "v1", Raw: "agntcy:d1.e2.v1"}}
	s3 := &Schema{ID: SchemaIdentifier{Domain: "d2", Entity: "e1", Version: "v1", Raw: "agntcy:d2.e1.v1"}}

	if err := registry.RegisterSchema(ctx, s1, nil); err != nil {
		t.Fatalf("failed to register s1: %v", err)
	}
	if err := registry.RegisterSchema(ctx, s2, nil); err != nil {
		t.Fatalf("failed to register s2: %v", err)
	}
	if err := registry.RegisterSchema(ctx, s3, nil); err != nil {
		t.Fatalf("failed to register s3: %v", err)
	}

	stats := registry.GetStats()
	if stats.TotalSchemas != 3 {
		t.Errorf("expected TotalSchemas 3, got %d", stats.TotalSchemas)
	}

	if stats.Domains["d1"] != 2 {
		t.Errorf("expected domain d1 count 2, got %d", stats.Domains["d1"])
	}
	if stats.Domains["d2"] != 1 {
		t.Errorf("expected domain d2 count 1, got %d", stats.Domains["d2"])
	}

	if stats.Entities["e1"] != 2 {
		t.Errorf("expected entity e1 count 2, got %d", stats.Entities["e1"])
	}
	if stats.Entities["e2"] != 1 {
		t.Errorf("expected entity e2 count 1, got %d", stats.Entities["e2"])
	}
}
