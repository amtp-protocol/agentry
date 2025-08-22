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

package processing

import (
	"context"
	"encoding/json"
	"time"

	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/types"
)

// MockDeliveryEngine for testing
type MockDeliveryEngine struct {
	deliveryResults map[string]*DeliveryResult
	deliveryError   error
}

func NewMockDeliveryEngine() *MockDeliveryEngine {
	return &MockDeliveryEngine{
		deliveryResults: make(map[string]*DeliveryResult),
	}
}

func (m *MockDeliveryEngine) DeliverMessage(ctx context.Context, message *types.Message, recipient string) (*DeliveryResult, error) {
	if m.deliveryError != nil {
		return nil, m.deliveryError
	}

	if result, exists := m.deliveryResults[recipient]; exists {
		return result, nil
	}

	// Default successful delivery
	return &DeliveryResult{
		Status:     types.StatusDelivered,
		StatusCode: 200,
		Timestamp:  time.Now().UTC(),
		Attempts:   1,
	}, nil
}

func (m *MockDeliveryEngine) SetDeliveryResult(recipient string, result *DeliveryResult) {
	m.deliveryResults[recipient] = result
}

func (m *MockDeliveryEngine) SetDeliveryError(err error) {
	m.deliveryError = err
}

// MockDiscovery for testing
type MockDiscovery struct {
	capabilities map[string]*discovery.AMTPCapabilities
	error        error
}

func NewMockDiscovery() *MockDiscovery {
	return &MockDiscovery{
		capabilities: make(map[string]*discovery.AMTPCapabilities),
	}
}

func (m *MockDiscovery) DiscoverCapabilities(ctx context.Context, domain string) (*discovery.AMTPCapabilities, error) {
	if m.error != nil {
		return nil, m.error
	}

	if cap, exists := m.capabilities[domain]; exists {
		return cap, nil
	}

	// Default capabilities
	return &discovery.AMTPCapabilities{
		Version:      "1.0",
		Gateway:      "https://" + domain,
		MaxSize:      10485760,
		Features:     []string{"immediate-path"},
		DiscoveredAt: time.Now(),
		TTL:          5 * time.Minute,
	}, nil
}

func (m *MockDiscovery) SetCapabilities(domain string, cap *discovery.AMTPCapabilities) {
	m.capabilities[domain] = cap
}

func (m *MockDiscovery) SetError(err error) {
	m.error = err
}

func (m *MockDiscovery) SupportsSchema(ctx context.Context, domain, schema string) (bool, error) {
	if m.error != nil {
		return false, m.error
	}
	// Default to supporting all schemas
	return true, nil
}

// Test message creation helper
func createTestMessage() *types.Message {
	return &types.Message{
		Version:        "1.0",
		MessageID:      "01234567-89ab-7def-8123-456789abcdef",
		IdempotencyKey: "01234567-89ab-4def-8123-456789abcdef",
		Timestamp:      time.Now().UTC(),
		Sender:         "test@example.com",
		Recipients:     []string{"recipient@test.com"},
		Subject:        "Test Message",
		Payload:        json.RawMessage(`{"message": "Hello, World!"}`),
	}
}
