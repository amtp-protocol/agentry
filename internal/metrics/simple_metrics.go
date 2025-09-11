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
	"encoding/json"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// SimpleMetrics provides a simple in-memory metrics implementation
type SimpleMetrics struct {
	mu sync.RWMutex

	// HTTP metrics
	httpRequests  map[string]int64
	httpDurations map[string][]float64
	httpInFlight  int64

	// Message processing metrics
	messages         map[string]int64
	messageDurations map[string][]float64
	messagesInFlight int64
	messageSizes     map[string][]float64

	// Delivery metrics
	deliveries        map[string]int64
	deliveryDurations map[string][]float64
	deliveryAttempts  map[string]int64
	deliveryRetries   map[string]int64

	// Discovery metrics
	discoveries        map[string]int64
	discoveryDurations map[string][]float64
	discoveryCacheHits map[string]int64

	// System metrics
	connectionsActive float64
	memoryUsageBytes  float64
	goroutinesActive  float64

	// Error metrics
	errors map[string]int64

	// Timestamps
	startTime  time.Time
	lastUpdate time.Time
}

// NewSimpleMetrics creates a new simple metrics instance
func NewSimpleMetrics() *SimpleMetrics {
	return &SimpleMetrics{
		httpRequests:       make(map[string]int64),
		httpDurations:      make(map[string][]float64),
		messages:           make(map[string]int64),
		messageDurations:   make(map[string][]float64),
		messageSizes:       make(map[string][]float64),
		deliveries:         make(map[string]int64),
		deliveryDurations:  make(map[string][]float64),
		deliveryAttempts:   make(map[string]int64),
		deliveryRetries:    make(map[string]int64),
		discoveries:        make(map[string]int64),
		discoveryDurations: make(map[string][]float64),
		discoveryCacheHits: make(map[string]int64),
		errors:             make(map[string]int64),
		startTime:          time.Now(),
		lastUpdate:         time.Now(),
	}
}

// RecordHTTPRequest records HTTP request metrics
func (m *SimpleMetrics) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := method + ":" + path + ":" + strconv.Itoa(statusCode)
	m.httpRequests[key]++
	m.httpDurations[key] = append(m.httpDurations[key], duration.Seconds())
	m.lastUpdate = time.Now()
}

// IncHTTPRequestsInFlight increments in-flight HTTP requests
func (m *SimpleMetrics) IncHTTPRequestsInFlight() {
	atomic.AddInt64(&m.httpInFlight, 1)
}

// DecHTTPRequestsInFlight decrements in-flight HTTP requests
func (m *SimpleMetrics) DecHTTPRequestsInFlight() {
	atomic.AddInt64(&m.httpInFlight, -1)
}

// RecordMessage records message processing metrics
func (m *SimpleMetrics) RecordMessage(status, coordinationType string, duration time.Duration, sizeBytes int64, schema string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := status + ":" + coordinationType
	m.messages[key]++
	m.messageDurations[key] = append(m.messageDurations[key], duration.Seconds())

	if sizeBytes > 0 && schema != "" {
		m.messageSizes[schema] = append(m.messageSizes[schema], float64(sizeBytes))
	}
	m.lastUpdate = time.Now()
}

// IncMessagesInFlight increments in-flight messages
func (m *SimpleMetrics) IncMessagesInFlight() {
	atomic.AddInt64(&m.messagesInFlight, 1)
}

// DecMessagesInFlight decrements in-flight messages
func (m *SimpleMetrics) DecMessagesInFlight() {
	atomic.AddInt64(&m.messagesInFlight, -1)
}

// RecordDelivery records delivery metrics
func (m *SimpleMetrics) RecordDelivery(status, domain string, duration time.Duration, attempts int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := status + ":" + domain
	m.deliveries[key]++
	m.deliveryDurations[key] = append(m.deliveryDurations[key], duration.Seconds())
	m.deliveryAttempts[domain] += int64(attempts)
	m.lastUpdate = time.Now()
}

// RecordDeliveryRetry records delivery retry metrics
func (m *SimpleMetrics) RecordDeliveryRetry(domain, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := domain + ":" + reason
	m.deliveryRetries[key]++
	m.lastUpdate = time.Now()
}

// RecordDiscovery records discovery metrics
func (m *SimpleMetrics) RecordDiscovery(domain, method, status string, duration time.Duration, cacheHit bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := domain + ":" + method + ":" + status
	m.discoveries[key]++
	m.discoveryDurations[key] = append(m.discoveryDurations[key], duration.Seconds())

	if cacheHit {
		m.discoveryCacheHits[domain]++
	}
	m.lastUpdate = time.Now()
}

// SetConnectionsActive sets the number of active connections
func (m *SimpleMetrics) SetConnectionsActive(count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionsActive = count
	m.lastUpdate = time.Now()
}

// SetMemoryUsage sets the memory usage
func (m *SimpleMetrics) SetMemoryUsage(bytes float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryUsageBytes = bytes
	m.lastUpdate = time.Now()
}

// SetGoroutinesActive sets the number of active goroutines
func (m *SimpleMetrics) SetGoroutinesActive(count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goroutinesActive = count
	m.lastUpdate = time.Now()
}

// RecordError records error metrics
func (m *SimpleMetrics) RecordError(component, errorCode, errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := component + ":" + errorCode + ":" + errorType
	m.errors[key]++
	m.lastUpdate = time.Now()
}

// ToJSON exports metrics as JSON
func (m *SimpleMetrics) ToJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Update system metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	data := map[string]interface{}{
		"timestamp":      m.lastUpdate.Unix(),
		"uptime_seconds": time.Since(m.startTime).Seconds(),
		"http": map[string]interface{}{
			"requests":  m.httpRequests,
			"durations": m.calculateStats(m.httpDurations),
			"in_flight": atomic.LoadInt64(&m.httpInFlight),
		},
		"messages": map[string]interface{}{
			"total":     m.messages,
			"durations": m.calculateStats(m.messageDurations),
			"in_flight": atomic.LoadInt64(&m.messagesInFlight),
			"sizes":     m.calculateStats(m.messageSizes),
		},
		"deliveries": map[string]interface{}{
			"total":     m.deliveries,
			"durations": m.calculateStats(m.deliveryDurations),
			"attempts":  m.deliveryAttempts,
			"retries":   m.deliveryRetries,
		},
		"discovery": map[string]interface{}{
			"total":      m.discoveries,
			"durations":  m.calculateStats(m.discoveryDurations),
			"cache_hits": m.discoveryCacheHits,
		},
		"system": map[string]interface{}{
			"connections_active": m.connectionsActive,
			"memory_usage_bytes": memStats.Alloc,
			"memory_total_bytes": memStats.TotalAlloc,
			"goroutines_active":  runtime.NumGoroutine(),
			"gc_cycles":          memStats.NumGC,
		},
		"errors": m.errors,
	}

	return json.Marshal(data)
}

// calculateStats calculates basic statistics for duration/size arrays
func (m *SimpleMetrics) calculateStats(data map[string][]float64) map[string]interface{} {
	stats := make(map[string]interface{})

	for key, values := range data {
		if len(values) == 0 {
			continue
		}

		sum := 0.0
		min := values[0]
		max := values[0]

		for _, v := range values {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}

		avg := sum / float64(len(values))

		stats[key] = map[string]interface{}{
			"count": len(values),
			"sum":   sum,
			"avg":   avg,
			"min":   min,
			"max":   max,
		}
	}

	return stats
}
