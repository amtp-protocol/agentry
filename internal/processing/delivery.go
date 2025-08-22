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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/agents"
	"github.com/amtp-protocol/agentry/internal/discovery"
	"github.com/amtp-protocol/agentry/internal/schema"
	"github.com/amtp-protocol/agentry/internal/types"
)

// SchemaManager interface for schema validation
type SchemaManager interface {
	GetSchema(ctx context.Context, id schema.SchemaIdentifier) (*schema.Schema, error)
	ListSchemas(ctx context.Context, pattern string) ([]schema.SchemaIdentifier, error)
}

// DeliveryEngine handles outbound message delivery
type DeliveryEngine struct {
	httpClient    *http.Client
	discovery     DiscoveryService
	agentRegistry agents.AgentRegistry // for managing local agents
	config        DeliveryConfig
	localDomain   string
}

// DeliveryConfig defines delivery engine configuration
type DeliveryConfig struct {
	Timeout        time.Duration
	MaxRetries     int
	RetryDelay     time.Duration
	MaxConnections int
	IdleTimeout    time.Duration
	TLSConfig      *tls.Config
	UserAgent      string
	MaxMessageSize int64
	AllowHTTP      bool
	LocalDomain    string
}

// DeliveryResult represents the result of a delivery attempt
type DeliveryResult struct {
	Status       types.DeliveryStatus
	StatusCode   int
	ResponseBody string
	ErrorCode    string
	ErrorMessage string
	Timestamp    time.Time
	Attempts     int
	NextRetry    *time.Time
}

// NewDeliveryEngine creates a new delivery engine
func NewDeliveryEngine(discovery DiscoveryService, agentRegistry agents.AgentRegistry, config DeliveryConfig) *DeliveryEngine {
	// Create HTTP transport with connection pooling
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        config.MaxConnections,
		MaxIdleConnsPerHost: config.MaxConnections / 4,
		IdleConnTimeout:     config.IdleTimeout,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     config.TLSConfig,
		DisableCompression:  false,
	}

	// Create HTTP client
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Limit redirects to prevent infinite loops
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	return &DeliveryEngine{
		httpClient:    httpClient,
		discovery:     discovery,
		agentRegistry: agentRegistry,
		config:        config,
		localDomain:   config.LocalDomain,
	}
}

// DeliverMessage delivers a message to a specific recipient
func (de *DeliveryEngine) DeliverMessage(ctx context.Context, message *types.Message, recipient string) (*DeliveryResult, error) {
	result := &DeliveryResult{
		Status:    types.StatusDelivering,
		Timestamp: time.Now().UTC(),
		Attempts:  0,
	}

	// Extract domain from recipient
	domain := discovery.ExtractDomain(recipient)
	if domain == "" {
		result.Status = types.StatusFailed
		result.ErrorCode = "INVALID_RECIPIENT"
		result.ErrorMessage = "invalid recipient email format"
		return result, fmt.Errorf("invalid recipient email format: %s", recipient)
	}

	// Check if this is a local delivery (same domain as gateway)
	if domain == de.localDomain {
		return de.deliverLocal(ctx, message, recipient, result)
	}

	// Discover recipient capabilities
	capabilities, err := de.discovery.DiscoverCapabilities(ctx, domain)
	if err != nil {
		result.Status = types.StatusFailed
		result.ErrorCode = "DISCOVERY_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to discover capabilities for %s: %v", domain, err)
		return result, fmt.Errorf("discovery failed for %s: %w", domain, err)
	}

	// Validate gateway URL
	if err := discovery.ValidateGatewayURL(capabilities.Gateway, de.config.AllowHTTP); err != nil {
		result.Status = types.StatusFailed
		result.ErrorCode = "INVALID_GATEWAY"
		result.ErrorMessage = fmt.Sprintf("invalid gateway URL: %v", err)
		return result, fmt.Errorf("invalid gateway URL for %s: %w", domain, err)
	}

	// Check schema compatibility if specified
	if message.Schema != "" {
		supported, err := de.discovery.SupportsSchema(ctx, domain, message.Schema)
		if err != nil {
			result.Status = types.StatusFailed
			result.ErrorCode = "SCHEMA_CHECK_FAILED"
			result.ErrorMessage = fmt.Sprintf("failed to check schema support: %v", err)
			return result, fmt.Errorf("schema check failed for %s: %w", domain, err)
		}
		if !supported {
			result.Status = types.StatusFailed
			result.ErrorCode = "SCHEMA_NOT_SUPPORTED"
			result.ErrorMessage = fmt.Sprintf("schema %s not supported by %s", message.Schema, domain)
			return result, fmt.Errorf("schema %s not supported by %s", message.Schema, domain)
		}
	}

	// Check message size limits
	if capabilities.MaxSize > 0 && message.Size() > capabilities.MaxSize {
		result.Status = types.StatusFailed
		result.ErrorCode = "MESSAGE_TOO_LARGE"
		result.ErrorMessage = fmt.Sprintf("message size %d exceeds limit %d", message.Size(), capabilities.MaxSize)
		return result, fmt.Errorf("message too large for %s", domain)
	}

	// Attempt delivery with retries
	return de.attemptDeliveryWithRetries(ctx, message, recipient, capabilities, result)
}

