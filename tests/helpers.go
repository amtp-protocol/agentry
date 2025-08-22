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

package tests

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
	"github.com/amtp-protocol/agentry/pkg/uuid"
)

// TestMessageBuilder provides a fluent interface for building test messages
type TestMessageBuilder struct {
	message *types.Message
}

// NewTestMessage creates a new test message builder with default values
func NewTestMessage() *TestMessageBuilder {
	messageID, _ := uuid.GenerateV7()
	idempotencyKey, _ := uuid.GenerateV4()

	return &TestMessageBuilder{
		message: &types.Message{
			Version:        "1.0",
			MessageID:      messageID,
			IdempotencyKey: idempotencyKey,
			Timestamp:      time.Now().UTC(),
			Sender:         "test@example.com",
			Recipients:     []string{"recipient@test.com"},
			Subject:        "Test Message",
			Payload:        json.RawMessage(`{"message": "Hello, World!"}`),
		},
	}
}

// WithSender sets the sender
func (b *TestMessageBuilder) WithSender(sender string) *TestMessageBuilder {
	b.message.Sender = sender
	return b
}

// WithRecipients sets the recipients
func (b *TestMessageBuilder) WithRecipients(recipients ...string) *TestMessageBuilder {
	b.message.Recipients = recipients
	return b
}

// WithSubject sets the subject
func (b *TestMessageBuilder) WithSubject(subject string) *TestMessageBuilder {
	b.message.Subject = subject
	return b
}

// WithPayload sets the payload
func (b *TestMessageBuilder) WithPayload(payload interface{}) *TestMessageBuilder {
	payloadBytes, _ := json.Marshal(payload)
	b.message.Payload = json.RawMessage(payloadBytes)
	return b
}

// WithSchema sets the schema
func (b *TestMessageBuilder) WithSchema(schema string) *TestMessageBuilder {
	b.message.Schema = schema
	return b
}

// WithHeaders sets the headers
func (b *TestMessageBuilder) WithHeaders(headers map[string]interface{}) *TestMessageBuilder {
	b.message.Headers = headers
	return b
}

// WithCoordination sets the coordination config
func (b *TestMessageBuilder) WithCoordination(coordination *types.CoordinationConfig) *TestMessageBuilder {
	b.message.Coordination = coordination
	return b
}

// WithParallelCoordination sets parallel coordination
func (b *TestMessageBuilder) WithParallelCoordination(timeout int) *TestMessageBuilder {
	b.message.Coordination = &types.CoordinationConfig{
		Type:    "parallel",
		Timeout: timeout,
	}
	return b
}

// WithSequentialCoordination sets sequential coordination
func (b *TestMessageBuilder) WithSequentialCoordination(sequence []string, timeout int) *TestMessageBuilder {
	b.message.Coordination = &types.CoordinationConfig{
		Type:     "sequential",
		Sequence: sequence,
		Timeout:  timeout,
	}
	return b
}

// WithConditionalCoordination sets conditional coordination
func (b *TestMessageBuilder) WithConditionalCoordination(conditions []types.ConditionalRule, timeout int) *TestMessageBuilder {
	b.message.Coordination = &types.CoordinationConfig{
		Type:       "conditional",
		Conditions: conditions,
		Timeout:    timeout,
	}
	return b
}

// WithAttachments sets the attachments
func (b *TestMessageBuilder) WithAttachments(attachments ...types.Attachment) *TestMessageBuilder {
	b.message.Attachments = attachments
	return b
}

// WithMessageID sets a specific message ID
func (b *TestMessageBuilder) WithMessageID(messageID string) *TestMessageBuilder {
	b.message.MessageID = messageID
	return b
}

// WithIdempotencyKey sets a specific idempotency key
func (b *TestMessageBuilder) WithIdempotencyKey(key string) *TestMessageBuilder {
	b.message.IdempotencyKey = key
	return b
}

