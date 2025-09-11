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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewSimpleMetrics(t *testing.T) {
	metrics := NewSimpleMetrics()

	if metrics == nil {
		t.Fatal("NewSimpleMetrics() returned nil")
	}

	// Verify all maps are initialized
	if metrics.httpRequests == nil {
		t.Error("httpRequests map should be initialized")
	}
	if metrics.httpDurations == nil {
		t.Error("httpDurations map should be initialized")
	}
	if metrics.messages == nil {
		t.Error("messages map should be initialized")
	}
	if metrics.messageDurations == nil {
		t.Error("messageDurations map should be initialized")
	}
	if metrics.messageSizes == nil {
		t.Error("messageSizes map should be initialized")
	}
	if metrics.deliveries == nil {
		t.Error("deliveries map should be initialized")
	}
	if metrics.deliveryDurations == nil {
		t.Error("deliveryDurations map should be initialized")
	}
	if metrics.deliveryAttempts == nil {
		t.Error("deliveryAttempts map should be initialized")
	}
	if metrics.deliveryRetries == nil {
		t.Error("deliveryRetries map should be initialized")
	}
	if metrics.discoveries == nil {
		t.Error("discoveries map should be initialized")
	}
	if metrics.discoveryDurations == nil {
		t.Error("discoveryDurations map should be initialized")
	}
	if metrics.discoveryCacheHits == nil {
		t.Error("discoveryCacheHits map should be initialized")
	}
	if metrics.errors == nil {
		t.Error("errors map should be initialized")
	}

	// Verify timestamps are set
	if metrics.startTime.IsZero() {
		t.Error("startTime should be set")
	}
	if metrics.lastUpdate.IsZero() {
		t.Error("lastUpdate should be set")
	}

	// Verify atomic counters are zero
	if atomic.LoadInt64(&metrics.httpInFlight) != 0 {
		t.Error("httpInFlight should be zero initially")
	}
	if atomic.LoadInt64(&metrics.messagesInFlight) != 0 {
		t.Error("messagesInFlight should be zero initially")
	}
}

func TestSimpleMetrics_RecordHTTPRequest(t *testing.T) {
	metrics := NewSimpleMetrics()

	method := "GET"
	path := "/api/test"
	statusCode := 200
	duration := 100 * time.Millisecond

	// Record first request
	metrics.RecordHTTPRequest(method, path, statusCode, duration)

	// Verify the request was recorded
	key := "GET:/api/test:200"
	if count := metrics.httpRequests[key]; count != 1 {
		t.Errorf("Expected 1 request, got %d", count)
	}

	if len(metrics.httpDurations[key]) != 1 {
		t.Errorf("Expected 1 duration entry, got %d", len(metrics.httpDurations[key]))
	}

	expectedDuration := duration.Seconds()
	if metrics.httpDurations[key][0] != expectedDuration {
		t.Errorf("Expected duration %f, got %f", expectedDuration, metrics.httpDurations[key][0])
	}

	// Record second request
	metrics.RecordHTTPRequest(method, path, statusCode, duration*2)

	if count := metrics.httpRequests[key]; count != 2 {
		t.Errorf("Expected 2 requests, got %d", count)
	}

	if len(metrics.httpDurations[key]) != 2 {
		t.Errorf("Expected 2 duration entries, got %d", len(metrics.httpDurations[key]))
	}
}

func TestSimpleMetrics_HTTPInFlight(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Initially should be zero
	if count := atomic.LoadInt64(&metrics.httpInFlight); count != 0 {
		t.Errorf("Expected 0 in-flight requests, got %d", count)
	}

	// Increment
	metrics.IncHTTPRequestsInFlight()
	if count := atomic.LoadInt64(&metrics.httpInFlight); count != 1 {
		t.Errorf("Expected 1 in-flight request, got %d", count)
	}

	// Increment again
	metrics.IncHTTPRequestsInFlight()
	if count := atomic.LoadInt64(&metrics.httpInFlight); count != 2 {
		t.Errorf("Expected 2 in-flight requests, got %d", count)
	}

	// Decrement
	metrics.DecHTTPRequestsInFlight()
	if count := atomic.LoadInt64(&metrics.httpInFlight); count != 1 {
		t.Errorf("Expected 1 in-flight request, got %d", count)
	}

	// Decrement to zero
	metrics.DecHTTPRequestsInFlight()
	if count := atomic.LoadInt64(&metrics.httpInFlight); count != 0 {
		t.Errorf("Expected 0 in-flight requests, got %d", count)
	}
}

