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
	"strings"
	"testing"
)

func TestBareInvocation_PrintsHelpAndExitsNonZero(t *testing.T) {
	// No subcommand: should print help and signal a non-zero exit via errExit.
	_, _, err := runCLI(t, "http://127.0.0.1:0", nil)
	if !errors.Is(err, errExit) {
		t.Fatalf("err = %v, want errExit", err)
	}
}

func TestUnknownCommand_ReturnsError(t *testing.T) {
	// Unknown commands are surfaced by cobra itself (not errExit), so main
	// prints them.
	_, _, err := runCLI(t, "http://127.0.0.1:0", nil, "bogus")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if errors.Is(err, errExit) {
		t.Fatal("unknown command should surface a real cobra error, not errExit")
	}
}

// TestGlobalFlagsAfterSubcommand verifies cobra accepts persistent flags placed
// after the subcommand and its positional args, matching how the old manual
// parser behaved.
func TestGlobalFlagsAfterSubcommand(t *testing.T) {
	srv, cap := newMockGateway(t, 200, `{"count":0,"schemas":[]}`)
	keyFile := writeTempFile(t, "admin-key")

	_, stderr, err := runCLI(t, srv.URL, srv.Client(),
		"schema", "list", "--admin-key-file", keyFile)
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr: %s)", err, stderr)
	}
	if cap.Header.Get("X-Admin-Key") != "admin-key" {
		t.Errorf("admin key not applied when flag placed after subcommand")
	}
}

func TestHelpFlag_ExitsZero(t *testing.T) {
	stdout, _, err := runCLI(t, "http://127.0.0.1:0", nil, "--help")
	if err != nil {
		t.Fatalf("--help should exit cleanly, got %v", err)
	}
	if !strings.Contains(stdout, "Agentry Admin Tool") {
		t.Errorf("help output = %q", stdout)
	}
}
