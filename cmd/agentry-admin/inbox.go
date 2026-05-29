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
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Inbox management commands (requires agent API key)",
}

func init() {
	getCmd := &cobra.Command{
		Use:   "get <recipient>",
		Short: "Get messages for recipient",
		Example: "  agentry-admin inbox get test2@localhost --key your-api-key\n" +
			"  agentry-admin inbox get test2@localhost --key-file test2.key",
		Args: cobra.ExactArgs(1),
		Run:  runInboxGet,
	}
	getCmd.Flags().String("key", "", "Agent API key for authentication")
	getCmd.Flags().String("key-file", "", "File containing agent API key")

	ackCmd := &cobra.Command{
		Use:   "ack <recipient> <message-id>",
		Short: "Acknowledge/remove a message",
		Example: "  agentry-admin inbox ack test2@localhost message-id-123 --key your-api-key\n" +
			"  agentry-admin inbox ack test2@localhost message-id-123 --key-file test2.key",
		Args: cobra.ExactArgs(2),
		Run:  runInboxAck,
	}
	ackCmd.Flags().String("key", "", "Agent API key for authentication")
	ackCmd.Flags().String("key-file", "", "File containing agent API key")

	inboxCmd.AddCommand(getCmd, ackCmd)
}

// resolveAPIKey returns the API key from the --key flag, or reads it from the
// --key-file flag if provided. It exits the process with an error if neither
// yields a key.
func resolveAPIKey(cmd *cobra.Command) string {
	apiKey, _ := cmd.Flags().GetString("key")
	keyFile, _ := cmd.Flags().GetString("key-file")

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
		_ = cmd.Usage()
		os.Exit(1)
	}

	return apiKey
}

func runInboxGet(cmd *cobra.Command, args []string) {
	recipient := args[0]
	apiKey := resolveAPIKey(cmd)

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

func runInboxAck(cmd *cobra.Command, args []string) {
	recipient := args[0]
	messageID := args[1]
	apiKey := resolveAPIKey(cmd)

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