// Build returns the constructed message
func (b *TestMessageBuilder) Build() *types.Message {
	return b.message
}

// TestSendRequestBuilder provides a fluent interface for building send requests
type TestSendRequestBuilder struct {
	request *types.SendMessageRequest
}

// NewTestSendRequest creates a new test send request builder with default values
func NewTestSendRequest() *TestSendRequestBuilder {
	return &TestSendRequestBuilder{
		request: &types.SendMessageRequest{
			Sender:     "test@example.com",
			Recipients: []string{"recipient@test.com"},
			Subject:    "Test Message",
			Payload:    json.RawMessage(`{"message": "Hello, World!"}`),
		},
	}
}

// WithSender sets the sender
func (b *TestSendRequestBuilder) WithSender(sender string) *TestSendRequestBuilder {
	b.request.Sender = sender
	return b
}

// WithRecipients sets the recipients
func (b *TestSendRequestBuilder) WithRecipients(recipients ...string) *TestSendRequestBuilder {
	b.request.Recipients = recipients
	return b
}

// WithSubject sets the subject
func (b *TestSendRequestBuilder) WithSubject(subject string) *TestSendRequestBuilder {
	b.request.Subject = subject
	return b
}

// WithPayload sets the payload
func (b *TestSendRequestBuilder) WithPayload(payload interface{}) *TestSendRequestBuilder {
	payloadBytes, _ := json.Marshal(payload)
	b.request.Payload = json.RawMessage(payloadBytes)
	return b
}

// WithSchema sets the schema
func (b *TestSendRequestBuilder) WithSchema(schema string) *TestSendRequestBuilder {
	b.request.Schema = schema
	return b
}

// WithHeaders sets the headers
func (b *TestSendRequestBuilder) WithHeaders(headers map[string]interface{}) *TestSendRequestBuilder {
	b.request.Headers = headers
	return b
}

// WithCoordination sets the coordination config
func (b *TestSendRequestBuilder) WithCoordination(coordination *types.CoordinationConfig) *TestSendRequestBuilder {
	b.request.Coordination = coordination
	return b
}

// WithAttachments sets the attachments
func (b *TestSendRequestBuilder) WithAttachments(attachments ...types.Attachment) *TestSendRequestBuilder {
	b.request.Attachments = attachments
	return b
}

// Build returns the constructed send request
func (b *TestSendRequestBuilder) Build() *types.SendMessageRequest {
	return b.request
}

// TestDataGenerator provides utilities for generating test data
type TestDataGenerator struct {
	rand *rand.Rand
}