// attemptDeliveryWithRetries attempts delivery with retry logic
func (de *DeliveryEngine) attemptDeliveryWithRetries(ctx context.Context, message *types.Message, recipient string, capabilities *discovery.AMTPCapabilities, result *DeliveryResult) (*DeliveryResult, error) {
	var lastErr error

	for attempt := 1; attempt <= de.config.MaxRetries; attempt++ {
		result.Attempts = attempt

		// Attempt delivery
		deliveryErr := de.attemptSingleDelivery(ctx, message, recipient, capabilities, result)
		if deliveryErr == nil {
			// Success
			result.Status = types.StatusDelivered
			return result, nil
		}

		lastErr = deliveryErr

		// Check if error is retryable
		if !de.isRetryableError(result.StatusCode, deliveryErr) {
			break
		}

		// Don't retry on last attempt
		if attempt == de.config.MaxRetries {
			break
		}

		// Calculate next retry time
		retryDelay := de.calculateRetryDelay(attempt)
		nextRetry := time.Now().Add(retryDelay)
		result.NextRetry = &nextRetry

		// Wait for retry delay or context cancellation
		select {
		case <-ctx.Done():
			result.Status = types.StatusFailed
			result.ErrorCode = "CONTEXT_CANCELLED"
			result.ErrorMessage = "delivery cancelled"
			return result, ctx.Err()
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	// All attempts failed
	result.Status = types.StatusFailed
	if result.ErrorCode == "" {
		result.ErrorCode = "DELIVERY_FAILED"
	}
	if result.ErrorMessage == "" {
		result.ErrorMessage = fmt.Sprintf("delivery failed after %d attempts", result.Attempts)
	}

	return result, lastErr
}

// attemptSingleDelivery attempts a single delivery
func (de *DeliveryEngine) attemptSingleDelivery(ctx context.Context, message *types.Message, recipient string, capabilities *discovery.AMTPCapabilities, result *DeliveryResult) error {
	// Prepare delivery payload
	deliveryPayload := map[string]interface{}{
		"version":         message.Version,
		"message_id":      message.MessageID,
		"idempotency_key": message.IdempotencyKey,
		"timestamp":       message.Timestamp.Format(time.RFC3339),
		"sender":          message.Sender,
		"recipients":      []string{recipient}, // Single recipient for this delivery
		"subject":         message.Subject,
		"schema":          message.Schema,
		"coordination":    message.Coordination,
		"headers":         message.Headers,
		"payload":         message.Payload,
		"attachments":     message.Attachments,
		"signature":       message.Signature,
		"in_reply_to":     message.InReplyTo,
		"response_type":   message.ResponseType,
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(deliveryPayload)
	if err != nil {
		result.ErrorCode = "PAYLOAD_MARSHAL_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to marshal payload: %v", err)
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	gatewayURL := strings.TrimSuffix(capabilities.Gateway, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(payloadBytes))
	if err != nil {
		result.ErrorCode = "REQUEST_CREATION_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to create request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", de.config.UserAgent)
	req.Header.Set("Accept", "application/json")

	// Add authentication headers if required
	// This would be expanded based on the authentication methods supported
	if len(capabilities.Auth) > 0 {
		// For now, just add a basic header indicating AMTP support
		req.Header.Set("X-AMTP-Version", "1.0")
	}

	// Perform HTTP request
	resp, err := de.httpClient.Do(req)
	if err != nil {
		result.ErrorCode = "HTTP_REQUEST_FAILED"
		result.ErrorMessage = fmt.Sprintf("HTTP request failed: %v", err)
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // nolint:errcheck // Ignore close error in defer
	}()

	result.StatusCode = resp.StatusCode

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		result.ErrorCode = "RESPONSE_READ_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to read response: %v", err)
		return fmt.Errorf("failed to read response: %w", err)
	}
	result.ResponseBody = string(bodyBytes)

	// Handle response based on status code
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// Success
		return nil

	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		// Client error - usually not retryable
		result.ErrorCode = "CLIENT_ERROR"
		result.ErrorMessage = fmt.Sprintf("client error %d: %s", resp.StatusCode, result.ResponseBody)
		return fmt.Errorf("client error %d: %s", resp.StatusCode, result.ResponseBody)

	case resp.StatusCode >= 500:
		// Server error - retryable
		result.ErrorCode = "SERVER_ERROR"
		result.ErrorMessage = fmt.Sprintf("server error %d: %s", resp.StatusCode, result.ResponseBody)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, result.ResponseBody)

	default:
		// Unexpected status code
		result.ErrorCode = "UNEXPECTED_STATUS"
		result.ErrorMessage = fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, result.ResponseBody)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, result.ResponseBody)
	}
}

