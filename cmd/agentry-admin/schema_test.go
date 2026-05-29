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
	"errors"
	"strings"
	"testing"
)

func TestSchemaRegister_Success(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{"message":"ok","schema_id":"agntcy:commerce.order.v1"}`)
	keyFile := writeTempFile(t, "admin-key")
	schemaFile := writeTempFile(t, `{"type":"object"}`)

	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"--admin-key-file", keyFile,
		"schema", "register", "agntcy:commerce.order.v1", "-f", schemaFile, "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Successfully registered schema: agntcy:commerce.order.v1") {
		t.Errorf("stdout = %q", stdout)
	}

	if cap.Method != "POST" || cap.Path != "/v1/admin/schemas" {
		t.Errorf("request = %s %s", cap.Method, cap.Path)
	}
	var req RegisterSchemaRequest
	if e := json.Unmarshal(cap.Body, &req); e != nil {
		t.Fatalf("decode request body: %v", e)
	}
	if req.ID != "agntcy:commerce.order.v1" {
		t.Errorf("request id = %q", req.ID)
	}
	if !req.Force {
		t.Errorf("force not propagated to request")
	}
	if strings.TrimSpace(string(req.Definition)) != `{"type":"object"}` {
		t.Errorf("definition = %q", req.Definition)
	}
}

func TestSchemaRegister_MissingFileFlag(t *testing.T) {
	keyFile := writeTempFile(t, "admin-key")
	// No server should be hit; use an unreachable URL to prove that.
	stdout, stderr, err := runCLI(t, "http://127.0.0.1:0", nil,
		"--admin-key-file", keyFile,
		"schema", "register", "agntcy:commerce.order.v1")
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
	if !strings.Contains(stderr, "Schema file is required") {
		t.Errorf("stderr = %q", stderr)
	}
	// Usage is also dumped after the validation error. cobra's Usage() writes
	// to the out writer (in production both out and err resolve to stderr).
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("expected usage dump, got stdout %q", stdout)
	}
}

func TestSchemaList_Success(t *testing.T) {
	resp := `{"count":2,"schemas":[{"raw":"agntcy:commerce.order.v1"},{"domain":"crm","entity":"lead","version":"v1"}]}`
	srv, cap := newMockGateway(t, 200, resp)
	keyFile := writeTempFile(t, "admin-key")

	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"--admin-key-file", keyFile, "schema", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if cap.Method != "GET" || cap.Path != "/v1/admin/schemas" {
		t.Errorf("request = %s %s", cap.Method, cap.Path)
	}
	if !strings.Contains(stdout, "Found 2 schema(s):") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "agntcy:commerce.order.v1") || !strings.Contains(stdout, "agntcy:crm.lead.v1") {
		t.Errorf("stdout missing schemas: %q", stdout)
	}
}

func TestSchemaValidate_InvalidPayloadExits(t *testing.T) {
	resp := `{"valid":false,"errors":[{"field":"x"}]}`
	srv, _ := newMockGateway(t, 200, resp)
	keyFile := writeTempFile(t, "admin-key")
	payload := writeTempFile(t, `{"a":1}`)

	stdout, _, err := runCLI(t, srv.URL, srv.Client(),
		"--admin-key-file", keyFile,
		"schema", "validate", "agntcy:commerce.order.v1", "-f", payload)
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit for invalid payload", err)
	}
	if !strings.Contains(stdout, "✗ Payload is invalid") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestSchemaCommand_RequiresAdminKey(t *testing.T) {
	// No admin key file: AdminRequest fails before any network call.
	stdout, stderr, err := runCLI(t, "http://127.0.0.1:0", nil, "schema", "list")
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
	if !strings.Contains(stderr, "admin key file is required") {
		t.Errorf("stderr = %q (stdout %q)", stderr, stdout)
	}
}
