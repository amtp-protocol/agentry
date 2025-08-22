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
	"testing"

	"github.com/amtp-protocol/agentry/internal/types"
)

func TestNewBypassManager(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: []string{"example.com", "trusted.org"},
	}

	manager := NewBypassManager(config)

	if manager == nil {
		t.Errorf("expected manager to be created")
		return
	}

	if manager.config.Enabled != config.Enabled {
		t.Errorf("expected enabled %t, got %t", config.Enabled, manager.config.Enabled)
	}

	if len(manager.config.TrustedDomains) != len(config.TrustedDomains) {
		t.Errorf("expected %d trusted domains, got %d", len(config.TrustedDomains), len(manager.config.TrustedDomains))
	}
}

func TestBypassManager_ShouldBypass(t *testing.T) {
	tests := []struct {
		name     string
		config   BypassConfig
		message  *types.Message
		expected bool
	}{
		{
			name: "bypass disabled",
			config: BypassConfig{
				Enabled:        false,
				TrustedDomains: []string{"example.com"},
			},
			message: &types.Message{
				Sender: "user@example.com",
				Schema: "agntcy:commerce.order.v1",
			},
			expected: false,
		},
		{
			name: "no schema specified",
			config: BypassConfig{
				Enabled:        true,
				TrustedDomains: []string{"example.com"},
			},
			message: &types.Message{
				Sender: "user@untrusted.com",
				Schema: "",
			},
			expected: true,
		},
		{
			name: "trusted sender domain",
			config: BypassConfig{
				Enabled:        true,
				TrustedDomains: []string{"example.com", "trusted.org"},
			},
			message: &types.Message{
				Sender: "user@example.com",
				Schema: "agntcy:commerce.order.v1",
			},
			expected: true,
		},
		{
			name: "trusted sender domain case insensitive",
			config: BypassConfig{
				Enabled:        true,
				TrustedDomains: []string{"Example.Com"},
			},
			message: &types.Message{
				Sender: "user@example.com",
				Schema: "agntcy:commerce.order.v1",
			},
			expected: true,
		},
		{
			name: "untrusted sender domain",
			config: BypassConfig{
				Enabled:        true,
				TrustedDomains: []string{"example.com"},
			},
			message: &types.Message{
				Sender: "user@untrusted.com",
				Schema: "agntcy:commerce.order.v1",
			},
			expected: false,
		},
		{
			name: "invalid sender email format",
			config: BypassConfig{
				Enabled:        true,
				TrustedDomains: []string{"example.com"},
			},
			message: &types.Message{
				Sender: "invalid-email",
				Schema: "agntcy:commerce.order.v1",
			},
			expected: false,
		},
		{
			name: "sender with multiple @ symbols",
			config: BypassConfig{
				Enabled:        true,
				TrustedDomains: []string{"example.com"},
			},
			message: &types.Message{
				Sender: "user@test@example.com",
				Schema: "agntcy:commerce.order.v1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewBypassManager(tt.config)
			ctx := context.Background()

			result := manager.ShouldBypass(ctx, tt.message)

			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestBypassManager_isTrustedSender(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: []string{"example.com", "trusted.org"},
	}
	manager := NewBypassManager(config)

	tests := []struct {
		name     string
		sender   string
		expected bool
	}{
		{
			name:     "trusted domain",
			sender:   "user@example.com",
			expected: true,
		},
		{
			name:     "another trusted domain",
			sender:   "admin@trusted.org",
			expected: true,
		},
		{
			name:     "untrusted domain",
			sender:   "user@untrusted.com",
			expected: false,
		},
		{
			name:     "invalid email format",
			sender:   "invalid-email",
			expected: false,
		},
		{
			name:     "empty sender",
			sender:   "",
			expected: false,
		},
		{
			name:     "sender with no domain",
			sender:   "user@",
			expected: false,
		},
		{
			name:     "sender with empty domain",
			sender:   "user@",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.isTrustedSender(tt.sender)

			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestBypassManager_isTrustedDomain(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: []string{"example.com", "Trusted.Org", "test-domain.net"},
	}
	manager := NewBypassManager(config)

	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		{
			name:     "exact match",
			domain:   "example.com",
			expected: true,
		},
		{
			name:     "case insensitive match",
			domain:   "EXAMPLE.COM",
			expected: true,
		},
		{
			name:     "mixed case trusted domain",
			domain:   "trusted.org",
			expected: true,
		},
		{
			name:     "domain with hyphens",
			domain:   "test-domain.net",
			expected: true,
		},
		{
			name:     "untrusted domain",
			domain:   "untrusted.com",
			expected: false,
		},
		{
			name:     "empty domain",
			domain:   "",
			expected: false,
		},
		{
			name:     "partial match",
			domain:   "example",
			expected: false,
		},
		{
			name:     "subdomain not trusted",
			domain:   "sub.example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.isTrustedDomain(tt.domain)

			if result != tt.expected {
				t.Errorf("expected %t, got %t", tt.expected, result)
			}
		})
	}
}

func TestBypassManager_GetBypassInfo(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: []string{"example.com", "trusted.org"},
	}
	manager := NewBypassManager(config)

	info := manager.GetBypassInfo()

	if info == nil {
		t.Errorf("expected bypass info to be returned")
		return
	}

	if info.Enabled != config.Enabled {
		t.Errorf("expected enabled %t, got %t", config.Enabled, info.Enabled)
	}

	if len(info.TrustedDomains) != len(config.TrustedDomains) {
		t.Errorf("expected %d trusted domains, got %d", len(config.TrustedDomains), len(info.TrustedDomains))
	}

	for i, domain := range config.TrustedDomains {
		if info.TrustedDomains[i] != domain {
			t.Errorf("expected trusted domain %s at index %d, got %s", domain, i, info.TrustedDomains[i])
		}
	}
}

func TestBypassConfig_EmptyTrustedDomains(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: []string{},
	}
	manager := NewBypassManager(config)

	message := &types.Message{
		Sender: "user@example.com",
		Schema: "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	result := manager.ShouldBypass(ctx, message)

	// Should not bypass since no domains are trusted
	if result {
		t.Errorf("expected false when no trusted domains are configured")
	}
}

func TestBypassConfig_NilTrustedDomains(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: nil,
	}
	manager := NewBypassManager(config)

	message := &types.Message{
		Sender: "user@example.com",
		Schema: "agntcy:commerce.order.v1",
	}

	ctx := context.Background()
	result := manager.ShouldBypass(ctx, message)

	// Should not bypass since no domains are trusted
	if result {
		t.Errorf("expected false when trusted domains is nil")
	}
}

func TestBypassManager_EdgeCases(t *testing.T) {
	config := BypassConfig{
		Enabled:        true,
		TrustedDomains: []string{"example.com"},
	}
	manager := NewBypassManager(config)
	ctx := context.Background()

	t.Run("nil message", func(t *testing.T) {
		// Should handle nil message gracefully and return false (no bypass)
		result := manager.ShouldBypass(ctx, nil)
		if result {
			t.Errorf("expected ShouldBypass to return false for nil message, got true")
		}
	})

	t.Run("message with empty sender", func(t *testing.T) {
		message := &types.Message{
			Sender: "",
			Schema: "agntcy:commerce.order.v1",
		}

		result := manager.ShouldBypass(ctx, message)
		if result {
			t.Errorf("expected false for empty sender")
		}
	})
}
