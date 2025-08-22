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

package uuid

import (
	"testing"
	"time"
)

func TestGenerateV7(t *testing.T) {
	uuid1, err := GenerateV7()
	if err != nil {
		t.Fatalf("Failed to generate UUIDv7: %v", err)
	}

	if len(uuid1) != 36 {
		t.Errorf("Expected UUID length 36, got %d", len(uuid1))
	}

	if !IsValidV7(uuid1) {
		t.Errorf("Generated UUID is not valid V7: %s", uuid1)
	}

	// Generate another UUID and ensure they're different
	uuid2, err := GenerateV7()
	if err != nil {
		t.Fatalf("Failed to generate second UUIDv7: %v", err)
	}

	if uuid1 == uuid2 {
		t.Error("Generated UUIDs should be unique")
	}
}

func TestGenerateV4(t *testing.T) {
	uuid1, err := GenerateV4()
	if err != nil {
		t.Fatalf("Failed to generate UUIDv4: %v", err)
	}

	if len(uuid1) != 36 {
		t.Errorf("Expected UUID length 36, got %d", len(uuid1))
	}

	if !IsValidV4(uuid1) {
		t.Errorf("Generated UUID is not valid V4: %s", uuid1)
	}

	// Generate another UUID and ensure they're different
	uuid2, err := GenerateV4()
	if err != nil {
		t.Fatalf("Failed to generate second UUIDv4: %v", err)
	}

	if uuid1 == uuid2 {
		t.Error("Generated UUIDs should be unique")
	}
}

func TestIsValidV7(t *testing.T) {
	tests := []struct {
		uuid  string
		valid bool
	}{
		{"01234567-89ab-7def-8123-456789abcdef", true},
		{"01234567-89ab-4def-8123-456789abcdef", false}, // version 4, not 7
		{"01234567-89ab-7def-0123-456789abcdef", false}, // invalid variant
		{"invalid-uuid", false},
		{"", false},
		{"01234567-89ab-7def-8123-456789abcde", false}, // too short
	}

	for _, test := range tests {
		result := IsValidV7(test.uuid)
		if result != test.valid {
			t.Errorf("IsValidV7(%s) = %v, expected %v", test.uuid, result, test.valid)
		}
	}
}

func TestIsValidV4(t *testing.T) {
	tests := []struct {
		uuid  string
		valid bool
	}{
		{"01234567-89ab-4def-8123-456789abcdef", true},
		{"01234567-89ab-7def-8123-456789abcdef", false}, // version 7, not 4
		{"01234567-89ab-4def-0123-456789abcdef", false}, // invalid variant
		{"invalid-uuid", false},
		{"", false},
		{"01234567-89ab-4def-8123-456789abcde", false}, // too short
	}

	for _, test := range tests {
		result := IsValidV4(test.uuid)
		if result != test.valid {
			t.Errorf("IsValidV4(%s) = %v, expected %v", test.uuid, result, test.valid)
		}
	}
}

func TestExtractTimestamp(t *testing.T) {
	// Generate a UUIDv7 and extract its timestamp
	uuid, err := GenerateV7()
	if err != nil {
		t.Fatalf("Failed to generate UUIDv7: %v", err)
	}

	timestamp, err := ExtractTimestamp(uuid)
	if err != nil {
		t.Fatalf("Failed to extract timestamp: %v", err)
	}

	// The timestamp should be close to now (within 1 second)
	now := time.Now()
	diff := now.Sub(timestamp)
	if diff < 0 {
		diff = -diff
	}

	if diff > time.Second {
		t.Errorf("Extracted timestamp %v is too far from now %v (diff: %v)", timestamp, now, diff)
	}
}

func TestExtractTimestampInvalid(t *testing.T) {
	_, err := ExtractTimestamp("invalid-uuid")
	if err == nil {
		t.Error("Expected error for invalid UUID")
	}

	_, err = ExtractTimestamp("01234567-89ab-4def-8123-456789abcdef") // V4, not V7
	if err == nil {
		t.Error("Expected error for V4 UUID")
	}
}

func BenchmarkGenerateV7(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GenerateV7()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateV4(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GenerateV4()
		if err != nil {
			b.Fatal(err)
		}
	}
}
