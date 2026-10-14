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

package signing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// DecodeBody parses a raw request body into a map, enforcing the I-JSON
// constraints the signature profile depends on:
//
//   - the body is exactly one JSON object (no trailing data),
//   - no duplicate member names anywhere in the document,
//   - no two members of the same object differ only in case,
//   - all numbers are within IEEE 754 double precision range.
//
// The case rule exists because encoding/json binds object members to struct
// fields case-insensitively and lets the last one win: with "sender" and
// "Sender" both present, the verifier (which reads the exact key) would
// check one sender while the handler stored the other. Rejecting the
// ambiguity outright is the only way both readers are guaranteed to agree.
//
// encoding/json silently keeps the last of duplicate members and accepts
// trailing data, so this decoder walks the token stream itself. Numbers are
// returned as float64 (the type the JCS formatter consumes), which means a
// literal carrying more precision than a double holds canonicalizes to the
// double's value — the behavior RFC 8785 defines.
func DecodeBody(raw []byte) (map[string]interface{}, error) {
	doc, err := decodeStrict(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedBody, err)
	}
	m, ok := doc.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%w: top-level value is not an object", ErrMalformedBody)
	}
	if err := checkNumbers(m); err != nil {
		return nil, err
	}
	return convertNumbers(m), nil
}

// decodeStrict decodes exactly one JSON value from raw, rejecting duplicate
// member names (including members differing only in case) at any depth and
// any trailing data after the value. Numbers are left as json.Number for the
// caller to convert.
func decodeStrict(raw []byte) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	doc, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	// A further token means trailing JSON or garbage followed the value.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return doc, nil
}

// decodeValue decodes one JSON value from the token stream, rejecting
// duplicate object member names at any depth. json.Decoder.Token unescapes
// string keys, so "\u0061" and "a" correctly collide as duplicates.
func decodeValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, number, bool, or nil
	}
	switch delim {
	case '{':
		m := make(map[string]interface{})
		folded := make(map[string]string, 0)
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string: %v", keyTok)
			}
			if _, dup := m[key]; dup {
				return nil, fmt.Errorf("duplicate member %q", key)
			}
			// Members differing only in case are ambiguous: struct
			// binding is case-insensitive while the verifier reads
			// exact keys. See DecodeBody.
			fold := strings.ToLower(key)
			if prev, dup := folded[fold]; dup {
				return nil, fmt.Errorf("members %q and %q differ only in case", prev, key)
			}
			folded[fold] = key
			val, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			m[key] = val
		}
		if _, err := dec.Token(); err != nil { // consume closing '}'
			return nil, err
		}
		return m, nil
	case '[':
		arr := make([]interface{}, 0)
		for dec.More() {
			val, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil { // consume closing ']'
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", delim)
	}
}

// checkNumbers rejects numbers outside IEEE 754 double range (e.g. 1e400):
// their canonical form is undefined and cross-language verifiers would
// disagree. Subnormal underflow (1e-400 → 0) is in range and canonicalizes
// to "0" per RFC 8785.
func checkNumbers(v interface{}) error {
	switch t := v.(type) {
	case map[string]interface{}:
		for _, val := range t {
			if err := checkNumbers(val); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, val := range t {
			if err := checkNumbers(val); err != nil {
				return err
			}
		}
	case json.Number:
		if _, err := t.Float64(); err != nil {
			return fmt.Errorf("%w: number %s out of double range", ErrMalformedBody, t.String())
		}
	}
	return nil
}

// convertNumbers rewrites json.Number values to float64 throughout the tree
// so the result matches what a plain json.Unmarshal into interface{} would
// produce (the input types the JCS formatter supports).
func convertNumbers(m map[string]interface{}) map[string]interface{} {
	convertValue(m)
	return m
}

// convertValue rewrites every json.Number in the tree rooted at v to float64
// and returns the converted value.
//
// Callers must assign the result back: the recursion visits interface values,
// so a replacement inside an array (or at a scalar root) is only observable
// through the return value. Recursing into arrays without writing back used
// to leave numeric array elements as json.Number — a type the JCS formatter
// rejects — so payloads such as [1,2] could be neither signed nor verified.
func convertValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			t[k] = convertValue(val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = convertValue(val)
		}
		return t
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t
	default:
		return v
	}
}

// convertTree converts a json.Number tree rooted at any value type (object,
// array, or scalar) and returns the converted value.
func convertTree(v interface{}) interface{} {
	return convertValue(v)
}

// ExtractSenderDomain returns the normalized domain of the body's "sender"
// member. It is the verification-side counterpart of the handler's address
// validation: by the time verification runs, structure validation has
// already accepted the sender, so failures here are defensive.
func ExtractSenderDomain(body map[string]interface{}) (string, error) {
	senderRaw, ok := body[memberSender]
	if !ok {
		return "", fmt.Errorf("%w: missing sender", ErrMalformedBody)
	}
	sender, ok := senderRaw.(string)
	if !ok {
		return "", fmt.Errorf("%w: sender is not a string", ErrMalformedBody)
	}
	at := strings.LastIndex(sender, "@")
	if at < 0 || at == len(sender)-1 {
		return "", fmt.Errorf("%w: sender %q has no domain", ErrMalformedBody, sender)
	}
	return NormalizeDomain(sender[at+1:])
}
