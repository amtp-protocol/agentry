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
	"errors"

	"github.com/spf13/cobra"

	"github.com/amtp-protocol/agentry/internal/version"
)

// errExit signals that a command already reported its failure to stderr and the
// process should exit non-zero without printing anything further. It lets
// command handlers preserve their exact error output while routing the actual
// exit through main().
var errExit = errors.New("")

// buildRootCmd assembles the full command tree around the given client. The
// client's configuration fields are bound to the persistent flags, so they are
// populated when the flags are parsed and shared with every subcommand.
func buildRootCmd(c *Client) *cobra.Command {
	root := &cobra.Command{
		Use:     "agentry-admin",
		Version: version.Version,
		Short:   "Agentry Admin Tool",
		Long:    "Agentry Admin Tool - manage schemas, local agents, and inboxes on an Agentry gateway.",
		// Mirror the original behavior: bare invocation prints usage and exits
		// non-zero. (`--help` is intercepted by cobra before RunE and exits 0.)
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errExit
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&c.GatewayURL, "gateway-url", "http://localhost:8080", "Gateway URL")
	pf.BoolVarP(&c.Verbose, "verbose", "v", false, "Verbose output")
	pf.StringVar(&c.AdminKeyFile, "admin-key-file", "", "Admin API key file for administrative operations")

	root.AddCommand(newSchemaCmd(c), newAgentCmd(c), newInboxCmd(c))

	return root
}
