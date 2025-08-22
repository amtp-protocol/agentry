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
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	HTTPRequestsInFlight prometheus.Gauge

	// Message processing metrics
	MessagesTotal             *prometheus.CounterVec
	MessageProcessingDuration *prometheus.HistogramVec
	MessagesInFlight          prometheus.Gauge
	MessageSizeBytes          *prometheus.HistogramVec

	// Delivery metrics
	DeliveriesTotal  *prometheus.CounterVec
	DeliveryDuration *prometheus.HistogramVec
	DeliveryAttempts *prometheus.CounterVec
	DeliveryRetries  *prometheus.CounterVec

	// Discovery metrics
	DiscoveryTotal     *prometheus.CounterVec
	DiscoveryDuration  *prometheus.HistogramVec
	DiscoveryCacheHits *prometheus.CounterVec

	// System metrics
	ConnectionsActive prometheus.Gauge
	MemoryUsageBytes  prometheus.Gauge
	GoroutinesActive  prometheus.Gauge

	// Error metrics
	ErrorsTotal *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status_code"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "amtp_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status_code"},
		),
		HTTPRequestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "amtp_http_requests_in_flight",
				Help: "Number of HTTP requests currently being processed",
			},
		),

		// Message processing metrics
		MessagesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_messages_total",
				Help: "Total number of messages processed",
			},
			[]string{"status", "coordination_type"},
		),
		MessageProcessingDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "amtp_message_processing_duration_seconds",
				Help:    "Message processing duration in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
			},
			[]string{"status", "coordination_type"},
		),
		MessagesInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "amtp_messages_in_flight",
				Help: "Number of messages currently being processed",
			},
		),
		MessageSizeBytes: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "amtp_message_size_bytes",
				Help:    "Message size in bytes",
				Buckets: []float64{1024, 10240, 102400, 1048576, 10485760}, // 1KB to 10MB
			},
			[]string{"schema"},
		),

		// Delivery metrics
		DeliveriesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_deliveries_total",
				Help: "Total number of message deliveries attempted",
			},
			[]string{"status", "domain"},
		),
		DeliveryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "amtp_delivery_duration_seconds",
				Help:    "Message delivery duration in seconds",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"status", "domain"},
		),
		DeliveryAttempts: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_delivery_attempts_total",
				Help: "Total number of delivery attempts",
			},
			[]string{"domain"},
		),
		DeliveryRetries: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_delivery_retries_total",
				Help: "Total number of delivery retries",
			},
			[]string{"domain", "reason"},
		),

		// Discovery metrics
		DiscoveryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_discovery_total",
				Help: "Total number of capability discovery attempts",
			},
			[]string{"domain", "method", "status"},
		),
		DiscoveryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "amtp_discovery_duration_seconds",
				Help:    "Capability discovery duration in seconds",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
			},
			[]string{"domain", "method", "status"},
		),
		DiscoveryCacheHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_discovery_cache_hits_total",
				Help: "Total number of discovery cache hits",
			},
			[]string{"domain"},
		),

		// System metrics
		ConnectionsActive: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "amtp_connections_active",
				Help: "Number of active connections",
			},
		),
		MemoryUsageBytes: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "amtp_memory_usage_bytes",
				Help: "Memory usage in bytes",
			},
		),
		GoroutinesActive: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "amtp_goroutines_active",
				Help: "Number of active goroutines",
			},
		),

		// Error metrics
		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "amtp_errors_total",
				Help: "Total number of errors",
			},
			[]string{"component", "error_code", "error_type"},
		),
	}
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration) {
	statusStr := strconv.Itoa(statusCode)
	m.HTTPRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
}

// IncHTTPRequestsInFlight increments in-flight HTTP requests
func (m *Metrics) IncHTTPRequestsInFlight() {
	m.HTTPRequestsInFlight.Inc()
}

// DecHTTPRequestsInFlight decrements in-flight HTTP requests
func (m *Metrics) DecHTTPRequestsInFlight() {
	m.HTTPRequestsInFlight.Dec()
}

// RecordMessage records message processing metrics
func (m *Metrics) RecordMessage(status, coordinationType string, duration time.Duration, sizeBytes int64, schema string) {
	m.MessagesTotal.WithLabelValues(status, coordinationType).Inc()
	m.MessageProcessingDuration.WithLabelValues(status, coordinationType).Observe(duration.Seconds())

	if sizeBytes > 0 {
		m.MessageSizeBytes.WithLabelValues(schema).Observe(float64(sizeBytes))
	}
}

// IncMessagesInFlight increments in-flight messages
func (m *Metrics) IncMessagesInFlight() {
	m.MessagesInFlight.Inc()
}

// DecMessagesInFlight decrements in-flight messages
func (m *Metrics) DecMessagesInFlight() {
	m.MessagesInFlight.Dec()
}

// RecordDelivery records delivery metrics
func (m *Metrics) RecordDelivery(status, domain string, duration time.Duration, attempts int) {
	m.DeliveriesTotal.WithLabelValues(status, domain).Inc()
	m.DeliveryDuration.WithLabelValues(status, domain).Observe(duration.Seconds())

	for i := 0; i < attempts; i++ {
		m.DeliveryAttempts.WithLabelValues(domain).Inc()
	}
}

// RecordDeliveryRetry records delivery retry metrics
func (m *Metrics) RecordDeliveryRetry(domain, reason string) {
	m.DeliveryRetries.WithLabelValues(domain, reason).Inc()
}

// RecordDiscovery records discovery metrics
func (m *Metrics) RecordDiscovery(domain, method, status string, duration time.Duration, cacheHit bool) {
	m.DiscoveryTotal.WithLabelValues(domain, method, status).Inc()
	m.DiscoveryDuration.WithLabelValues(domain, method, status).Observe(duration.Seconds())

	if cacheHit {
		m.DiscoveryCacheHits.WithLabelValues(domain).Inc()
	}
}

// SetConnectionsActive sets the number of active connections
func (m *Metrics) SetConnectionsActive(count float64) {
	m.ConnectionsActive.Set(count)
}

// SetMemoryUsage sets the memory usage
func (m *Metrics) SetMemoryUsage(bytes float64) {
	m.MemoryUsageBytes.Set(bytes)
}

// SetGoroutinesActive sets the number of active goroutines
func (m *Metrics) SetGoroutinesActive(count float64) {
	m.GoroutinesActive.Set(count)
}

// RecordError records error metrics
func (m *Metrics) RecordError(component, errorCode, errorType string) {
	m.ErrorsTotal.WithLabelValues(component, errorCode, errorType).Inc()
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

// ObserveHistogram observes the elapsed time in a histogram
func (t *Timer) ObserveHistogram(histogram prometheus.Observer) {
	histogram.Observe(t.Duration().Seconds())
}

// Helper functions for common metric patterns

// WithTimer executes a function and measures its duration
func WithTimer(fn func() error, observer prometheus.Observer) error {
	timer := NewTimer()
	err := fn()
	observer.Observe(timer.Duration().Seconds())
	return err
}

// WithTimerAndLabels executes a function and measures its duration with labels
func WithTimerAndLabels(fn func() error, histogram *prometheus.HistogramVec, labels ...string) error {
	timer := NewTimer()
	err := fn()
	histogram.WithLabelValues(labels...).Observe(timer.Duration().Seconds())
	return err
}