func TestSimpleMetrics_RecordMessage(t *testing.T) {
	metrics := NewSimpleMetrics()

	status := "success"
	coordinationType := "direct"
	duration := 50 * time.Millisecond
	sizeBytes := int64(1024)
	schema := "test-schema"

	metrics.RecordMessage(status, coordinationType, duration, sizeBytes, schema)

	// Verify message was recorded
	key := "success:direct"
	if count := metrics.messages[key]; count != 1 {
		t.Errorf("Expected 1 message, got %d", count)
	}

	if len(metrics.messageDurations[key]) != 1 {
		t.Errorf("Expected 1 duration entry, got %d", len(metrics.messageDurations[key]))
	}

	// Verify size was recorded
	if len(metrics.messageSizes[schema]) != 1 {
		t.Errorf("Expected 1 size entry, got %d", len(metrics.messageSizes[schema]))
	}

	if metrics.messageSizes[schema][0] != float64(sizeBytes) {
		t.Errorf("Expected size %f, got %f", float64(sizeBytes), metrics.messageSizes[schema][0])
	}
}

func TestSimpleMetrics_RecordMessage_NoSize(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Record message without size/schema
	metrics.RecordMessage("success", "direct", 50*time.Millisecond, 0, "")

	// Should not record size
	if len(metrics.messageSizes) != 0 {
		t.Errorf("Expected no size entries, got %d", len(metrics.messageSizes))
	}
}

func TestSimpleMetrics_MessagesInFlight(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Test increment/decrement similar to HTTP
	metrics.IncMessagesInFlight()
	if count := atomic.LoadInt64(&metrics.messagesInFlight); count != 1 {
		t.Errorf("Expected 1 in-flight message, got %d", count)
	}

	metrics.DecMessagesInFlight()
	if count := atomic.LoadInt64(&metrics.messagesInFlight); count != 0 {
		t.Errorf("Expected 0 in-flight messages, got %d", count)
	}
}

func TestSimpleMetrics_RecordDelivery(t *testing.T) {
	metrics := NewSimpleMetrics()

	status := "delivered"
	domain := "example.com"
	duration := 200 * time.Millisecond
	attempts := 3

	metrics.RecordDelivery(status, domain, duration, attempts)

	// Verify delivery was recorded
	key := "delivered:example.com"
	if count := metrics.deliveries[key]; count != 1 {
		t.Errorf("Expected 1 delivery, got %d", count)
	}

	if len(metrics.deliveryDurations[key]) != 1 {
		t.Errorf("Expected 1 duration entry, got %d", len(metrics.deliveryDurations[key]))
	}

	// Verify attempts were recorded
	if attemptCount := metrics.deliveryAttempts[domain]; attemptCount != int64(attempts) {
		t.Errorf("Expected %d attempts, got %d", attempts, attemptCount)
	}
}

func TestSimpleMetrics_RecordDeliveryRetry(t *testing.T) {
	metrics := NewSimpleMetrics()

	domain := "example.com"
	reason := "timeout"

	metrics.RecordDeliveryRetry(domain, reason)

	key := "example.com:timeout"
	if count := metrics.deliveryRetries[key]; count != 1 {
		t.Errorf("Expected 1 retry, got %d", count)
	}

	// Record another retry
	metrics.RecordDeliveryRetry(domain, reason)
	if count := metrics.deliveryRetries[key]; count != 2 {
		t.Errorf("Expected 2 retries, got %d", count)
	}
}

func TestSimpleMetrics_RecordDiscovery(t *testing.T) {
	metrics := NewSimpleMetrics()

	domain := "example.com"
	method := "dns"
	status := "success"
	duration := 30 * time.Millisecond
	cacheHit := true

	metrics.RecordDiscovery(domain, method, status, duration, cacheHit)

	// Verify discovery was recorded
	key := "example.com:dns:success"
	if count := metrics.discoveries[key]; count != 1 {
		t.Errorf("Expected 1 discovery, got %d", count)
	}

	if len(metrics.discoveryDurations[key]) != 1 {
		t.Errorf("Expected 1 duration entry, got %d", len(metrics.discoveryDurations[key]))
	}

	// Verify cache hit was recorded
	if hits := metrics.discoveryCacheHits[domain]; hits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", hits)
	}

	// Record discovery without cache hit
	metrics.RecordDiscovery(domain, method, status, duration, false)

	// Cache hits should remain 1
	if hits := metrics.discoveryCacheHits[domain]; hits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", hits)
	}
}

