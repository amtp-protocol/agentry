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

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	gatewayURL   = "http://localhost:8080"
	verbose      = false
	adminKeyFile = ""
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

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Parse global flags
	globalFlags := flag.NewFlagSet("global", flag.ContinueOnError)
	globalFlags.StringVar(&gatewayURL, "gateway-url", "http://localhost:8080", "Gateway URL")
	globalFlags.BoolVar(&verbose, "v", false, "Verbose output")
	globalFlags.BoolVar(&verbose, "verbose", false, "Verbose output")
	globalFlags.StringVar(&adminKeyFile, "admin-key-file", "", "Admin API key file for administrative operations")

	// Find where the command starts (after global flags)
	args := os.Args[1:]
	commandIndex := 0

	// Parse global flags until we hit a non-flag argument
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			commandIndex = i
			break
		}
		// Skip flag values
		if arg == "--gateway-url" && i+1 < len(args) {
			gatewayURL = args[i+1]
			i++ // Skip the value
		} else if arg == "--admin-key-file" && i+1 < len(args) {
			adminKeyFile = args[i+1]
			i++ // Skip the value
		} else if arg == "-v" || arg == "--verbose" {
			verbose = true
		}
	}

	if commandIndex >= len(args) {
		printUsage()
		os.Exit(1)
	}

	command := args[commandIndex]
	commandArgs := args[commandIndex+1:]

	switch command {
	case "schema":
		handleSchemaCommand(commandArgs)
	case "agent":
		handleAgentCommand(commandArgs)
	case "inbox":
		handleInboxCommand(commandArgs)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Agentry Admin Tool")
	fmt.Println("")
	fmt.Println("Usage: agentry-admin [global-flags] <command> [args]")
	fmt.Println("")
	fmt.Println("Global Flags:")
	fmt.Println("  --gateway-url <url>        Gateway URL (default: http://localhost:8080)")
	fmt.Println("  --admin-key-file <file>    Admin API key file for administrative operations")
	fmt.Println("  -v, --verbose             Verbose output")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  schema                    Schema management commands (requires admin key)")
	fmt.Println("    register <id> [flags]               Register a new schema")
	fmt.Println("    list                                List all schemas")
	fmt.Println("    get <id>                            Get a schema definition")
	fmt.Println("    delete <id>                         Delete a schema")
	fmt.Println("    validate <id> [flags]               Validate a payload against a schema")
	fmt.Println("    stats                               Show schema registry statistics")
	fmt.Println("")
	fmt.Println("  agent                     Local agent management commands (requires admin key)")
	fmt.Println("    register <name> [flags]             Register a local agent (name only, domain auto-added)")
	fmt.Println("    unregister <address>                Unregister a local agent")
	fmt.Println("    list                                List all registered agents")
	fmt.Println("")
	fmt.Println("  inbox                     Inbox management commands (requires agent API key)")
	fmt.Println("    get <recipient> [flags]             Get messages for recipient")
	fmt.Println("    ack <recipient> <message-id> [flags] Acknowledge/remove a message")
	fmt.Println("")
	fmt.Println("Schema Register Flags:")
	fmt.Println("  -f, --file <file>         Schema definition file (required)")
	fmt.Println("  --force                   Overwrite existing schema")
	fmt.Println("")
	fmt.Println("Schema Validate Flags:")
	fmt.Println("  -f, --file <file>         Payload file to validate (required)")
	fmt.Println("")
	fmt.Println("Agent Register Flags:")
	fmt.Println("  --mode <mode>             Delivery mode: 'push' or 'pull' (default: pull)")
	fmt.Println("  --target <url>            Push target URL (required for push mode)")
	fmt.Println("  --header <key=value>      Custom header (can be used multiple times)")
	fmt.Println("")
	fmt.Println("Inbox Flags:")
	fmt.Println("  --key <api-key>           Agent API key for authentication")
	fmt.Println("  --key-file <file>         File containing agent API key")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  # Admin operations (require admin key)")
	fmt.Println("  agentry-admin --admin-key-file admin.key schema register agntcy:commerce.order.v1 -f order-schema.json")
	fmt.Println("  agentry-admin --admin-key-file admin.key agent register user --mode pull")
	fmt.Println("  agentry-admin --admin-key-file admin.key agent register api-service --mode push --target http://api:8080/webhook")
	fmt.Println("  agentry-admin --admin-key-file admin.key agent list")
	fmt.Println("")
	fmt.Println("  # Agent inbox operations (require agent API key)")
	fmt.Println("  agentry-admin inbox get user@localhost --key-file user.key")
	fmt.Println("  agentry-admin inbox ack user@localhost message-id-123 --key-file user.key")
	fmt.Println("")
	fmt.Println("  # Schema statistics")
	fmt.Println("  agentry-admin --admin-key-file admin.key schema stats")
	fmt.Println("  agentry-admin --gateway-url http://gateway.example.com:8080 --admin-key-file admin.key schema list")
}

func handleSchemaCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Schema commands: register, list, get, delete, validate, stats")
		os.Exit(1)
	}

	subcommand := args[0]
	subcommandArgs := args[1:]

	switch subcommand {
	case "register":
		handleSchemaRegister(subcommandArgs)
	case "list":
		handleSchemaList(subcommandArgs)
	case "get":
		handleSchemaGet(subcommandArgs)
	case "delete":
		handleSchemaDelete(subcommandArgs)
	case "validate":
		handleSchemaValidate(subcommandArgs)
	case "stats":
		handleSchemaStats(subcommandArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown schema command: %s\n", subcommand)
		os.Exit(1)
	}
}

func handleSchemaRegister(args []string) {
	// Create flag set for register command
	registerFlags := flag.NewFlagSet("register", flag.ExitOnError)

	var schemaFile string
	var force bool

	registerFlags.StringVar(&schemaFile, "f", "", "Schema definition file (required)")
	registerFlags.StringVar(&schemaFile, "file", "", "Schema definition file (required)")
	registerFlags.BoolVar(&force, "force", false, "Overwrite existing schema")

	registerFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin schema register <schema-id> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		registerFlags.PrintDefaults()
	}

	if len(args) < 1 {
		registerFlags.Usage()
		os.Exit(1)
	}

	schemaID := args[0]

	// Parse flags starting from the second argument
	if err := registerFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	if schemaFile == "" {
		fmt.Fprintf(os.Stderr, "Error: Schema file is required (-f or --file flag)\n")
		registerFlags.Usage()
		os.Exit(1)
	}

	// Read schema file
	data, err := os.ReadFile(filepath.Clean(schemaFile))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read schema file: %v\n", err)
		os.Exit(1)
	}

	// Validate JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid JSON in schema file: %v\n", err)
		os.Exit(1)
	}

	// Create request
	req := RegisterSchemaRequest{
		ID:         schemaID,
		Definition: json.RawMessage(data),
		Force:      force,
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("POST", "/v1/admin/schemas", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register schema: %v\n", err)
		os.Exit(1)
	}

	var response SchemaResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if response.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", response.Error)
		os.Exit(1)
	}

	fmt.Printf("Successfully registered schema: %s\n", schemaID)
}

func handleSchemaList(args []string) {
	// Create flag set for list command (no flags currently, but ready for future)
	listFlags := flag.NewFlagSet("list", flag.ExitOnError)

	if err := listFlags.Parse(args); err != nil {
		os.Exit(1)
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("GET", "/v1/admin/schemas", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list schemas: %v\n", err)
		os.Exit(1)
	}

	var response ListSchemasResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d schema(s):\n\n", response.Count)
	for _, schema := range response.Schemas {
		if schema.Raw != "" {
			fmt.Printf("  %s\n", schema.Raw)
		} else {
			fmt.Printf("  agntcy:%s.%s.%s\n", schema.Domain, schema.Entity, schema.Version)
		}
	}
}

func handleSchemaGet(args []string) {
	// Create flag set for get command
	getFlags := flag.NewFlagSet("get", flag.ExitOnError)

	getFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin schema get <schema-id>\n")
	}

	if len(args) < 1 {
		getFlags.Usage()
		os.Exit(1)
	}

	schemaID := args[0]

	if err := getFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("GET", "/v1/admin/schemas/"+schemaID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get schema: %v\n", err)
		os.Exit(1)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	// Pretty print the schema
	prettyJSON, err := json.MarshalIndent(response["schema"], "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to format schema: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Schema: %s\n\n", schemaID)
	fmt.Println(string(prettyJSON))
}

func handleSchemaDelete(args []string) {
	// Create flag set for delete command
	deleteFlags := flag.NewFlagSet("delete", flag.ExitOnError)

	deleteFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin schema delete <schema-id>\n")
	}

	if len(args) < 1 {
		deleteFlags.Usage()
		os.Exit(1)
	}

	schemaID := args[0]

	if err := deleteFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("DELETE", "/v1/admin/schemas/"+schemaID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete schema: %v\n", err)
		os.Exit(1)
	}

	var response SchemaResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if response.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", response.Error)
		os.Exit(1)
	}

	fmt.Printf("Successfully deleted schema: %s\n", schemaID)
}

