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
	"errors"
	"strings"
	"testing"
)

func TestInboxGet_WithKeyFileSendsBearer(t *testing.T) {
	resp := `{"recipient":"u@localhost","count":1,"messages":[{"message_id":"m1","sender":"a@b","subject":"hi","payload":{"k":"v"}}]}`
	srv, cap := newMockGateway(t, 200, resp)
	keyFile := writeTempFile(t, "agent-secret\n")

	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"inbox", "get", "u@localhost", "--key-file", keyFile)
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if cap.Method != "GET" || cap.Path != "/v1/inbox/u@localhost" {
		t.Errorf("request = %s %s", cap.Method, cap.Path)
	}
	if got := cap.Header.Get("Authorization"); got != "Bearer agent-secret" {
		t.Errorf("Authorization = %q, want trimmed bearer token", got)
	}
	if !strings.Contains(stdout, "Inbox for u@localhost: 1 message(s)") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "ID: m1") || !strings.Contains(stdout, "From: a@b") {
		t.Errorf("stdout missing message fields: %q", stdout)
	}
}

func TestInboxGet_KeyViaFlag(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{"recipient":"u@localhost","count":0,"messages":[]}`)
	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"inbox", "get", "u@localhost", "--key", "raw-key")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if got := cap.Header.Get("Authorization"); got != "Bearer raw-key" {
		t.Errorf("Authorization = %q", got)
	}
	if !strings.Contains(stdout, "No messages") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestInboxGet_MissingKey(t *testing.T) {
	_, stderr, err := runCLI(t, "http://127.0.0.1:0", nil, "inbox", "get", "u@localhost")
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
	if !strings.Contains(stderr, "API key is required") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestInboxAck_Success(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{"message":"ok","recipient":"u@localhost","message_id":"m1"}`)
	stdout, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"inbox", "ack", "u@localhost", "m1", "--key", "raw-key")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if cap.Method != "DELETE" || cap.Path != "/v1/inbox/u@localhost/m1" {
		t.Errorf("request = %s %s", cap.Method, cap.Path)
	}
	if !strings.Contains(stdout, "Successfully acknowledged message: m1") {
		t.Errorf("stdout = %q", stdout)
	}
}
