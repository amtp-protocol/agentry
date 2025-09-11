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

package metrics

import (
	"time"
)

// MetricsProvider defines the interface for metrics collection
type MetricsProvider interface {
	// HTTP metrics
	RecordHTTPRequest(method, path string, statusCode int, duration time.Duration)
	IncHTTPRequestsInFlight()
	DecHTTPRequestsInFlight()

	// Message processing metrics
	RecordMessage(status, coordinationType string, duration time.Duration, sizeBytes int64, schema string)
	IncMessagesInFlight()
	DecMessagesInFlight()

	// Delivery metrics
	RecordDelivery(status, domain string, duration time.Duration, attempts int)
	RecordDeliveryRetry(domain, reason string)

	// Discovery metrics
	RecordDiscovery(domain, method, status string, duration time.Duration, cacheHit bool)

	// System metrics
	SetConnectionsActive(count float64)
	SetMemoryUsage(bytes float64)
	SetGoroutinesActive(count float64)

	// Error metrics
	RecordError(component, errorCode, errorType string)

	// Export metrics as JSON
	ToJSON() ([]byte, error)
}

// NewMetricsProvider creates a new metrics provider instance
// Currently returns SimpleMetrics, but can be extended to support other implementations
func NewMetricsProvider() MetricsProvider {
	return NewSimpleMetrics()
}

// Timer provides a convenient way to time operations
type Timer struct {
	start time.Time
}

// NewTimer creates a new timer
func NewTimer() *Timer {
	return &Timer{start: time.Now()}
}

// Duration returns the elapsed duration
func (t *Timer) Duration() time.Duration {
	return time.Since(t.start)
}
