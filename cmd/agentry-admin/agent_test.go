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

func TestAgentRegister_PullWithSchemas(t *testing.T) {
	resp := `{"agent":{"address":"sales@localhost","api_key":"ABCDEFGH1234","delivery_mode":"pull"}}`
	srv, cap := newMockGateway(t, 200, resp)
	keyFile := writeTempFile(t, "admin-key")

	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"--admin-key-file", keyFile,
		"agent", "register", "sales", "--mode", "pull",
		"--schema", "agntcy:commerce.*", "--schema", "agntcy:crm.lead.v1")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}

	var sent LocalAgent
	if e := json.Unmarshal(cap.Body, &sent); e != nil {
		t.Fatalf("decode request body: %v", e)
	}
	if sent.Address != "sales" || sent.DeliveryMode != "pull" {
		t.Errorf("sent = %+v", sent)
	}
	want := []string{"agntcy:commerce.*", "agntcy:crm.lead.v1"}
	if strings.Join(sent.SupportedSchemas, ",") != strings.Join(want, ",") {
		t.Errorf("supported_schemas = %v, want %v", sent.SupportedSchemas, want)
	}
	if !strings.Contains(stdout, "Successfully registered agent: sales@localhost") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "API Key: ABCDEFGH1234") {
		t.Errorf("stdout missing api key: %q", stdout)
	}
}

func TestAgentRegister_PushHeadersParsed(t *testing.T) {
	resp := `{"agent":{"address":"bot@localhost","delivery_mode":"push","push_target":"http://webhook:8080"}}`
	srv, cap := newMockGateway(t, 200, resp)
	keyFile := writeTempFile(t, "admin-key")

	_, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"--admin-key-file", keyFile,
		"agent", "register", "bot", "--mode", "push", "--target", "http://webhook:8080",
		"--header", "Auth=Bearer token", "--header", "X-Env=prod")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	var sent LocalAgent
	if e := json.Unmarshal(cap.Body, &sent); e != nil {
		t.Fatalf("decode request body: %v", e)
	}
	if sent.Headers["Auth"] != "Bearer token" || sent.Headers["X-Env"] != "prod" {
		t.Errorf("headers = %v", sent.Headers)
	}
}

func TestAgentRegister_InvalidMode(t *testing.T) {
	keyFile := writeTempFile(t, "admin-key")
	_, stderr, err := runCLI(t, "http://127.0.0.1:0", nil,
		"--admin-key-file", keyFile,
		"agent", "register", "x", "--mode", "sideways")
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
	if !strings.Contains(stderr, "Delivery mode must be 'push' or 'pull'") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAgentRegister_PushRequiresTarget(t *testing.T) {
	keyFile := writeTempFile(t, "admin-key")
	_, stderr, err := runCLI(t, "http://127.0.0.1:0", nil,
		"--admin-key-file", keyFile,
		"agent", "register", "x", "--mode", "push")
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
	if !strings.Contains(stderr, "Push target URL is required for push mode") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAgentUnregister_RejectsFullAddress(t *testing.T) {
	keyFile := writeTempFile(t, "admin-key")
	_, stderr, err := runCLI(t, "http://127.0.0.1:0", nil,
		"--admin-key-file", keyFile,
		"agent", "unregister", "user@localhost")
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
	if !strings.Contains(stderr, "Only agent names are allowed") || !strings.Contains(stderr, "Use 'user' instead of 'user@localhost'") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAgentList_Empty(t *testing.T) {
	srv, _ := newMockGateway(t, 200, `{"count":0,"agents":{}}`)
	keyFile := writeTempFile(t, "admin-key")
	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"--admin-key-file", keyFile, "agent", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Found 0 agent(s):") || !strings.Contains(stdout, "No agents registered") {
		t.Errorf("stdout = %q", stdout)
	}
}
