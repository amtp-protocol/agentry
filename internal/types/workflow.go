/*
 * Copyright 2026 Sen Wang
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

package types

import (
	"encoding/json"
	"time"
)

// WorkflowStatus represents the execution state of a workflow
type WorkflowStatus string

const (
	WorkflowStatusPending    WorkflowStatus = "pending"
	WorkflowStatusInProgress WorkflowStatus = "in_progress"
	WorkflowStatusCompleted  WorkflowStatus = "completed"
	WorkflowStatusFailed     WorkflowStatus = "failed"
	WorkflowStatusTimeout    WorkflowStatus = "timeout"
)

// ParticipantStatus represents the state of a single participant in a workflow
type ParticipantStatus string

const (
	ParticipantStatusPending   ParticipantStatus = "pending"
	ParticipantStatusCompleted ParticipantStatus = "completed"
	ParticipantStatusFailed    ParticipantStatus = "failed"
	ParticipantStatusTimeout   ParticipantStatus = "timeout"
)

// Workflow represents the overarching state of a multi-agent coordination workflow
type Workflow struct {
	WorkflowID       string                `json:"workflow_id"`
	Status           WorkflowStatus        `json:"status"`
	CoordinationType string                `json:"coordination_type"`
	TimeoutSeconds   int                   `json:"timeout_seconds"`
	Participants     []WorkflowParticipant `json:"participants"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

// WorkflowParticipant tracks the state and response of a participant in a workflow
type WorkflowParticipant struct {
	WorkflowID      string            `json:"workflow_id"`
	Address         string            `json:"address"`
	Status          ParticipantStatus `json:"status"`
	ResponsePayload json.RawMessage   `json:"response_payload,omitempty"`
	Deadline        *time.Time        `json:"deadline,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}