func handleSchemaValidate(args []string) {
	// Create flag set for validate command
	validateFlags := flag.NewFlagSet("validate", flag.ExitOnError)

	var payloadFile string

	validateFlags.StringVar(&payloadFile, "f", "", "Payload file to validate (required)")
	validateFlags.StringVar(&payloadFile, "file", "", "Payload file to validate (required)")

	validateFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin schema validate <schema-id> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		validateFlags.PrintDefaults()
	}

	if len(args) < 1 {
		validateFlags.Usage()
		os.Exit(1)
	}

	schemaID := args[0]

	// Parse flags starting from the second argument
	if err := validateFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	if payloadFile == "" {
		fmt.Fprintf(os.Stderr, "Error: Payload file is required (-f or --file flag)\n")
		validateFlags.Usage()
		os.Exit(1)
	}

	// Read payload file
	data, err := os.ReadFile(filepath.Clean(payloadFile))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read payload file: %v\n", err)
		os.Exit(1)
	}

	// Validate JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid JSON in payload file: %v\n", err)
		os.Exit(1)
	}

	// Create request
	req := ValidatePayloadRequest{
		Payload: json.RawMessage(data),
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("POST", "/v1/admin/schemas/"+schemaID+"/validate", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to validate payload: %v\n", err)
		os.Exit(1)
	}

	var response ValidationResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if response.Valid {
		fmt.Printf("✓ Payload is valid against schema: %s\n", schemaID)
	} else {
		fmt.Printf("✗ Payload is invalid against schema: %s\n", schemaID)
		if len(response.Errors) > 0 {
			fmt.Println("\nErrors:")
			for _, err := range response.Errors {
				fmt.Printf("  - %v\n", err)
			}
		}
		if len(response.Warnings) > 0 {
			fmt.Println("\nWarnings:")
			for _, warning := range response.Warnings {
				fmt.Printf("  - %v\n", warning)
			}
		}
		os.Exit(1)
	}
}

func handleSchemaStats(args []string) {
	// Create flag set for stats command
	statsFlags := flag.NewFlagSet("stats", flag.ExitOnError)

	if err := statsFlags.Parse(args); err != nil {
		os.Exit(1)
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("GET", "/v1/admin/schemas/stats", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get schema statistics: %v\n", err)
		os.Exit(1)
	}

	var response SchemaStatsResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Schema Registry Statistics:")
	prettyJSON, err := json.MarshalIndent(response.Stats, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to format stats: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(prettyJSON))
}

func makeAPIRequest(method, endpoint string, body interface{}) ([]byte, error) {
	url := strings.TrimRight(gatewayURL, "/") + endpoint

	if verbose {
		fmt.Printf("Making %s request to: %s\n", method, url)
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

// Agent management command handlers

func handleAgentCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Agent commands: register, unregister, list")
		os.Exit(1)
	}

	subcommand := args[0]
	subcommandArgs := args[1:]

	switch subcommand {
	case "register":
		handleAgentRegister(subcommandArgs)
	case "unregister":
		handleAgentUnregister(subcommandArgs)
	case "list":
		handleAgentList(subcommandArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown agent command: %s\n", subcommand)
		os.Exit(1)
	}
}

func handleAgentRegister(args []string) {
	// Create flag set for register command
	registerFlags := flag.NewFlagSet("register", flag.ExitOnError)

	var mode string
	var target string
	var headers []string
	var schemas []string

	registerFlags.StringVar(&mode, "mode", "pull", "Delivery mode: 'push' or 'pull'")
	registerFlags.StringVar(&target, "target", "", "Push target URL (required for push mode)")

	// Custom flag for multiple headers
	registerFlags.Func("header", "Custom header in format key=value (can be used multiple times)", func(value string) error {
		headers = append(headers, value)
		return nil
	})

	// Custom flag for multiple schemas
	registerFlags.Func("schema", "Supported schema in format agntcy:domain.entity.version or agntcy:domain.* (can be used multiple times)", func(value string) error {
		schemas = append(schemas, value)
		return nil
	})

	registerFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin agent register <name> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nRegister a local agent using the agent name (domain will be auto-added).\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		registerFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin --admin-key-file admin.key agent register user --mode pull\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin --admin-key-file admin.key agent register api-service --mode push --target http://webhook:8080\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin --admin-key-file admin.key agent register purchase-bot --mode push --target http://webhook:8080 --header \"Auth=Bearer token\"\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin --admin-key-file admin.key agent register sales --mode pull --schema \"agntcy:commerce.*\" --schema \"agntcy:crm.lead.v1\"\n")
	}

	if len(args) < 1 {
		registerFlags.Usage()
		os.Exit(1)
	}

	agentName := args[0]

	// Parse flags starting from the second argument
	if err := registerFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	// Validate mode
	if mode != "push" && mode != "pull" {
		fmt.Fprintf(os.Stderr, "Error: Delivery mode must be 'push' or 'pull'\n")
		os.Exit(1)
	}

	// Validate push mode requirements
	if mode == "push" && target == "" {
		fmt.Fprintf(os.Stderr, "Error: Push target URL is required for push mode (--target flag)\n")
		registerFlags.Usage()
		os.Exit(1)
	}

	// Parse headers
	headerMap := make(map[string]string)
	for _, header := range headers {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: Invalid header format '%s'. Use key=value format\n", header)
			os.Exit(1)
		}
		headerMap[parts[0]] = parts[1]
	}

	// Create agent request
	agent := LocalAgent{
		Address:          agentName,
		DeliveryMode:     mode,
		PushTarget:       target,
		Headers:          headerMap,
		SupportedSchemas: schemas,
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("POST", "/v1/admin/agents", agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register agent: %v\n", err)
		os.Exit(1)
	}

	var response AgentResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if response.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", response.Error)
		os.Exit(1)
	}

	// The response contains the full address after normalization
	finalAddress := agentName
	if response.Agent != nil && response.Agent.Address != "" {
		finalAddress = response.Agent.Address
	}

	fmt.Printf("Successfully registered agent: %s\n", finalAddress)
	fmt.Printf("  Mode: %s\n", mode)
	if response.Agent != nil && response.Agent.APIKey != "" {
		fmt.Printf("  API Key: %s\n", response.Agent.APIKey)
		fmt.Printf("  ⚠️  IMPORTANT: Save this API key securely! It's required for inbox access.\n")
	}
	if mode == "push" {
		fmt.Printf("  Target: %s\n", target)
		if len(headerMap) > 0 {
			fmt.Printf("  Headers:\n")
			for key, value := range headerMap {
				fmt.Printf("    %s: %s\n", key, value)
			}
		}
	}
}

