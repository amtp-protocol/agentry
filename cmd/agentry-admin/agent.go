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
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newAgentCmd(c *Client) *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Local agent management commands (requires admin key)",
	}

	registerCmd := &cobra.Command{
		Use:   "register <name>",
		Short: "Register a local agent (name only, domain auto-added)",
		Long:  "Register a local agent using the agent name (domain will be auto-added).",
		Example: "  agentry-admin --admin-key-file admin.key agent register user --mode pull\n" +
			"  agentry-admin --admin-key-file admin.key agent register api-service --mode push --target http://webhook:8080\n" +
			"  agentry-admin --admin-key-file admin.key agent register purchase-bot --mode push --target http://webhook:8080 --header \"Auth=Bearer token\"\n" +
			"  agentry-admin --admin-key-file admin.key agent register sales --mode pull --schema \"agntcy:commerce.*\" --schema \"agntcy:crm.lead.v1\"",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentRegister(c, cmd, args)
		},
	}
	registerCmd.Flags().String("mode", "pull", "Delivery mode: 'push' or 'pull'")
	registerCmd.Flags().String("target", "", "Push target URL (required for push mode)")
	registerCmd.Flags().StringArray("header", nil, "Custom header in format key=value (can be used multiple times)")
	registerCmd.Flags().StringArray("schema", nil, "Supported schema in format agntcy:domain.entity.version or agntcy:domain.* (can be used multiple times)")

	unregisterCmd := &cobra.Command{
		Use:   "unregister <name>",
		Short: "Unregister a local agent",
		Long:  "Unregister a local agent using the agent name.",
		Example: "  agentry-admin --admin-key-file admin.key agent unregister user\n" +
			"  agentry-admin --admin-key-file admin.key agent unregister api-service",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentUnregister(c, cmd, args)
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentList(c, cmd, args)
		},
	}

	agentCmd.AddCommand(registerCmd, unregisterCmd, listCmd)
	return agentCmd
}

func runAgentRegister(c *Client, cmd *cobra.Command, args []string) error {
	agentName := args[0]
	mode, _ := cmd.Flags().GetString("mode")
	target, _ := cmd.Flags().GetString("target")
	headers, _ := cmd.Flags().GetStringArray("header")
	schemas, _ := cmd.Flags().GetStringArray("schema")

	// Validate mode
	if mode != "push" && mode != "pull" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: Delivery mode must be 'push' or 'pull'\n")
		return errExit
	}

	// Validate push mode requirements
	if mode == "push" && target == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: Push target URL is required for push mode (--target flag)\n")
		_ = cmd.Usage()
		return errExit
	}

	// Parse headers
	headerMap := make(map[string]string)
	for _, header := range headers {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) != 2 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: Invalid header format '%s'. Use key=value format\n", header)
			return errExit
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
	resp, err := c.AdminRequest("POST", "/v1/admin/agents", agent)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to register agent: %v\n", err)
		return errExit
	}

	var response AgentResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	if response.Error != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", response.Error)
		return errExit
	}

	// The response contains the full address after normalization
	finalAddress := agentName
	if response.Agent != nil && response.Agent.Address != "" {
		finalAddress = response.Agent.Address
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Successfully registered agent: %s\n", finalAddress)
	fmt.Fprintf(out, "  Mode: %s\n", mode)
	if response.Agent != nil && response.Agent.APIKey != "" {
		fmt.Fprintf(out, "  API Key: %s\n", response.Agent.APIKey)
		fmt.Fprintf(out, "  ⚠️  IMPORTANT: Save this API key securely! It's required for inbox access.\n")
	}
	if mode == "push" {
		fmt.Fprintf(out, "  Target: %s\n", target)
		if len(headerMap) > 0 {
			fmt.Fprintf(out, "  Headers:\n")
			for key, value := range headerMap {
				fmt.Fprintf(out, "    %s: %s\n", key, value)
			}
		}
	}
	return nil
}

func runAgentUnregister(c *Client, cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Reject full addresses - only accept agent names
	if strings.Contains(agentName, "@") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: Only agent names are allowed, not full addresses. Use '%s' instead of '%s'\n",
			strings.Split(agentName, "@")[0], agentName)
		return errExit
	}

	// Make HTTP request with admin authentication - server will handle normalization
	resp, err := c.AdminRequest("DELETE", "/v1/admin/agents/"+agentName, nil)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to unregister agent: %v\n", err)
		return errExit
	}

	var response AgentResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	if response.Error != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", response.Error)
		return errExit
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Successfully unregistered agent: %s\n", agentName)
	return nil
}

func runAgentList(c *Client, cmd *cobra.Command, args []string) error {
	// Make HTTP request with admin authentication
	resp, err := c.AdminRequest("GET", "/v1/admin/agents", nil)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to list agents: %v\n", err)
		return errExit
	}

	var response ListAgentsResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Found %d agent(s):\n\n", response.Count)
	if response.Count == 0 {
		fmt.Fprintln(out, "  No agents registered")
		return nil
	}

	for address, agent := range response.Agents {
		fmt.Fprintf(out, "  %s\n", address)
		fmt.Fprintf(out, "    Mode: %s\n", agent.DeliveryMode)
		if agent.APIKey != "" {
			// Show only first 8 characters for security
			maskedKey := agent.APIKey[:8] + "..."
			fmt.Fprintf(out, "    API Key: %s (masked)\n", maskedKey)
		}
		if !agent.CreatedAt.IsZero() {
			fmt.Fprintf(out, "    Created: %s\n", agent.CreatedAt.Format(time.RFC3339))
		}
		if !agent.LastAccess.IsZero() {
			fmt.Fprintf(out, "    Last Access: %s\n", agent.LastAccess.Format(time.RFC3339))
		}
		if agent.DeliveryMode == "push" {
			fmt.Fprintf(out, "    Target: %s\n", agent.PushTarget)
			if len(agent.Headers) > 0 {
				fmt.Fprintf(out, "    Headers:\n")
				for key, value := range agent.Headers {
					fmt.Fprintf(out, "      %s: %s\n", key, value)
				}
			}
		}
		fmt.Fprintln(out)
	}
	return nil
}
