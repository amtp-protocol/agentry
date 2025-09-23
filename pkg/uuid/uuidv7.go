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
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"
)

// UUIDv7 generates a UUIDv7 (time-ordered UUID) according to the draft specification
// Format: xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx
// Where the first 48 bits are a Unix timestamp in milliseconds
func GenerateV7() (string, error) {
	var uuid [16]byte

	// Get current timestamp in milliseconds
	timestamp := time.Now().UnixMilli()

	// Set timestamp (first 48 bits)
	binary.BigEndian.PutUint32(uuid[0:4], uint32(timestamp>>16)) // #nosec G115 -- false positive
	binary.BigEndian.PutUint16(uuid[4:6], uint16(timestamp))     // #nosec G115 -- false positive

	// Generate random bytes for the rest
	if _, err := rand.Read(uuid[6:]); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Set version (4 bits) - version 7
	uuid[6] = (uuid[6] & 0x0f) | 0x70

	// Set variant (2 bits) - RFC 4122 variant
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	// Format as string
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(uuid[0:4]),
		binary.BigEndian.Uint16(uuid[4:6]),
		binary.BigEndian.Uint16(uuid[6:8]),
		binary.BigEndian.Uint16(uuid[8:10]),
		uuid[10:16]), nil
}

// GenerateV4 generates a standard UUIDv4 for idempotency keys
func GenerateV4() (string, error) {
	var uuid [16]byte

	// Generate random bytes
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Set version (4 bits) - version 4
	uuid[6] = (uuid[6] & 0x0f) | 0x40

	// Set variant (2 bits) - RFC 4122 variant
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	// Format as string
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(uuid[0:4]),
		binary.BigEndian.Uint16(uuid[4:6]),
		binary.BigEndian.Uint16(uuid[6:8]),
		binary.BigEndian.Uint16(uuid[8:10]),
		uuid[10:16]), nil
}

// IsValidV7 validates that a string is a valid UUIDv7
func IsValidV7(uuid string) bool {
	if len(uuid) != 36 {
		return false
	}

	// Check format: xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx
	if uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
		return false
	}

	// Check version (should be 7)
	if uuid[14] != '7' {
		return false
	}

	// Check variant (should be 8, 9, A, or B)
	variant := uuid[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'A' &&
		variant != 'b' && variant != 'B' {
		return false
	}

	return true
}

// IsValidV4 validates that a string is a valid UUIDv4
func IsValidV4(uuid string) bool {
	if len(uuid) != 36 {
		return false
	}

	// Check format: xxxxxxxx-xxxx-4xxx-xxxx-xxxxxxxxxxxx
	if uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
		return false
	}

	// Check version (should be 4)
	if uuid[14] != '4' {
		return false
	}

	// Check variant (should be 8, 9, A, or B)
	variant := uuid[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'A' &&
		variant != 'b' && variant != 'B' {
		return false
	}

	return true
}

// ExtractTimestamp extracts the timestamp from a UUIDv7
func ExtractTimestamp(uuid string) (time.Time, error) {
	if !IsValidV7(uuid) {
		return time.Time{}, fmt.Errorf("invalid UUIDv7 format")
	}

	// Extract the first 48 bits (timestamp in milliseconds)
	// Format: xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx
	// We need: xxxxxxxx-xxxx (first 12 hex chars, excluding hyphens)

	// Remove hyphens and get first 12 hex characters
	hexStr := ""
	for i, char := range uuid {
		if char != '-' {
			hexStr += string(char)
		}
		if len(hexStr) == 12 {
			break
		}
		if i >= 13 { // Safety check to avoid infinite loop
			break
		}
	}

	if len(hexStr) != 12 {
		return time.Time{}, fmt.Errorf("failed to extract timestamp hex string")
	}

	// Parse hex string to uint64
	var timestamp uint64
	for _, char := range hexStr {
		var digit uint64
		switch {
		case char >= '0' && char <= '9':
			digit = uint64(char - '0')
		case char >= 'a' && char <= 'f':
			digit = uint64(char - 'a' + 10)
		case char >= 'A' && char <= 'F':
			digit = uint64(char - 'A' + 10)
		default:
			return time.Time{}, fmt.Errorf("invalid hex character: %c", char)
		}

		timestamp = timestamp*16 + digit
	}

	return time.UnixMilli(int64(timestamp)), nil // #nosec G115 -- Never overflow
}
