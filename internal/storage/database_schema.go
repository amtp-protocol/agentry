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

package storage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/amtp-protocol/agentry/internal/schema"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// StoreSchema stores a schema in the database
func (s *DatabaseStorage) StoreSchema(ctx context.Context, sc *schema.Schema, meta *schema.SchemaMetadata) error {
	model := Schema{
		Domain:      sc.ID.Domain,
		Entity:      sc.ID.Entity,
		Version:     sc.ID.Version,
		Definition:  datatypes.JSON(sc.Definition),
		PublishedAt: sc.PublishedAt,
		Signature:   sc.Signature,
	}

	if meta != nil {
		model.Checksum = meta.Checksum
		model.Size = meta.Size
		// Intentionally ignoring FilePath as database is source of truth
		// Using database timestamps instead of meta timestamps to reflect storage time
	}

	result := s.db.WithContext(ctx).Create(&model)
	return result.Error
}

// GetSchema retrieves a schema from the database
func (s *DatabaseStorage) GetSchema(ctx context.Context, domain, entity, version string) (*schema.Schema, error) {
	var model Schema
	result := s.db.WithContext(ctx).Where("domain = ? AND entity = ? AND version = ?", domain, entity, version).First(&model)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, schema.ErrSchemaNotFound
		}
		return nil, result.Error
	}

	return toSchemaDomain(&model), nil
}

// ListSchemas lists schemas for a domain
func (s *DatabaseStorage) ListSchemas(ctx context.Context, domain string) ([]*schema.Schema, error) {
	var models []Schema
	var result *gorm.DB
	if domain == "" {
		// empty domain => return all schemas
		result = s.db.WithContext(ctx).Find(&models)
	} else {
		result = s.db.WithContext(ctx).Where("domain = ?", domain).Find(&models)
	}
	if result.Error != nil {
		return nil, result.Error
	}

	schemas := make([]*schema.Schema, len(models))
	for i, m := range models {
		schemas[i] = toSchemaDomain(&m)
	}
	return schemas, nil
}

// DeleteSchema deletes a schema from the database
func (s *DatabaseStorage) DeleteSchema(ctx context.Context, domain, entity, version string) error {
	result := s.db.WithContext(ctx).Where("domain = ? AND entity = ? AND version = ?", domain, entity, version).Delete(&Schema{})
	return result.Error
}

// GetRegistryStats retrieves registry statistics using optimized database queries
func (s *DatabaseStorage) GetRegistryStats(ctx context.Context) (*schema.RegistryStats, error) {
	var total int64
	if err := s.db.WithContext(ctx).Model(&Schema{}).Count(&total).Error; err != nil {
		return nil, err
	}

	var domainCounts []struct {
		Domain string
		Count  int
	}
	if err := s.db.WithContext(ctx).Model(&Schema{}).Select("domain, count(*) as count").Group("domain").Scan(&domainCounts).Error; err != nil {
		return nil, err
	}

	var entityCounts []struct {
		Entity string
		Count  int
	}
	if err := s.db.WithContext(ctx).Model(&Schema{}).Select("entity, count(*) as count").Group("entity").Scan(&entityCounts).Error; err != nil {
		return nil, err
	}

	stats := &schema.RegistryStats{
		TotalSchemas: int(total),
		Domains:      make(map[string]int),
		Entities:     make(map[string]int),
	}

	for _, d := range domainCounts {
		stats.Domains[d.Domain] = d.Count
	}
	for _, e := range entityCounts {
		stats.Entities[e.Entity] = e.Count
	}

	return stats, nil
}

func toSchemaDomain(m *Schema) *schema.Schema {
	return &schema.Schema{
		ID: schema.SchemaIdentifier{
			Domain:  m.Domain,
			Entity:  m.Entity,
			Version: m.Version,
		},
		Definition:  json.RawMessage(m.Definition),
		PublishedAt: m.PublishedAt,
		Signature:   m.Signature,
	}
}