// isRetryableError determines if an error is retryable
func (de *DeliveryEngine) isRetryableError(statusCode int, err error) bool {
	// Network errors are generally retryable
	if err != nil {
		// Check for network-related errors
		if strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "no such host") ||
			strings.Contains(err.Error(), "network unreachable") {
			return true
		}
	}

	// HTTP status codes that are retryable
	switch statusCode {
	case 429: // Too Many Requests
		return true
	case 502, 503, 504: // Bad Gateway, Service Unavailable, Gateway Timeout
		return true
	case 0: // No response received
		return true
	}

	// 4xx errors are generally not retryable (except 429)
	if statusCode >= 400 && statusCode < 500 {
		return false
	}

	// 5xx errors are retryable
	if statusCode >= 500 {
		return true
	}

	return false
}

// calculateRetryDelay calculates the delay before the next retry attempt
func (de *DeliveryEngine) calculateRetryDelay(attempt int) time.Duration {
	// Exponential backoff with jitter
	baseDelay := de.config.RetryDelay
	delay := baseDelay * time.Duration(1<<uint(attempt-1)) // 2^(attempt-1)

	// Cap the maximum delay
	maxDelay := 5 * time.Minute
	if delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter (±25%)
	jitter := time.Duration(float64(delay) * 0.25)
	delay = delay + time.Duration(float64(jitter)*(2*0.5-1)) // Random between -jitter and +jitter

	return delay
}

// DeliverBatch delivers a message to multiple recipients in parallel
func (de *DeliveryEngine) DeliverBatch(ctx context.Context, message *types.Message, recipients []string) (map[string]*DeliveryResult, error) {
	results := make(map[string]*DeliveryResult)
	resultChan := make(chan struct {
		recipient string
		result    *DeliveryResult
		err       error
	}, len(recipients))

	// Start delivery goroutines
	for _, recipient := range recipients {
		go func(addr string) {
			result, err := de.DeliverMessage(ctx, message, addr)
			resultChan <- struct {
				recipient string
				result    *DeliveryResult
				err       error
			}{addr, result, err}
		}(recipient)
	}

	// Collect results
	for i := 0; i < len(recipients); i++ {
		select {
		case res := <-resultChan:
			results[res.recipient] = res.result
		case <-ctx.Done():
			return results, ctx.Err()
		}
	}

	return results, nil
}

