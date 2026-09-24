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
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// Wire field names, in one place, so the builder and the parity test cannot
// drift from the protocol.
const (
	wireVersion         = "version"
	wireMessageID       = "message_id"
	wireIdempotencyKey  = "idempotency_key"
	wireTimestamp       = "timestamp"
	wireSender          = "sender"
	wireRecipients      = "recipients"
	wireSubject         = "subject"
	wireSchema          = "schema"
	wireCoordination    = "coordination"
	wireHeaders         = "headers"
	wirePayload         = "payload"
	wireAttachments     = "attachments"
	wireInReplyTo       = "in_reply_to"
	wireResponseType    = "response_type"
	wireWorkflowID      = "workflow_id"
	wireSignature       = "signature"
	timestampFormatWire = time.RFC3339
)

// BuildWireBody constructs the final single-recipient wire body for message
// addressed to recipient, as the value tree that is canonicalized and
// signed. It includes every protocol field — notably workflow_id, which the
// legacy delivery path omitted — and refuses a message that already carries
// a signature (relay re-signing is not supported).
//
// Field policy mirrors the protocol's conformance vectors: the core fields
// (including workflow_id, even when empty) are always present; the optional
// metadata fields (in_reply_to, response_type, attachments, coordination,
// headers) are present only when set. The returned map uses float64 numbers
// and jcs-compatible slice types, with no "signature" member, ready for
// Signer.SignBody.
func BuildWireBody(message *types.Message, recipient string) (map[string]interface{}, error) {
	if message == nil {
		return nil, fmt.Errorf("signing: nil message")
	}
	if message.Signature != nil {
		return nil, fmt.Errorf("signing: message already carries a signature")
	}
	body := map[string]interface{}{
		wireVersion:        message.Version,
		wireMessageID:      message.MessageID,
		wireIdempotencyKey: message.IdempotencyKey,
		wireTimestamp:      message.Timestamp.UTC().Format(timestampFormatWire),
		wireSender:         message.Sender,
		wireRecipients:     []interface{}{recipient},
		wireSubject:        message.Subject,
		wireSchema:         message.Schema,
		wirePayload:        nil,
		wireWorkflowID:     message.WorkflowID,
	}
	if message.InReplyTo != "" {
		body[wireInReplyTo] = message.InReplyTo
	}
	if message.ResponseType != "" {
		body[wireResponseType] = message.ResponseType
	}
	if atts := attachmentsToWire(message.Attachments); atts != nil {
		body[wireAttachments] = atts
	}
	if message.Coordination != nil {
		body[wireCoordination] = coordinationToWire(message.Coordination)
	}
	if message.Headers != nil {
		body[wireHeaders] = message.Headers
	}
	if len(message.Payload) > 0 {
		payload, err := decodeRawJSON(message.Payload)
		if err != nil {
			return nil, fmt.Errorf("signing: payload is not I-JSON: %w", err)
		}
		body[wirePayload] = payload
	}
	return body, nil
}

// AttachWireBody inserts a signature member into a wire body.
func AttachWireBody(body map[string]interface{}, sig *Signature) (map[string]interface{}, error) {
	if _, exists := body[wireSignature]; exists {
		return nil, ErrAlreadySigned
	}
	out := make(map[string]interface{}, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	out[wireSignature] = sig.ToMap()
	return out, nil
}

// MarshalWireBody serializes a wire body to the exact bytes sent over HTTP.
// The signature, if present, was computed over the JCS canonical form of
// this same value tree, so any serializer is safe — but we use standard
// JSON for determinism and readability.
func MarshalWireBody(body map[string]interface{}) ([]byte, error) {
	return json.Marshal(body)
}

// decodeRawJSON converts a json.RawMessage into the generic value tree the
// JCS formatter consumes. It rejects numbers outside double range (e.g.
// 1e400) — RawMessage preserves the literal, but the canonical form of such
// a number is undefined, so a body containing one cannot be signed.
func decodeRawJSON(raw json.RawMessage) (interface{}, error) {
	var v interface{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after payload JSON")
	}
	if err := checkNumbers(v); err != nil {
		return nil, err
	}
	return convertTree(v), nil
}

// attachmentsToWire converts typed attachments to the generic tree.
func attachmentsToWire(atts []types.Attachment) interface{} {
	if len(atts) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(atts))
	for _, a := range atts {
		out = append(out, map[string]interface{}{
			"filename":     a.Filename,
			"content_type": a.ContentType,
			"size":         float64(a.Size),
			"hash":         a.Hash,
			"url":          a.URL,
		})
	}
	return out
}

// coordinationToWire converts a CoordinationConfig to the generic tree.
func coordinationToWire(c *types.CoordinationConfig) interface{} {
	m := map[string]interface{}{
		"type":    c.Type,
		"timeout": float64(c.Timeout),
	}
	if len(c.RequiredResponses) > 0 {
		m["required_responses"] = stringSliceToWire(c.RequiredResponses)
	}
	if len(c.OptionalResponses) > 0 {
		m["optional_responses"] = stringSliceToWire(c.OptionalResponses)
	}
	if len(c.Sequence) > 0 {
		m["sequence"] = stringSliceToWire(c.Sequence)
	}
	if c.StopOnFailure {
		m["stop_on_failure"] = true
	}
	if len(c.Conditions) > 0 {
		conds := make([]interface{}, 0, len(c.Conditions))
		for _, rule := range c.Conditions {
			conds = append(conds, map[string]interface{}{
				"if":   rule.If,
				"then": stringSliceToWire(rule.Then),
				"else": stringSliceToWire(rule.Else),
			})
		}
		m["conditions"] = conds
	}
	return m
}

func stringSliceToWire(s []string) interface{} {
	out := make([]interface{}, 0, len(s))
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
