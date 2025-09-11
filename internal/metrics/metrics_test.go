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
	"testing"
	"time"
)

func TestNewMetricsProvider(t *testing.T) {
	provider := NewMetricsProvider()

	if provider == nil {
		t.Fatal("NewMetricsProvider() returned nil")
	}

	// Verify it returns a SimpleMetrics instance
	if _, ok := provider.(*SimpleMetrics); !ok {
		t.Errorf("NewMetricsProvider() should return *SimpleMetrics, got %T", provider)
	}
}

func TestNewTimer(t *testing.T) {
	timer := NewTimer()

	if timer == nil {
		t.Fatal("NewTimer() returned nil")
	}

	if timer.start.IsZero() {
		t.Error("Timer start time should not be zero")
	}

	// Verify start time is recent (within last second)
	if time.Since(timer.start) > time.Second {
		t.Error("Timer start time should be recent")
	}
}

func TestTimer_Duration(t *testing.T) {
	timer := NewTimer()

	// Sleep for a small duration to test timing
	sleepDuration := 10 * time.Millisecond
	time.Sleep(sleepDuration)

	duration := timer.Duration()

	// Duration should be at least the sleep duration
	if duration < sleepDuration {
		t.Errorf("Timer duration %v should be at least %v", duration, sleepDuration)
	}

	// Duration should be reasonable (less than 1 second for this test)
	if duration > time.Second {
		t.Errorf("Timer duration %v seems too long for this test", duration)
	}
}

func TestTimer_MultipleDurationCalls(t *testing.T) {
	timer := NewTimer()

	// First duration call
	time.Sleep(5 * time.Millisecond)
	duration1 := timer.Duration()

	// Second duration call after more time
	time.Sleep(5 * time.Millisecond)
	duration2 := timer.Duration()

	// Second duration should be longer than first
	if duration2 <= duration1 {
		t.Errorf("Second duration %v should be longer than first %v", duration2, duration1)
	}
}

func TestTimer_Precision(t *testing.T) {
	timer := NewTimer()

	// Get duration immediately
	duration := timer.Duration()

	// Duration should be very small but not zero
	if duration < 0 {
		t.Error("Timer duration should not be negative")
	}

	// Duration should be less than 1 millisecond for immediate call
	if duration > time.Millisecond {
		t.Errorf("Immediate timer duration %v should be very small", duration)
	}
}

// Benchmark tests for Timer performance
func BenchmarkNewTimer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewTimer()
	}
}

func BenchmarkTimer_Duration(b *testing.B) {
	timer := NewTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		timer.Duration()
	}
}
