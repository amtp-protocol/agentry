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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// capturedRequest records what a mock gateway received, so tests can assert on
// the method, path, headers, and body the CLI produced.
type capturedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// newMockGateway starts an httptest server that records the last request into
// the returned capturedRequest and replies with the given status and body. The
// server is closed automatically when the test finishes.
func newMockGateway(t *testing.T, status int, respBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.Method = r.Method
		cap.Path = r.URL.Path
		cap.Header = r.Header.Clone()
		cap.Body = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// writeTempFile writes content to a fresh temp file and returns its path.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// runCLI builds the full command tree against a client pointing at gatewayURL,
// runs it with the given args, and returns captured stdout, stderr, and the
// Execute error. --gateway-url is injected automatically. If httpClient is nil
// the client's default is used. Verbose diagnostics are folded into stdout, as
// in production.
func runCLI(t *testing.T, gatewayURL string, httpClient *http.Client, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	out := &capWriter{}
	errOut := &capWriter{}

	c := newClient()
	if httpClient != nil {
		c.HTTP = httpClient
	}
	c.Out = out

	root := buildRootCmd(c)
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(append([]string{"--gateway-url", gatewayURL}, args...))

	err = root.Execute()
	return out.String(), errOut.String(), err
}

// capWriter is a minimal io.Writer that accumulates everything written to it.
type capWriter struct{ b []byte }

func (w *capWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

func (w *capWriter) String() string { return string(w.b) }