func handleAgentUnregister(args []string) {
	// Create flag set for unregister command
	unregisterFlags := flag.NewFlagSet("unregister", flag.ExitOnError)

	unregisterFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin agent unregister <name>\n")
		fmt.Fprintf(os.Stderr, "\nUnregister a local agent using the agent name.\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin --admin-key-file admin.key agent unregister user\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin --admin-key-file admin.key agent unregister api-service\n")
	}

	if len(args) < 1 {
		unregisterFlags.Usage()
		os.Exit(1)
	}

	agentName := args[0]

	if err := unregisterFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	// Reject full addresses - only accept agent names
	if strings.Contains(agentName, "@") {
		fmt.Fprintf(os.Stderr, "Error: Only agent names are allowed, not full addresses. Use '%s' instead of '%s'\n",
			strings.Split(agentName, "@")[0], agentName)
		os.Exit(1)
	}

	// Make HTTP request with admin authentication - server will handle normalization
	resp, err := makeAdminAPIRequest("DELETE", "/v1/admin/agents/"+agentName, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unregister agent: %v\n", err)
		os.Exit(1)
	}

	var response AgentResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if response.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", response.Error)
		os.Exit(1)
	}

	fmt.Printf("Successfully unregistered agent: %s\n", agentName)
}

func handleAgentList(args []string) {
	// Create flag set for list command
	listFlags := flag.NewFlagSet("list", flag.ExitOnError)

	if err := listFlags.Parse(args); err != nil {
		os.Exit(1)
	}

	// Make HTTP request with admin authentication
	resp, err := makeAdminAPIRequest("GET", "/v1/admin/agents", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list agents: %v\n", err)
		os.Exit(1)
	}

	var response ListAgentsResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d agent(s):\n\n", response.Count)
	if response.Count == 0 {
		fmt.Println("  No agents registered")
		return
	}

	for address, agent := range response.Agents {
		fmt.Printf("  %s\n", address)
		fmt.Printf("    Mode: %s\n", agent.DeliveryMode)
		if agent.APIKey != "" {
			// Show only first 8 characters for security
			maskedKey := agent.APIKey[:8] + "..."
			fmt.Printf("    API Key: %s (masked)\n", maskedKey)
		}
		if !agent.CreatedAt.IsZero() {
			fmt.Printf("    Created: %s\n", agent.CreatedAt.Format(time.RFC3339))
		}
		if !agent.LastAccess.IsZero() {
			fmt.Printf("    Last Access: %s\n", agent.LastAccess.Format(time.RFC3339))
		}
		if agent.DeliveryMode == "push" {
			fmt.Printf("    Target: %s\n", agent.PushTarget)
			if len(agent.Headers) > 0 {
				fmt.Printf("    Headers:\n")
				for key, value := range agent.Headers {
					fmt.Printf("      %s: %s\n", key, value)
				}
			}
		}
		fmt.Println()
	}
}

