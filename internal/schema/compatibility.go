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
	"time"
)

// CompatibilityConfig holds configuration for compatibility checking
type CompatibilityConfig struct {
	Enabled    bool `yaml:"enabled" json:"enabled"`
	StrictMode bool `yaml:"strict_mode" json:"strict_mode"`
}

// CompatibilityChecker handles schema compatibility analysis
type CompatibilityChecker struct {
	registryClient RegistryClient
	config         CompatibilityConfig
}

// NewCompatibilityChecker creates a new compatibility checker
func NewCompatibilityChecker(registryClient RegistryClient, config CompatibilityConfig) *CompatibilityChecker {
	return &CompatibilityChecker{
		registryClient: registryClient,
		config:         config,
	}
}

// CheckCompatibility checks if two schemas are compatible
func (cc *CompatibilityChecker) CheckCompatibility(ctx context.Context, current, new SchemaIdentifier) (bool, error) {
	if !cc.config.Enabled {
		return true, nil // Compatibility checking disabled
	}

	return cc.registryClient.CheckCompatibility(ctx, current, new)
}

// AnalyzeCompatibility provides detailed compatibility analysis
func (cc *CompatibilityChecker) AnalyzeCompatibility(ctx context.Context, current, new SchemaIdentifier) (*CompatibilityAnalysis, error) {
	analysis := &CompatibilityAnalysis{
		CurrentSchema: current,
		NewSchema:     new,
		Timestamp:     time.Now(),
	}

	// Basic compatibility check
	compatible, err := cc.CheckCompatibility(ctx, current, new)
	if err != nil {
		analysis.Error = err.Error()
		return analysis, err
	}

	analysis.Compatible = compatible

	if !compatible {
		analysis.Issues = append(analysis.Issues, "Schemas are not compatible")
	}

	return analysis, nil
}

// CompatibilityAnalysis represents detailed compatibility analysis
type CompatibilityAnalysis struct {
	CurrentSchema SchemaIdentifier `json:"current_schema"`
	NewSchema     SchemaIdentifier `json:"new_schema"`
	Compatible    bool             `json:"compatible"`
	Issues        []string         `json:"issues,omitempty"`
	Error         string           `json:"error,omitempty"`
	Timestamp     time.Time        `json:"timestamp"`
}