func TestSimpleMetrics_SystemMetrics(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Test SetConnectionsActive
	metrics.SetConnectionsActive(10.5)
	if metrics.connectionsActive != 10.5 {
		t.Errorf("Expected 10.5 active connections, got %f", metrics.connectionsActive)
	}

	// Test SetMemoryUsage
	metrics.SetMemoryUsage(1024.0)
	if metrics.memoryUsageBytes != 1024.0 {
		t.Errorf("Expected 1024.0 memory usage, got %f", metrics.memoryUsageBytes)
	}

	// Test SetGoroutinesActive
	metrics.SetGoroutinesActive(25.0)
	if metrics.goroutinesActive != 25.0 {
		t.Errorf("Expected 25.0 active goroutines, got %f", metrics.goroutinesActive)
	}
}

func TestSimpleMetrics_RecordError(t *testing.T) {
	metrics := NewSimpleMetrics()

	component := "server"
	errorCode := "500"
	errorType := "internal"

	metrics.RecordError(component, errorCode, errorType)

	key := "server:500:internal"
	if count := metrics.errors[key]; count != 1 {
		t.Errorf("Expected 1 error, got %d", count)
	}

	// Record same error again
	metrics.RecordError(component, errorCode, errorType)
	if count := metrics.errors[key]; count != 2 {
		t.Errorf("Expected 2 errors, got %d", count)
	}
}

func TestSimpleMetrics_ToJSON(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Record some test data
	metrics.RecordHTTPRequest("GET", "/test", 200, 100*time.Millisecond)
	metrics.RecordMessage("success", "direct", 50*time.Millisecond, 1024, "test-schema")
	metrics.RecordDelivery("delivered", "example.com", 200*time.Millisecond, 2)
	metrics.RecordDiscovery("example.com", "dns", "success", 30*time.Millisecond, true)
	metrics.RecordError("server", "500", "internal")
	metrics.SetConnectionsActive(5.0)

	jsonData, err := metrics.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() failed: %v", err)
	}

	// Parse JSON to verify structure
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify top-level keys exist
	expectedKeys := []string{"timestamp", "uptime_seconds", "http", "messages", "deliveries", "discovery", "system", "errors"}
	for _, key := range expectedKeys {
		if _, exists := data[key]; !exists {
			t.Errorf("Missing key in JSON: %s", key)
		}
	}

	// Verify HTTP section
	httpData := data["http"].(map[string]interface{})
	if httpData["in_flight"].(float64) != 0 {
		t.Error("Expected 0 in-flight HTTP requests")
	}

	// Verify system section has runtime metrics
	systemData := data["system"].(map[string]interface{})
	if _, exists := systemData["memory_usage_bytes"]; !exists {
		t.Error("Missing memory_usage_bytes in system metrics")
	}
	if _, exists := systemData["goroutines_active"]; !exists {
		t.Error("Missing goroutines_active in system metrics")
	}
}

func TestSimpleMetrics_CalculateStats(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Create test data
	testData := map[string][]float64{
		"test1": {1.0, 2.0, 3.0, 4.0, 5.0},
		"test2": {10.0, 20.0},
		"empty": {},
	}

	stats := metrics.calculateStats(testData)

	// Verify test1 stats
	test1Stats := stats["test1"].(map[string]interface{})
	if test1Stats["count"].(int) != 5 {
		t.Errorf("Expected count 5, got %v", test1Stats["count"])
	}
	if test1Stats["sum"].(float64) != 15.0 {
		t.Errorf("Expected sum 15.0, got %v", test1Stats["sum"])
	}
	if test1Stats["avg"].(float64) != 3.0 {
		t.Errorf("Expected avg 3.0, got %v", test1Stats["avg"])
	}
	if test1Stats["min"].(float64) != 1.0 {
		t.Errorf("Expected min 1.0, got %v", test1Stats["min"])
	}
	if test1Stats["max"].(float64) != 5.0 {
		t.Errorf("Expected max 5.0, got %v", test1Stats["max"])
	}

	// Verify test2 stats
	test2Stats := stats["test2"].(map[string]interface{})
	if test2Stats["count"].(int) != 2 {
		t.Errorf("Expected count 2, got %v", test2Stats["count"])
	}
	if test2Stats["avg"].(float64) != 15.0 {
		t.Errorf("Expected avg 15.0, got %v", test2Stats["avg"])
	}

	// Verify empty data is not included
	if _, exists := stats["empty"]; exists {
		t.Error("Empty data should not be included in stats")
	}
}

