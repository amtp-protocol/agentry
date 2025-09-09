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

	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/types"
)

// Ensure Discovery implementations implement DiscoveryService
var _ DiscoveryService = (*discovery.Discovery)(nil)
var _ DiscoveryService = (*discovery.MockDiscovery)(nil)

// Ensure DeliveryEngine implements DeliveryService
var _ DeliveryService = (*DeliveryEngine)(nil)

// Ensure MessageProcessor implements MessageProcessorService
var _ MessageProcessorService = (*MessageProcessor)(nil)

// DiscoveryService defines the interface for AMTP discovery
type DiscoveryService interface {
	DiscoverCapabilities(ctx context.Context, domain string) (*discovery.AMTPCapabilities, error)
	SupportsSchema(ctx context.Context, domain, schema string) (bool, error)
}

// DeliveryService defines the interface for message delivery
type DeliveryService interface {
	DeliverMessage(ctx context.Context, message *types.Message, recipient string) (*DeliveryResult, error)
}

// MessageProcessorService defines the interface for message processing
type MessageProcessorService interface {
	ProcessMessage(ctx context.Context, message *types.Message, options ProcessingOptions) (*ProcessingResult, error)
	GetMessage(messageID string) (*types.Message, error)
	GetMessageStatus(messageID string) (*types.MessageStatus, error)
	GetInboxMessages(recipient string) []*types.Message
	AcknowledgeMessage(recipient, messageID string) error
}
