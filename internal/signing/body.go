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
//   - all numbers are within IEEE 754 double precision range.
//
// encoding/json silently keeps the last of duplicate members and accepts
// trailing data, so this decoder walks the token stream itself. Numbers are
// returned as float64 (the type the JCS formatter consumes), which means a
// literal carrying more precision than a double holds canonicalizes to the
// double's value — the behavior RFC 8785 defines.
func DecodeBody(raw []byte) (map[string]interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	doc, err := decodeValue(dec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedBody, err)
	}
	// A further token means trailing JSON or garbage followed the value.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing data after JSON value", ErrMalformedBody)
	}
	m, ok := doc.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%w: top-level value is not an object", ErrMalformedBody)
	}
	if err := checkNumbers(doc); err != nil {
		return nil, err
	}
	return convertNumbers(m), nil
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

func convertValue(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if n, ok := val.(json.Number); ok {
				if f, err := n.Float64(); err == nil {
					t[k] = f
				}
			} else {
				convertValue(val)
			}
		}
	case []interface{}:
		for _, val := range t {
			convertValue(val)
		}
	}
}

// convertTree converts a json.Number tree rooted at any value type (object,
// array, or scalar) and returns the converted value.
func convertTree(v interface{}) interface{} {
	convertValue(v)
	return v
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
