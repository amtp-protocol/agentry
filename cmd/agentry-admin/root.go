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
	"os"

	"github.com/spf13/cobra"
)

// Global flags shared across all commands.
var (
	gatewayURL   = "http://localhost:8080"
	verbose      = false
	adminKeyFile = ""
)

var rootCmd = &cobra.Command{
	Use:   "agentry-admin",
	Short: "Agentry Admin Tool",
	Long:  "Agentry Admin Tool - manage schemas, local agents, and inboxes on an Agentry gateway.",
	// Mirror the original behavior: bare invocation prints usage and exits
	// non-zero. (`--help` is intercepted by cobra before Run and exits 0.)
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
		os.Exit(1)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&gatewayURL, "gateway-url", "http://localhost:8080", "Gateway URL")
	pf.BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	pf.StringVar(&adminKeyFile, "admin-key-file", "", "Admin API key file for administrative operations")

	rootCmd.AddCommand(schemaCmd, agentCmd, inboxCmd)
}
