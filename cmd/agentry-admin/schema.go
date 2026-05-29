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
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Schema management commands (requires admin key)",
}

func init() {
	registerCmd := &cobra.Command{
		Use:   "register <schema-id>",
		Short: "Register a new schema",
		Args:  cobra.ExactArgs(1),
		Run:   runSchemaRegister,
	}
	registerCmd.Flags().StringP("file", "f", "", "Schema definition file (required)")
	registerCmd.Flags().Bool("force", false, "Overwrite existing schema")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all schemas",
		Args:  cobra.NoArgs,
		Run:   runSchemaList,
	}

	getCmd := &cobra.Command{
		Use:   "get <schema-id>",
		Short: "Get a schema definition",
		Args:  cobra.ExactArgs(1),
		Run:   runSchemaGet,
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <schema-id>",
		Short: "Delete a schema",
		Args:  cobra.ExactArgs(1),
		Run:   runSchemaDelete,
	}

	validateCmd := &cobra.Command{
		Use:   "validate <schema-id>",
		Short: "Validate a payload against a schema",
		Args:  cobra.ExactArgs(1),
		Run:   runSchemaValidate,
	}
	validateCmd.Flags().StringP("file", "f", "", "Payload file to validate (required)")

	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show schema registry statistics",
		Args:  cobra.NoArgs,
		Run:   runSchemaStats,
	}

	schemaCmd.AddCommand(registerCmd, listCmd, getCmd, deleteCmd, validateCmd, statsCmd)
}

func runSchemaRegister(cmd *cobra.Command, args []string) {
	schemaID := args[0]
	schemaFile, _ := cmd.Flags().GetString("file")
	force, _ := cmd.Flags().GetBool("force")

	if schemaFile == "" {
		fmt.Fprintf(os.Stderr, "Error: Schema file is required (-f or --file flag)\n")
		_ = cmd.Usage()
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

func runSchemaList(cmd *cobra.Command, args []string) {
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

func runSchemaGet(cmd *cobra.Command, args []string) {
	schemaID := args[0]

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

func runSchemaDelete(cmd *cobra.Command, args []string) {
	schemaID := args[0]

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

func runSchemaValidate(cmd *cobra.Command, args []string) {
	schemaID := args[0]
	payloadFile, _ := cmd.Flags().GetString("file")

	if payloadFile == "" {
		fmt.Fprintf(os.Stderr, "Error: Payload file is required (-f or --file flag)\n")
		_ = cmd.Usage()
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

func runSchemaStats(cmd *cobra.Command, args []string) {
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
