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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client talks to an Agentry gateway's admin and inbox APIs. Its configuration
// is populated from the root command's persistent flags; the HTTP client and
// verbose output sink are injectable so the commands can be exercised in tests.
type Client struct {
	GatewayURL   string
	AdminKeyFile string
	Verbose      bool
	HTTP         *http.Client
	Out          io.Writer
}

// newClient returns a Client with production defaults: a 30s HTTP timeout and
// verbose diagnostics written to stdout.
func newClient() *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 30 * time.Second},
		Out:  os.Stdout,
	}
}

func (c *Client) logf(format string, args ...interface{}) {
	if c.Verbose {
		fmt.Fprintf(c.Out, format, args...)
	}
}

// AdminRequest performs an admin-authenticated request, reading the admin key
// from the configured key file and sending it in the X-Admin-Key header.
func (c *Client) AdminRequest(method, endpoint string, body interface{}) ([]byte, error) {
	if c.AdminKeyFile == "" {
		return nil, fmt.Errorf("admin key file is required for administrative operations. Use --admin-key-file flag")
	}

	adminKeyBytes, err := os.ReadFile(c.AdminKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read admin key file: %w", err)
	}
	adminKey := strings.TrimSpace(string(adminKeyBytes))

	if adminKey == "" {
		return nil, fmt.Errorf("admin key file is empty")
	}

	return c.do("admin", method, endpoint, body, func(req *http.Request) {
		req.Header.Set("X-Admin-Key", adminKey)
	})
}

// AuthenticatedRequest performs a request authenticated with an agent API key
// sent as a bearer token.
func (c *Client) AuthenticatedRequest(method, endpoint string, body interface{}, apiKey string) ([]byte, error) {
	return c.do("authenticated", method, endpoint, body, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	})
}

// do builds, sends, and reads a single request. kind labels the request in
// verbose output ("admin"/"authenticated"); auth sets the relevant auth header.
func (c *Client) do(kind, method, endpoint string, body interface{}, auth func(*http.Request)) ([]byte, error) {
	url := strings.TrimRight(c.GatewayURL, "/") + endpoint

	c.logf("Making %s %s request to: %s\n", kind, method, url)

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)

		c.logf("Request body: %s\n", string(jsonData))
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	auth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	c.logf("Response status: %d\n", resp.StatusCode)
	c.logf("Response body: %s\n", string(respBody))

	if resp.StatusCode >= 400 {
		// Try to parse error response
		var errorResp map[string]interface{}
		if json.Unmarshal(respBody, &errorResp) == nil {
			if msg, ok := errorResp["message"].(string); ok {
				return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, msg)
			}
		}
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
