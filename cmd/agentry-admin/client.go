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

func makeAdminAPIRequest(method, endpoint string, body interface{}) ([]byte, error) {
	// Check if admin key file is provided
	if adminKeyFile == "" {
		return nil, fmt.Errorf("admin key file is required for administrative operations. Use --admin-key-file flag")
	}

	// Read admin key from file
	adminKeyBytes, err := os.ReadFile(adminKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read admin key file: %w", err)
	}
	adminKey := strings.TrimSpace(string(adminKeyBytes))

	if adminKey == "" {
		return nil, fmt.Errorf("admin key file is empty")
	}

	url := strings.TrimRight(gatewayURL, "/") + endpoint

	if verbose {
		fmt.Printf("Making admin %s request to: %s\n", method, url)
	}

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)

		if verbose {
			fmt.Printf("Request body: %s\n", string(jsonData))
		}
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add admin authentication header
	req.Header.Set("X-Admin-Key", adminKey)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if verbose {
		fmt.Printf("Response status: %d\n", resp.StatusCode)
		fmt.Printf("Response body: %s\n", string(respBody))
	}

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

func makeAuthenticatedAPIRequest(method, endpoint string, body interface{}, apiKey string) ([]byte, error) {
	url := strings.TrimRight(gatewayURL, "/") + endpoint

	if verbose {
		fmt.Printf("Making authenticated %s request to: %s\n", method, url)
	}

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)

		if verbose {
			fmt.Printf("Request body: %s\n", string(jsonData))
		}
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add authentication header
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if verbose {
		fmt.Printf("Response status: %d\n", resp.StatusCode)
		fmt.Printf("Response body: %s\n", string(respBody))
	}

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
