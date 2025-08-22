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
	"strings"

	"github.com/amtp-protocol/agentry/internal/types"
)

// BypassConfig holds configuration for validation bypass
type BypassConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	TrustedDomains []string `yaml:"trusted_domains" json:"trusted_domains"`
}

// BypassManager handles validation bypass logic
type BypassManager struct {
	config BypassConfig
}

// NewBypassManager creates a new bypass manager
func NewBypassManager(config BypassConfig) *BypassManager {
	return &BypassManager{
		config: config,
	}
}

// ShouldBypass determines if validation should be bypassed for a message
func (bm *BypassManager) ShouldBypass(ctx context.Context, message *types.Message) bool {
	if !bm.config.Enabled {
		return false
	}

	// Handle nil message gracefully
	if message == nil {
		return false
	}

	// Bypass if no schema is specified
	if message.Schema == "" {
		return true
	}

	// Check if sender domain is trusted
	if bm.isTrustedSender(message.Sender) {
		return true
	}

	return false
}

// isTrustedSender checks if a sender email is from a trusted domain
func (bm *BypassManager) isTrustedSender(sender string) bool {
	// Extract domain from email
	parts := strings.Split(sender, "@")
	if len(parts) != 2 {
		return false
	}
	domain := parts[1]

	return bm.isTrustedDomain(domain)
}

// isTrustedDomain checks if a domain is in the trusted domains list
func (bm *BypassManager) isTrustedDomain(domain string) bool {
	for _, trustedDomain := range bm.config.TrustedDomains {
		if strings.EqualFold(domain, trustedDomain) {
			return true
		}
	}
	return false
}

// GetBypassInfo returns information about bypass configuration
func (bm *BypassManager) GetBypassInfo() *BypassInfo {
	return &BypassInfo{
		Enabled:        bm.config.Enabled,
		TrustedDomains: bm.config.TrustedDomains,
	}
}

// BypassInfo provides information about bypass configuration
type BypassInfo struct {
	Enabled        bool     `json:"enabled"`
	TrustedDomains []string `json:"trusted_domains"`
}
