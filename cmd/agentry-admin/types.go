/*
 * Copyright 2026 Cong Wang
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

package main

import (
	"encoding/json"
	"time"
)

// API request/response structures
type RegisterSchemaRequest struct {
	ID         string          `json:"id"`
	Definition json.RawMessage `json:"definition"`
	Force      bool            `json:"force,omitempty"`
}

type SchemaResponse struct {
	Message   string    `json:"message,omitempty"`
	SchemaID  string    `json:"schema_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

type SchemaIdentifier struct {
	Domain  string `json:"domain"`
	Entity  string `json:"entity"`
	Version string `json:"version"`
	Raw     string `json:"raw"`
}

type ListSchemasResponse struct {
	Schemas   []SchemaIdentifier `json:"schemas"`
	Count     int                `json:"count"`
	Timestamp time.Time          `json:"timestamp"`
}

type ValidatePayloadRequest struct {
	Payload json.RawMessage `json:"payload"`
}

type ValidationResponse struct {
	Valid     bool                     `json:"valid"`
	Errors    []map[string]interface{} `json:"errors"`
	Warnings  []map[string]interface{} `json:"warnings"`
	Timestamp time.Time                `json:"timestamp"`
}

type SchemaStatsResponse struct {
	Stats     map[string]interface{} `json:"stats"`
	Timestamp time.Time              `json:"timestamp"`
}

// Agent management structures
type LocalAgent struct {
	Address          string            `json:"address"`
	DeliveryMode     string            `json:"delivery_mode"`
	PushTarget       string            `json:"push_target"`
	Headers          map[string]string `json:"headers"`
	APIKey           string            `json:"api_key"`
	SupportedSchemas []string          `json:"supported_schemas"`
	RequiresSchema   bool              `json:"requires_schema"` // whether this agent requires schema validation
	CreatedAt        time.Time         `json:"created_at"`
	LastAccess       time.Time         `json:"last_access"`
}

type AgentResponse struct {
	Message   string      `json:"message,omitempty"`
	Agent     *LocalAgent `json:"agent,omitempty"`
	Address   string      `json:"address,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Error     string      `json:"error,omitempty"`
}

type ListAgentsResponse struct {
	Agents    map[string]*LocalAgent `json:"agents"`
	Count     int                    `json:"count"`
	Timestamp time.Time              `json:"timestamp"`
}

type Message struct {
	Version        string                 `json:"version"`
	MessageID      string                 `json:"message_id"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Timestamp      time.Time              `json:"timestamp"`
	Sender         string                 `json:"sender"`
	Recipients     []string               `json:"recipients"`
	Subject        string                 `json:"subject"`
	Payload        map[string]interface{} `json:"payload"`
}

type InboxResponse struct {
	Recipient string     `json:"recipient"`
	Messages  []*Message `json:"messages"`
	Count     int        `json:"count"`
	Timestamp time.Time  `json:"timestamp"`
}

type AckResponse struct {
	Message   string    `json:"message"`
	Recipient string    `json:"recipient"`
	MessageID string    `json:"message_id"`
	Timestamp time.Time `json:"timestamp"`
}
