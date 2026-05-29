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

func newSchemaCmd(c *Client) *cobra.Command {
	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "Schema management commands (requires admin key)",
	}

	registerCmd := &cobra.Command{
		Use:   "register <schema-id>",
		Short: "Register a new schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaRegister(c, cmd, args)
		},
	}
	registerCmd.Flags().StringP("file", "f", "", "Schema definition file (required)")
	registerCmd.Flags().Bool("force", false, "Overwrite existing schema")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all schemas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaList(c, cmd, args)
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <schema-id>",
		Short: "Get a schema definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaGet(c, cmd, args)
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <schema-id>",
		Short: "Delete a schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaDelete(c, cmd, args)
		},
	}

	validateCmd := &cobra.Command{
		Use:   "validate <schema-id>",
		Short: "Validate a payload against a schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaValidate(c, cmd, args)
		},
	}
	validateCmd.Flags().StringP("file", "f", "", "Payload file to validate (required)")

	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show schema registry statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaStats(c, cmd, args)
		},
	}

	schemaCmd.AddCommand(registerCmd, listCmd, getCmd, deleteCmd, validateCmd, statsCmd)
	return schemaCmd
}

func runSchemaRegister(c *Client, cmd *cobra.Command, args []string) error {
	schemaID := args[0]
	schemaFile, _ := cmd.Flags().GetString("file")
	force, _ := cmd.Flags().GetBool("force")

	if schemaFile == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: Schema file is required (-f or --file flag)\n")
		_ = cmd.Usage()
		return errExit
	}

	// Read schema file
	data, err := os.ReadFile(filepath.Clean(schemaFile))
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to read schema file: %v\n", err)
		return errExit
	}

	// Validate JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Invalid JSON in schema file: %v\n", err)
		return errExit
	}

	// Create request
	req := RegisterSchemaRequest{
		ID:         schemaID,
		Definition: json.RawMessage(data),
		Force:      force,
	}

	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("POST", "/v1/admin/schemas", req)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to register schema: %v\n", err)
		return errExit
	}

	var response SchemaResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	if response.Error != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", response.Error)
		return errExit
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Successfully registered schema: %s\n", schemaID)
	return nil
}

func runSchemaList(c *Client, cmd *cobra.Command, args []string) error {
	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("GET", "/v1/admin/schemas", nil)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to list schemas: %v\n", err)
		return errExit
	}

	var response ListSchemasResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Found %d schema(s):\n\n", response.Count)
	for _, schema := range response.Schemas {
		if schema.Raw != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", schema.Raw)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  agntcy:%s.%s.%s\n", schema.Domain, schema.Entity, schema.Version)
		}
	}
	return nil
}

func runSchemaGet(c *Client, cmd *cobra.Command, args []string) error {
	schemaID := args[0]

	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("GET", "/v1/admin/schemas/"+schemaID, nil)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to get schema: %v\n", err)
		return errExit
	}

	var response map[string]interface{}
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	// Pretty print the schema
	prettyJSON, err := json.MarshalIndent(response["schema"], "", "  ")
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to format schema: %v\n", err)
		return errExit
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Schema: %s\n\n", schemaID)
	fmt.Fprintln(cmd.OutOrStdout(), string(prettyJSON))
	return nil
}

func runSchemaDelete(c *Client, cmd *cobra.Command, args []string) error {
	schemaID := args[0]

	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("DELETE", "/v1/admin/schemas/"+schemaID, nil)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to delete schema: %v\n", err)
		return errExit
	}

	var response SchemaResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	if response.Error != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", response.Error)
		return errExit
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Successfully deleted schema: %s\n", schemaID)
	return nil
}

func runSchemaValidate(c *Client, cmd *cobra.Command, args []string) error {
	schemaID := args[0]
	payloadFile, _ := cmd.Flags().GetString("file")

	if payloadFile == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: Payload file is required (-f or --file flag)\n")
		_ = cmd.Usage()
		return errExit
	}

	// Read payload file
	data, err := os.ReadFile(filepath.Clean(payloadFile))
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to read payload file: %v\n", err)
		return errExit
	}

	// Validate JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Invalid JSON in payload file: %v\n", err)
		return errExit
	}

	// Create request
	req := ValidatePayloadRequest{
		Payload: json.RawMessage(data),
	}

	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("POST", "/v1/admin/schemas/"+schemaID+"/validate", req)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to validate payload: %v\n", err)
		return errExit
	}

	var response ValidationResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	if response.Valid {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Payload is valid against schema: %s\n", schemaID)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "✗ Payload is invalid against schema: %s\n", schemaID)
		if len(response.Errors) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "\nErrors:")
			for _, err := range response.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %v\n", err)
			}
		}
		if len(response.Warnings) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "\nWarnings:")
			for _, warning := range response.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %v\n", warning)
			}
		}
		return errExit
	}
	return nil
}

func runSchemaStats(c *Client, cmd *cobra.Command, args []string) error {
	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("GET", "/v1/admin/schemas/stats", nil)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to get schema statistics: %v\n", err)
		return errExit
	}

	var response SchemaStatsResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Schema Registry Statistics:")
	prettyJSON, err := json.MarshalIndent(response.Stats, "", "  ")
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to format stats: %v\n", err)
		return errExit
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(prettyJSON))
	return nil
}