// NewTestDataGenerator creates a new test data generator
func NewTestDataGenerator() *TestDataGenerator {
	return &TestDataGenerator{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// RandomEmail generates a random email address
func (g *TestDataGenerator) RandomEmail() string {
	domains := []string{"example.com", "test.com", "demo.org", "sample.net"}
	usernames := []string{"user", "test", "demo", "sample", "agent", "client"}

	username := usernames[g.rand.Intn(len(usernames))]
	domain := domains[g.rand.Intn(len(domains))]

	return fmt.Sprintf("%s%d@%s", username, g.rand.Intn(1000), domain)
}

// RandomEmails generates multiple random email addresses
func (g *TestDataGenerator) RandomEmails(count int) []string {
	emails := make([]string, count)
	for i := 0; i < count; i++ {
		emails[i] = g.RandomEmail()
	}
	return emails
}

// RandomSubject generates a random subject line
func (g *TestDataGenerator) RandomSubject() string {
	subjects := []string{
		"Important Update",
		"Meeting Request",
		"Project Status",
		"System Notification",
		"Action Required",
		"Weekly Report",
		"Urgent: Please Review",
		"Confirmation Needed",
	}

	return subjects[g.rand.Intn(len(subjects))]
}

// RandomPayload generates a random JSON payload
func (g *TestDataGenerator) RandomPayload() json.RawMessage {
	payloads := []map[string]interface{}{
		{"message": "Hello, World!", "priority": "high"},
		{"action": "approve", "document_id": "doc-123", "deadline": "2024-12-31"},
		{"event": "user_login", "user_id": "user-456", "timestamp": time.Now().Unix()},
		{"notification": "system_update", "version": "1.2.3", "changes": []string{"bug fixes", "improvements"}},
		{"request": "data_export", "format": "json", "filters": map[string]string{"date": "2024-01-01"}},
	}

	payload := payloads[g.rand.Intn(len(payloads))]
	payloadBytes, _ := json.Marshal(payload)
	return json.RawMessage(payloadBytes)
}

// RandomSchema generates a random schema identifier
func (g *TestDataGenerator) RandomSchema() string {
	schemas := []string{
		"agntcy:commerce.order.v1",
		"agntcy:finance.payment.v2",
		"agntcy:logistics.shipment.v1",
		"agntcy:hr.employee.v3",
		"agntcy:marketing.campaign.v1",
		"agntcy:support.ticket.v2",
	}

	return schemas[g.rand.Intn(len(schemas))]
}

// RandomHeaders generates random headers
func (g *TestDataGenerator) RandomHeaders() map[string]interface{} {
	headers := map[string]interface{}{
		"priority": []string{"low", "normal", "high", "urgent"}[g.rand.Intn(4)],
		"category": []string{"notification", "action", "update", "alert"}[g.rand.Intn(4)],
		"source":   []string{"system", "user", "api", "scheduler"}[g.rand.Intn(4)],
	}

	// Randomly add additional headers
	if g.rand.Float32() < 0.5 {
		headers["correlation_id"] = fmt.Sprintf("corr-%d", g.rand.Intn(10000))
	}

	if g.rand.Float32() < 0.3 {
		headers["retry_count"] = g.rand.Intn(5)
	}

	return headers
}

// RandomAttachment generates a random attachment
func (g *TestDataGenerator) RandomAttachment() types.Attachment {
	filenames := []string{"document.pdf", "image.jpg", "data.csv", "report.xlsx", "config.json"}
	contentTypes := []string{"application/pdf", "image/jpeg", "text/csv", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/json"}

	idx := g.rand.Intn(len(filenames))

	return types.Attachment{
		Filename:    filenames[idx],
		ContentType: contentTypes[idx],
		Size:        int64(g.rand.Intn(1000000) + 1000), // 1KB to 1MB
		Hash:        fmt.Sprintf("sha256:%x", g.rand.Uint64()),
		URL:         fmt.Sprintf("https://files.example.com/%d/%s", g.rand.Intn(1000), filenames[idx]),
	}
}

// RandomAttachments generates multiple random attachments
func (g *TestDataGenerator) RandomAttachments(count int) []types.Attachment {
	attachments := make([]types.Attachment, count)
	for i := 0; i < count; i++ {
		attachments[i] = g.RandomAttachment()
	}
	return attachments
}

// LargePayload generates a large payload for testing size limits
func (g *TestDataGenerator) LargePayload(sizeBytes int) json.RawMessage {
	// Create a payload with approximately the specified size
	data := make(map[string]string)
	currentSize := 0
	counter := 0

	for currentSize < sizeBytes {
		key := fmt.Sprintf("field_%d", counter)
		value := fmt.Sprintf("value_%d_%s", counter, g.randomString(100))
		data[key] = value

		// Rough estimate of JSON size
		currentSize += len(key) + len(value) + 10 // account for JSON formatting
		counter++
	}

	payloadBytes, _ := json.Marshal(data)
	return json.RawMessage(payloadBytes)
}

// randomString generates a random string of specified length
func (g *TestDataGenerator) randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[g.rand.Intn(len(charset))]
	}
	return string(b)
}

// TestAssertions provides common test assertions
type TestAssertions struct{}

// NewTestAssertions creates a new test assertions helper
func NewTestAssertions() *TestAssertions {
	return &TestAssertions{}
}

// AssertValidMessageID checks if a message ID is valid UUIDv7
func (a *TestAssertions) AssertValidMessageID(t interface{}, messageID string) bool {
	// This would use the uuid package to validate
	// For now, just check basic format
	return len(messageID) == 36 && messageID[8] == '-' && messageID[13] == '-'
}

// AssertValidEmail checks if an email address is valid
func (a *TestAssertions) AssertValidEmail(t interface{}, email string) bool {
	// Basic email validation
	return len(email) > 0 &&
		len(email) < 255 &&
		strings.Contains(email, "@") &&
		!strings.HasPrefix(email, "@") &&
		!strings.HasSuffix(email, "@")
}

// AssertValidTimestamp checks if a timestamp is valid and recent
func (a *TestAssertions) AssertValidTimestamp(t interface{}, timestamp time.Time) bool {
	now := time.Now()
	return !timestamp.IsZero() &&
		timestamp.Before(now.Add(time.Minute)) &&
		timestamp.After(now.Add(-time.Hour))
}

// AssertValidDeliveryStatus checks if a delivery status is valid
func (a *TestAssertions) AssertValidDeliveryStatus(t interface{}, status types.DeliveryStatus) bool {
	validStatuses := []types.DeliveryStatus{
		types.StatusPending,
		types.StatusQueued,
		types.StatusDelivering,
		types.StatusDelivered,
		types.StatusFailed,
		types.StatusRetrying,
	}

	for _, validStatus := range validStatuses {
		if status == validStatus {
			return true
		}
	}
	return false
}

// Performance testing utilities

// PerformanceTest represents a performance test configuration
type PerformanceTest struct {
	Name           string
	RequestsPerSec int
	Duration       time.Duration
	MaxConcurrency int
	RequestBuilder func() *types.SendMessageRequest
}

// LoadTestResult represents the result of a load test
type LoadTestResult struct {
	TotalRequests   int
	SuccessfulReqs  int
	FailedReqs      int
	AvgResponseTime time.Duration
	MaxResponseTime time.Duration
	MinResponseTime time.Duration
	RequestsPerSec  float64
	ErrorRate       float64
	Errors          map[string]int
}

// Mock utilities for testing

// MockTime provides utilities for mocking time in tests
type MockTime struct {
	currentTime time.Time
}

// NewMockTime creates a new mock time utility
func NewMockTime(initialTime time.Time) *MockTime {
	return &MockTime{currentTime: initialTime}
}

// Now returns the current mocked time
func (m *MockTime) Now() time.Time {
	return m.currentTime
}

// Advance advances the mocked time by the specified duration
func (m *MockTime) Advance(duration time.Duration) {
	m.currentTime = m.currentTime.Add(duration)
}

// Set sets the mocked time to a specific value
func (m *MockTime) Set(t time.Time) {
	m.currentTime = t
}

// Test data constants
const (
	ValidMessageID      = "01234567-89ab-7def-8123-456789abcdef"
	ValidIdempotencyKey = "01234567-89ab-4def-8123-456789abcdef"
	ValidEmail          = "test@example.com"
	InvalidEmail        = "invalid-email"
	ValidSchema         = "agntcy:commerce.order.v1"
	InvalidSchema       = "invalid-schema"
)

// Common test payloads
var (
	SimplePayload  = json.RawMessage(`{"message": "Hello, World!"}`)
	ComplexPayload = json.RawMessage(`{
		"order_id": "order-123",
		"customer": {
			"id": "cust-456",
			"name": "John Doe",
			"email": "john@example.com"
		},
		"items": [
			{"id": "item-1", "quantity": 2, "price": 19.99},
			{"id": "item-2", "quantity": 1, "price": 29.99}
		],
		"total": 69.97,
		"currency": "USD"
	}`)
)
