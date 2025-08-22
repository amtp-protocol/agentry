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
	"strconv"
	"strings"
)

// NegotiationConfig holds configuration for schema negotiation
type NegotiationConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	FallbackStrategy string `yaml:"fallback_strategy" json:"fallback_strategy"` // "latest", "previous", "fail"
	MaxVersionDrift  int    `yaml:"max_version_drift" json:"max_version_drift"`
}

// NegotiationEngine handles schema version negotiation
type NegotiationEngine struct {
	registryClient RegistryClient
	config         NegotiationConfig
}

// NewNegotiationEngine creates a new schema negotiation engine
func NewNegotiationEngine(registryClient RegistryClient, config NegotiationConfig) *NegotiationEngine {
	if config.FallbackStrategy == "" {
		config.FallbackStrategy = "latest"
	}
	if config.MaxVersionDrift == 0 {
		config.MaxVersionDrift = 3
	}

	return &NegotiationEngine{
		registryClient: registryClient,
		config:         config,
	}
}

// NegotiateSchema negotiates the best schema version to use
func (ne *NegotiationEngine) NegotiateSchema(ctx context.Context, requestedSchema SchemaIdentifier) (*SchemaIdentifier, error) {
	if !ne.config.Enabled {
		// No negotiation, return requested schema as-is
		return &requestedSchema, nil
	}

	// First, try to get the exact requested schema
	if _, err := ne.registryClient.GetSchema(ctx, requestedSchema); err == nil {
		return &requestedSchema, nil
	}

	// Schema not found, try negotiation
	return ne.findBestAlternative(ctx, requestedSchema)
}

// findBestAlternative finds the best alternative schema version
func (ne *NegotiationEngine) findBestAlternative(ctx context.Context, requested SchemaIdentifier) (*SchemaIdentifier, error) {
	// List all schemas for the same domain and entity
	pattern := fmt.Sprintf("%s.%s", requested.Domain, requested.Entity)
	schemas, err := ne.registryClient.ListSchemas(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas for negotiation: %w", err)
	}

	// Filter schemas that match domain and entity
	var candidates []SchemaIdentifier
	for _, schema := range schemas {
		if schema.Domain == requested.Domain && schema.Entity == requested.Entity {
			candidates = append(candidates, schema)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no schemas found for %s.%s", requested.Domain, requested.Entity)
	}

	// Apply fallback strategy
	switch ne.config.FallbackStrategy {
	case "latest":
		return ne.findLatestVersion(candidates), nil
	case "previous":
		return ne.findPreviousVersion(requested, candidates), nil
	case "fail":
		return nil, fmt.Errorf("schema negotiation failed: exact version %s not found", requested.String())
	default:
		return ne.findLatestVersion(candidates), nil
	}
}

// findLatestVersion finds the latest version among candidates
func (ne *NegotiationEngine) findLatestVersion(candidates []SchemaIdentifier) *SchemaIdentifier {
	if len(candidates) == 0 {
		return nil
	}

	latest := candidates[0]
	latestVersion := ne.parseVersion(latest.Version)

	for _, candidate := range candidates[1:] {
		candidateVersion := ne.parseVersion(candidate.Version)
		if candidateVersion > latestVersion {
			latest = candidate
			latestVersion = candidateVersion
		}
	}

	return &latest
}

// findPreviousVersion finds the highest version that's lower than requested
func (ne *NegotiationEngine) findPreviousVersion(requested SchemaIdentifier, candidates []SchemaIdentifier) *SchemaIdentifier {
	requestedVersion := ne.parseVersion(requested.Version)

	var best *SchemaIdentifier
	bestVersion := -1

	for _, candidate := range candidates {
		candidateVersion := ne.parseVersion(candidate.Version)
		if candidateVersion < requestedVersion && candidateVersion > bestVersion {
			best = &candidate
			bestVersion = candidateVersion
		}
	}

	if best == nil {
		// No previous version found, fall back to latest
		return ne.findLatestVersion(candidates)
	}

	return best
}

// parseVersion extracts numeric version from version string (e.g., "v1" -> 1)
func (ne *NegotiationEngine) parseVersion(version string) int {
	// Remove 'v' prefix if present
	version = strings.TrimPrefix(version, "v")

	// Try to parse as integer
	if num, err := strconv.Atoi(version); err == nil {
		return num
	}

	// If parsing fails, return 0 as fallback
	return 0
}

// CheckVersionDrift checks if the version drift is within acceptable limits
func (ne *NegotiationEngine) CheckVersionDrift(requested, negotiated SchemaIdentifier) bool {
	if !ne.config.Enabled {
		return true
	}

	requestedVersion := ne.parseVersion(requested.Version)
	negotiatedVersion := ne.parseVersion(negotiated.Version)

	drift := abs(requestedVersion - negotiatedVersion)
	return drift <= ne.config.MaxVersionDrift
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// GetNegotiationInfo returns information about negotiation capabilities
func (ne *NegotiationEngine) GetNegotiationInfo(ctx context.Context, domain, entity string) (*NegotiationInfo, error) {
	pattern := fmt.Sprintf("%s.%s", domain, entity)
	schemas, err := ne.registryClient.ListSchemas(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get negotiation info: %w", err)
	}

	// Filter and collect version info
	var versions []string
	for _, schema := range schemas {
		if schema.Domain == domain && schema.Entity == entity {
			versions = append(versions, schema.Version)
		}
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no schemas found for %s.%s", domain, entity)
	}

	// Find latest version
	latest := versions[0]
	latestNum := ne.parseVersion(latest)
	for _, version := range versions[1:] {
		versionNum := ne.parseVersion(version)
		if versionNum > latestNum {
			latest = version
			latestNum = versionNum
		}
	}

	return &NegotiationInfo{
		Domain:             domain,
		Entity:             entity,
		AvailableVersions:  versions,
		LatestVersion:      latest,
		NegotiationEnabled: ne.config.Enabled,
		FallbackStrategy:   ne.config.FallbackStrategy,
		MaxVersionDrift:    ne.config.MaxVersionDrift,
	}, nil
}

// NegotiationInfo provides information about schema negotiation capabilities
type NegotiationInfo struct {
	Domain             string   `json:"domain"`
	Entity             string   `json:"entity"`
	AvailableVersions  []string `json:"available_versions"`
	LatestVersion      string   `json:"latest_version"`
	NegotiationEnabled bool     `json:"negotiation_enabled"`
	FallbackStrategy   string   `json:"fallback_strategy"`
	MaxVersionDrift    int      `json:"max_version_drift"`
}
