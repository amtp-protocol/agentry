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
	"strings"
	"testing"
)

// newTestClient returns a Client wired to srv with the given admin key file.
func newTestClient(srvURL, adminKeyFile string) *Client {
	c := newClient()
	c.GatewayURL = srvURL
	c.AdminKeyFile = adminKeyFile
	return c
}

func TestAdminRequest_SetsKeyHeaderAndBuildsURL(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{"message":"ok"}`)
	keyFile := writeTempFile(t, "secret-admin-key")
	c := newTestClient(srv.URL, keyFile)
	c.HTTP = srv.Client()

	body, err := c.AdminRequest("POST", "/v1/admin/schemas", map[string]string{"id": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"message":"ok"}` {
		t.Errorf("body = %q", body)
	}
	if cap.Method != "POST" || cap.Path != "/v1/admin/schemas" {
		t.Errorf("got %s %s", cap.Method, cap.Path)
	}
	if got := cap.Header.Get("X-Admin-Key"); got != "secret-admin-key" {
		t.Errorf("X-Admin-Key = %q", got)
	}
	if got := cap.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q (want application/json for body request)", got)
	}
}

func TestAdminRequest_TrimsKeyFileWhitespace(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{}`)
	keyFile := writeTempFile(t, "  padded-key\n")
	c := newTestClient(srv.URL, keyFile)
	c.HTTP = srv.Client()

	if _, err := c.AdminRequest("GET", "/v1/admin/schemas", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cap.Header.Get("X-Admin-Key"); got != "padded-key" {
		t.Errorf("X-Admin-Key = %q, want trimmed", got)
	}
}

func TestAdminRequest_NoContentTypeWithoutBody(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{}`)
	keyFile := writeTempFile(t, "k")
	c := newTestClient(srv.URL, keyFile)
	c.HTTP = srv.Client()

	if _, err := c.AdminRequest("GET", "/v1/admin/schemas", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cap.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want empty for body-less request", got)
	}
}

func TestAdminRequest_MissingKeyFile(t *testing.T) {
	c := newClient()
	c.AdminKeyFile = ""
	_, err := c.AdminRequest("GET", "/v1/admin/schemas", nil)
	if err == nil || !strings.Contains(err.Error(), "admin key file is required") {
		t.Fatalf("err = %v, want 'admin key file is required'", err)
	}
}

func TestAdminRequest_EmptyKeyFile(t *testing.T) {
	keyFile := writeTempFile(t, "   \n")
	c := newClient()
	c.AdminKeyFile = keyFile
	_, err := c.AdminRequest("GET", "/v1/admin/schemas", nil)
	if err == nil || !strings.Contains(err.Error(), "admin key file is empty") {
		t.Fatalf("err = %v, want 'admin key file is empty'", err)
	}
}

func TestAdminRequest_APIErrorWithMessage(t *testing.T) {
	srv, _ := newMockGateway(t, 400, `{"message":"bad schema"}`)
	keyFile := writeTempFile(t, "k")
	c := newTestClient(srv.URL, keyFile)
	c.HTTP = srv.Client()

	_, err := c.AdminRequest("POST", "/v1/admin/schemas", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "API error (400): bad schema") {
		t.Fatalf("err = %v, want 'API error (400): bad schema'", err)
	}
}

func TestAdminRequest_APIErrorPlainBody(t *testing.T) {
	srv, _ := newMockGateway(t, 500, `internal boom`)
	keyFile := writeTempFile(t, "k")
	c := newTestClient(srv.URL, keyFile)
	c.HTTP = srv.Client()

	_, err := c.AdminRequest("GET", "/v1/admin/schemas", nil)
	if err == nil || !strings.Contains(err.Error(), "API error (500): internal boom") {
		t.Fatalf("err = %v, want 'API error (500): internal boom'", err)
	}
}

func TestAuthenticatedRequest_SetsBearerToken(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{}`)
	c := newClient()
	c.GatewayURL = srv.URL
	c.HTTP = srv.Client()

	if _, err := c.AuthenticatedRequest("GET", "/v1/inbox/u@localhost", nil, "agent-key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cap.Header.Get("Authorization"); got != "Bearer agent-key" {
		t.Errorf("Authorization = %q, want 'Bearer agent-key'", got)
	}
	if got := cap.Header.Get("X-Admin-Key"); got != "" {
		t.Errorf("X-Admin-Key = %q, want empty for authenticated request", got)
	}
}