// deliverLocal handles local delivery for recipients in the same domain
func (de *DeliveryEngine) deliverLocal(ctx context.Context, message *types.Message, recipient string, result *DeliveryResult) (*DeliveryResult, error) {
	agent, err := de.agentRegistry.GetAgent(recipient)
	if err != nil {
		// Default to pull mode if agent is not registered
		return de.deliverLocalPull(ctx, message, recipient, result)
	}

	switch agent.DeliveryMode {
	case "push":
		return de.deliverLocalPush(ctx, message, recipient, agent, result)
	case "pull":
		return de.deliverLocalPull(ctx, message, recipient, result)
	default:
		result.Status = types.StatusFailed
		result.ErrorCode = "INVALID_DELIVERY_MODE"
		result.ErrorMessage = fmt.Sprintf("invalid delivery mode: %s", agent.DeliveryMode)
		return result, fmt.Errorf("invalid delivery mode: %s", agent.DeliveryMode)
	}
}

// deliverLocalPush delivers a message via push (webhook) to a local agent
func (de *DeliveryEngine) deliverLocalPush(ctx context.Context, message *types.Message, recipient string, agent *agents.LocalAgent, result *DeliveryResult) (*DeliveryResult, error) {
	if agent.PushTarget == "" {
		result.Status = types.StatusFailed
		result.ErrorCode = "MISSING_PUSH_TARGET"
		result.ErrorMessage = "push target URL is required for push delivery mode"
		return result, fmt.Errorf("push target URL is required for push delivery mode")
	}

	// Prepare delivery payload for local agent
	deliveryPayload := map[string]interface{}{
		"message_id":    message.MessageID,
		"sender":        message.Sender,
		"recipient":     recipient,
		"subject":       message.Subject,
		"schema":        message.Schema,
		"timestamp":     message.Timestamp.Format(time.RFC3339),
		"headers":       message.Headers,
		"payload":       message.Payload,
		"attachments":   message.Attachments,
		"coordination":  message.Coordination,
		"in_reply_to":   message.InReplyTo,
		"response_type": message.ResponseType,
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(deliveryPayload)
	if err != nil {
		result.Status = types.StatusFailed
		result.ErrorCode = "PAYLOAD_MARSHAL_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to marshal payload: %v", err)
		return result, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request to agent's webhook
	req, err := http.NewRequestWithContext(ctx, "POST", agent.PushTarget, bytes.NewReader(payloadBytes))
	if err != nil {
		result.Status = types.StatusFailed
		result.ErrorCode = "REQUEST_CREATION_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to create request: %v", err)
		return result, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", de.config.UserAgent)
	req.Header.Set("X-AMTP-Local-Delivery", "true")

	// Add custom headers from agent configuration
	for key, value := range agent.Headers {
		req.Header.Set(key, value)
	}

	// Perform HTTP request
	resp, err := de.httpClient.Do(req)
	if err != nil {
		result.Status = types.StatusFailed
		result.ErrorCode = "PUSH_REQUEST_FAILED"
		result.ErrorMessage = fmt.Sprintf("push request failed: %v", err)
		return result, fmt.Errorf("push request failed: %w", err)
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		result.ResponseBody = ""
	} else {
		result.ResponseBody = string(responseBody)
	}

	// Check if delivery was successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = types.StatusDelivered
		result.Attempts = 1
		return result, nil
	}

	// Push delivery failed
	result.Status = types.StatusFailed
	result.ErrorCode = "PUSH_DELIVERY_FAILED"
	result.ErrorMessage = fmt.Sprintf("push delivery failed with status %d", resp.StatusCode)
	result.Attempts = 1
	return result, fmt.Errorf("push delivery failed with status %d", resp.StatusCode)
}

// deliverLocalPull stores a message in the local inbox for pull-based delivery
func (de *DeliveryEngine) deliverLocalPull(ctx context.Context, message *types.Message, recipient string, result *DeliveryResult) (*DeliveryResult, error) {
	// Store message in recipient's inbox using agent registry
	if err := de.agentRegistry.StoreMessage(recipient, message); err != nil {
		result.Status = types.StatusFailed
		result.ErrorCode = "INBOX_STORE_FAILED"
		result.ErrorMessage = fmt.Sprintf("failed to store message in inbox: %v", err)
		return result, fmt.Errorf("failed to store message in inbox: %w", err)
	}

	// Mark as delivered (stored in inbox)
	result.Status = types.StatusDelivered
	result.Attempts = 1
	result.Timestamp = time.Now().UTC()
	return result, nil
}
