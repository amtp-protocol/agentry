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

func newInboxCmd(c *Client) *cobra.Command {
	inboxCmd := &cobra.Command{
		Use:   "inbox",
		Short: "Inbox management commands (requires agent API key)",
	}

	getCmd := &cobra.Command{
		Use:   "get <recipient>",
		Short: "Get messages for recipient",
		Example: "  agentry-admin inbox get test2@localhost --key your-api-key\n" +
			"  agentry-admin inbox get test2@localhost --key-file test2.key",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInboxGet(c, cmd, args)
		},
	}
	getCmd.Flags().String("key", "", "Agent API key for authentication")
	getCmd.Flags().String("key-file", "", "File containing agent API key")

	ackCmd := &cobra.Command{
		Use:   "ack <recipient> <message-id>",
		Short: "Acknowledge/remove a message",
		Example: "  agentry-admin inbox ack test2@localhost message-id-123 --key your-api-key\n" +
			"  agentry-admin inbox ack test2@localhost message-id-123 --key-file test2.key",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInboxAck(c, cmd, args)
		},
	}
	ackCmd.Flags().String("key", "", "Agent API key for authentication")
	ackCmd.Flags().String("key-file", "", "File containing agent API key")

	inboxCmd.AddCommand(getCmd, ackCmd)
	return inboxCmd
}

// resolveAPIKey returns the API key from the --key flag, or reads it from the
// --key-file flag if provided. On failure it reports the error to stderr and
// returns errExit.
func resolveAPIKey(cmd *cobra.Command) (string, error) {
	apiKey, _ := cmd.Flags().GetString("key")
	keyFile, _ := cmd.Flags().GetString("key-file")

	// Get API key from file if specified
	if keyFile != "" {
		keyBytes, err := os.ReadFile(filepath.Clean(keyFile))
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Failed to read key file: %v\n", err)
			return "", errExit
		}
		apiKey = strings.TrimSpace(string(keyBytes))
	}

	if apiKey == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: API key is required. Use --key or --key-file flag.\n")
		_ = cmd.Usage()
		return "", errExit
	}

	return apiKey, nil
}

func runInboxGet(c *Client, cmd *cobra.Command, args []string) error {
	recipient := args[0]
	apiKey, err := resolveAPIKey(cmd)
	if err != nil {
		return err
	}

	// Make HTTP request with authentication
	resp, err := c.AuthenticatedRequest("GET", "/v1/inbox/"+recipient, nil, apiKey)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to get inbox: %v\n", err)
		return errExit
	}

	var response InboxResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Inbox for %s: %d message(s)\n\n", recipient, response.Count)
	if response.Count == 0 {
		fmt.Fprintln(out, "  No messages")
		return nil
	}

	for i, message := range response.Messages {
		fmt.Fprintf(out, "  Message %d:\n", i+1)
		fmt.Fprintf(out, "    ID: %s\n", message.MessageID)
		fmt.Fprintf(out, "    From: %s\n", message.Sender)
		fmt.Fprintf(out, "    Subject: %s\n", message.Subject)
		fmt.Fprintf(out, "    Timestamp: %s\n", message.Timestamp.Format(time.RFC3339))
		if len(message.Payload) > 0 {
			fmt.Fprintf(out, "    Payload:\n")
			payloadJSON, _ := json.MarshalIndent(message.Payload, "      ", "  ")
			fmt.Fprintf(out, "      %s\n", string(payloadJSON))
		}
		fmt.Fprintln(out)
	}
	return nil
}

func runInboxAck(c *Client, cmd *cobra.Command, args []string) error {
	recipient := args[0]
	messageID := args[1]
	apiKey, err := resolveAPIKey(cmd)
	if err != nil {
		return err
	}

	// Make HTTP request with authentication
	resp, err := c.AuthenticatedRequest("DELETE", "/v1/inbox/"+recipient+"/"+messageID, nil, apiKey)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to acknowledge message: %v\n", err)
		return errExit
	}

	var response AckResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Failed to parse response: %v\n", err)
		return errExit
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Successfully acknowledged message: %s\n", messageID)
	fmt.Fprintf(out, "  Recipient: %s\n", recipient)
	return nil
}