func TestSimpleMetrics_ConcurrentAccess(t *testing.T) {
	metrics := NewSimpleMetrics()

	// Test concurrent access to ensure thread safety
	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// Concurrent HTTP requests
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				metrics.RecordHTTPRequest("GET", "/test", 200, time.Millisecond)
				metrics.IncHTTPRequestsInFlight()
				metrics.DecHTTPRequestsInFlight()
			}
		}(i)
	}

	// Concurrent message processing
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				metrics.RecordMessage("success", "direct", time.Millisecond, 1024, "schema")
				metrics.IncMessagesInFlight()
				metrics.DecMessagesInFlight()
			}
		}(i)
	}

	// Concurrent system metrics updates
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				metrics.SetConnectionsActive(float64(j))
				metrics.SetMemoryUsage(float64(j * 1024))
				metrics.SetGoroutinesActive(float64(j))
			}
		}(i)
	}

	wg.Wait()

	// Verify final state
	key := "GET:/test:200"
	expectedCount := int64(numGoroutines * numOperations)
	if count := metrics.httpRequests[key]; count != expectedCount {
		t.Errorf("Expected %d HTTP requests, got %d", expectedCount, count)
	}

	messageKey := "success:direct"
	if count := metrics.messages[messageKey]; count != expectedCount {
		t.Errorf("Expected %d messages, got %d", expectedCount, count)
	}

	// Verify in-flight counters are zero
	if count := atomic.LoadInt64(&metrics.httpInFlight); count != 0 {
		t.Errorf("Expected 0 in-flight HTTP requests, got %d", count)
	}
	if count := atomic.LoadInt64(&metrics.messagesInFlight); count != 0 {
		t.Errorf("Expected 0 in-flight messages, got %d", count)
	}
}

func TestSimpleMetrics_LastUpdateTracking(t *testing.T) {
	metrics := NewSimpleMetrics()
	initialUpdate := metrics.lastUpdate

	// Sleep briefly to ensure time difference
	time.Sleep(time.Millisecond)

	// Any operation should update lastUpdate
	metrics.RecordHTTPRequest("GET", "/test", 200, time.Millisecond)

	if !metrics.lastUpdate.After(initialUpdate) {
		t.Error("lastUpdate should be updated after recording HTTP request")
	}

	// Test other operations update lastUpdate
	prevUpdate := metrics.lastUpdate
	time.Sleep(time.Millisecond)

	metrics.RecordError("test", "error", "type")
	if !metrics.lastUpdate.After(prevUpdate) {
		t.Error("lastUpdate should be updated after recording error")
	}
}

// Benchmark tests
func BenchmarkSimpleMetrics_RecordHTTPRequest(b *testing.B) {
	metrics := NewSimpleMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordHTTPRequest("GET", "/test", 200, time.Millisecond)
	}
}

func BenchmarkSimpleMetrics_IncDecHTTPInFlight(b *testing.B) {
	metrics := NewSimpleMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.IncHTTPRequestsInFlight()
		metrics.DecHTTPRequestsInFlight()
	}
}

func BenchmarkSimpleMetrics_ToJSON(b *testing.B) {
	metrics := NewSimpleMetrics()

	// Add some test data
	for i := 0; i < 100; i++ {
		metrics.RecordHTTPRequest("GET", "/test", 200, time.Millisecond)
		metrics.RecordMessage("success", "direct", time.Millisecond, 1024, "schema")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := metrics.ToJSON()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSimpleMetrics_ConcurrentAccess(b *testing.B) {
	metrics := NewSimpleMetrics()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metrics.RecordHTTPRequest("GET", "/test", 200, time.Millisecond)
			metrics.IncHTTPRequestsInFlight()
			metrics.DecHTTPRequestsInFlight()
		}
	})
}
