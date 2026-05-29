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
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Local agent management commands (requires admin key)",
}

func init() {
	registerCmd := &cobra.Command{
		Use:   "register <name>",
		Short: "Register a local agent (name only, domain auto-added)",
		Long:  "Register a local agent using the agent name (domain will be auto-added).",
		Example: "  agentry-admin --admin-key-file admin.key agent register user --mode pull\n" +
			"  agentry-admin --admin-key-file admin.key agent register api-service --mode push --target http://webhook:8080\n" +
			"  agentry-admin --admin-key-file admin.key agent register purchase-bot --mode push --target http://webhook:8080 --header \"Auth=Bearer token\"\n" +
			"  agentry-admin --admin-key-file admin.key agent register sales --mode pull --schema \"agntcy:commerce.*\" --schema \"agntcy:crm.lead.v1\"",
		Args: cobra.ExactArgs(1),
		Run:  runAgentRegister,
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
		Run:  runAgentUnregister,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered agents",
		Args:  cobra.NoArgs,
		Run:   runAgentList,
	}

	agentCmd.AddCommand(registerCmd, unregisterCmd, listCmd)
}

func runAgentRegister(cmd *cobra.Command, args []string) {
	agentName := args[0]
	mode, _ := cmd.Flags().GetString("mode")
	target, _ := cmd.Flags().GetString("target")
	headers, _ := cmd.Flags().GetStringArray("header")
	schemas, _ := cmd.Flags().GetStringArray("schema")

	// Validate mode
	if mode != "push" && mode != "pull" {
		fmt.Fprintf(os.Stderr, "Error: Delivery mode must be 'push' or 'pull'\n")
		os.Exit(1)
	}

	// Validate push mode requirements
	if mode == "push" && target == "" {
		fmt.Fprintf(os.Stderr, "Error: Push target URL is required for push mode (--target flag)\n")
		_ = cmd.Usage()
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

func runAgentUnregister(cmd *cobra.Command, args []string) {
	agentName := args[0]

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

func runAgentList(cmd *cobra.Command, args []string) {
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