// Inbox management command handlers

func handleInboxCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Inbox commands: get, ack")
		os.Exit(1)
	}

	subcommand := args[0]
	subcommandArgs := args[1:]

	switch subcommand {
	case "get":
		handleInboxGet(subcommandArgs)
	case "ack":
		handleInboxAck(subcommandArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown inbox command: %s\n", subcommand)
		os.Exit(1)
	}
}

func handleInboxGet(args []string) {
	// Create flag set for get command
	getFlags := flag.NewFlagSet("get", flag.ExitOnError)

	var apiKey string
	var keyFile string

	getFlags.StringVar(&apiKey, "key", "", "Agent API key for authentication")
	getFlags.StringVar(&keyFile, "key-file", "", "File containing agent API key")

	getFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin inbox get <recipient> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		getFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin inbox get test2@localhost --key your-api-key\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin inbox get test2@localhost --key-file test2.key\n")
	}

	if len(args) < 1 {
		getFlags.Usage()
		os.Exit(1)
	}

	recipient := args[0]

	if err := getFlags.Parse(args[1:]); err != nil {
		os.Exit(1)
	}

	// Get API key from file if specified
	if keyFile != "" {
		keyBytes, err := os.ReadFile(filepath.Clean(keyFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read key file: %v\n", err)
			os.Exit(1)
		}
		apiKey = strings.TrimSpace(string(keyBytes))
	}

	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: API key is required. Use --key or --key-file flag.\n")
		getFlags.Usage()
		os.Exit(1)
	}

	// Make HTTP request with authentication
	resp, err := makeAuthenticatedAPIRequest("GET", "/v1/inbox/"+recipient, nil, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get inbox: %v\n", err)
		os.Exit(1)
	}

	var response InboxResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Inbox for %s: %d message(s)\n\n", recipient, response.Count)
	if response.Count == 0 {
		fmt.Println("  No messages")
		return
	}

	for i, message := range response.Messages {
		fmt.Printf("  Message %d:\n", i+1)
		fmt.Printf("    ID: %s\n", message.MessageID)
		fmt.Printf("    From: %s\n", message.Sender)
		fmt.Printf("    Subject: %s\n", message.Subject)
		fmt.Printf("    Timestamp: %s\n", message.Timestamp.Format(time.RFC3339))
		if len(message.Payload) > 0 {
			fmt.Printf("    Payload:\n")
			payloadJSON, _ := json.MarshalIndent(message.Payload, "      ", "  ")
			fmt.Printf("      %s\n", string(payloadJSON))
		}
		fmt.Println()
	}
}

func handleInboxAck(args []string) {
	// Create flag set for ack command
	ackFlags := flag.NewFlagSet("ack", flag.ExitOnError)

	var apiKey string
	var keyFile string

	ackFlags.StringVar(&apiKey, "key", "", "Agent API key for authentication")
	ackFlags.StringVar(&keyFile, "key-file", "", "File containing agent API key")

	ackFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: agentry-admin inbox ack <recipient> <message-id> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		ackFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin inbox ack test2@localhost message-id-123 --key your-api-key\n")
		fmt.Fprintf(os.Stderr, "  agentry-admin inbox ack test2@localhost message-id-123 --key-file test2.key\n")
	}

	if len(args) < 2 {
		ackFlags.Usage()
		os.Exit(1)
	}

	recipient := args[0]
	messageID := args[1]

	if err := ackFlags.Parse(args[2:]); err != nil {
		os.Exit(1)
	}

	// Get API key from file if specified
	if keyFile != "" {
		keyBytes, err := os.ReadFile(filepath.Clean(keyFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read key file: %v\n", err)
			os.Exit(1)
		}
		apiKey = strings.TrimSpace(string(keyBytes))
	}

	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: API key is required. Use --key or --key-file flag.\n")
		ackFlags.Usage()
		os.Exit(1)
	}

	// Make HTTP request with authentication
	resp, err := makeAuthenticatedAPIRequest("DELETE", "/v1/inbox/"+recipient+"/"+messageID, nil, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to acknowledge message: %v\n", err)
		os.Exit(1)
	}

	var response AckResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully acknowledged message: %s\n", messageID)
	fmt.Printf("  Recipient: %s\n", recipient)
}
