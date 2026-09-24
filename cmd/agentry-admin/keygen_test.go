/*
 * Copyright 2026 Sen Wang
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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amtp-protocol/agentry/internal/signing"
)

// runKeygen executes the keygen command and returns stdout plus the path of
// the generated private key file.
func runKeygen(t *testing.T, args ...string) (stdout string, keyPath string) {
	t.Helper()
	outDir := t.TempDir()
	keyFile := filepath.Join(outDir, "private.pem")
	stdout, stderr, err := runCLI(t, "http://127.0.0.1:0", nil,
		append([]string{"keygen", "--out", keyFile}, args...)...)
	if err != nil {
		t.Fatalf("keygen failed: %v (stderr: %s)", err, stderr)
	}
	return stdout, keyFile
}

// parseKeygenTXT extracts the DNS TXT record value from keygen output. The
// output prints a line of the form "TXT: <value>".
func parseKeygenTXT(t *testing.T, stdout string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "TXT: "); ok {
			return strings.Trim(after, `"`)
		}
	}
	t.Fatalf("no TXT record in keygen output:\n%s", stdout)
	return ""
}

func TestKeygenES256RoundTrip(t *testing.T) {
	stdout, keyPath := runKeygen(t, "--domain", "sender.example", "--key-id", "k1")

	// The private key file must exist with 0600 permissions.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("private key not written: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("private key permissions = %o, want 0600", info.Mode().Perm())
	}

	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	// The generated key must load as an ES256 signer.
	signer, err := signing.LoadSigner(pemBytes, "ES256", "k1")
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}
	if signer.Algorithm() != "ES256" || signer.KeyID() != "k1" {
		t.Errorf("signer = %s/%s, want ES256/k1", signer.Algorithm(), signer.KeyID())
	}

	// The printed TXT record must parse and match the signer's public key.
	txt := parseKeygenTXT(t, stdout)
	rec, err := signing.ParseKeyRecord(txt)
	if err != nil {
		t.Fatalf("ParseKeyRecord(%q): %v", txt, err)
	}
	want, err := signer.PublicKeyRecord()
	if err != nil {
		t.Fatalf("PublicKeyRecord: %v", err)
	}
	if rec.Algorithm != want.Algorithm {
		t.Errorf("TXT algorithm = %s, want %s", rec.Algorithm, want.Algorithm)
	}
	if !signing.KeysEqual(rec.PublicKey, want.PublicKey) {
		t.Error("TXT public key does not match the generated private key")
	}

	// The output must name the DNS owner so operators know where to publish.
	if !strings.Contains(stdout, "k1._amtpkey.sender.example.") {
		t.Errorf("output missing DNS owner name:\n%s", stdout)
	}
}

func TestKeygenRS256(t *testing.T) {
	stdout, keyPath := runKeygen(t, "--domain", "sender.example", "--key-id", "k2", "--algorithm", "RS256")

	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	signer, err := signing.LoadSigner(pemBytes, "RS256", "k2")
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}

	txt := parseKeygenTXT(t, stdout)
	rec, err := signing.ParseKeyRecord(txt)
	if err != nil {
		t.Fatalf("ParseKeyRecord(%q): %v", txt, err)
	}
	if rec.Algorithm != "RS256" {
		t.Errorf("TXT algorithm = %s, want RS256", rec.Algorithm)
	}
	want, _ := signer.PublicKeyRecord()
	if !signing.KeysEqual(rec.PublicKey, want.PublicKey) {
		t.Error("TXT public key does not match the generated private key")
	}
}

func TestKeygenSignVerifyRoundTrip(t *testing.T) {
	// End-to-end: sign a body with the generated key, publish the printed
	// TXT record through a static resolver, and verify the signed body.
	stdout, keyPath := runKeygen(t, "--domain", "sender.example")

	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	signer, err := signing.LoadSigner(pemBytes, "ES256", "k1")
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}

	body := map[string]interface{}{"version": "1.0", "sender": "agent@sender.example"}
	sig, err := signer.SignBody(body)
	if err != nil {
		t.Fatalf("SignBody: %v", err)
	}
	body["signature"] = map[string]interface{}{
		"algorithm": sig.Algorithm,
		"keyid":     sig.KeyID,
		"value":     sig.Value,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	txt := parseKeygenTXT(t, stdout)
	resolver := &staticTXTResolver{records: map[string][]string{
		"k1._amtpkey.sender.example": {txt},
	}}
	verifier := signing.NewVerifier(resolver)
	verification, err := verifier.VerifyBody(context.Background(), raw)
	if err != nil {
		t.Fatalf("VerifyBody: %v", err)
	}
	if verification.Result != signing.ResultVerified {
		t.Errorf("verification result = %s, want verified", verification.Result)
	}
}

// staticTXTResolver is a signing.KeyResolver backed by a fixed owner->TXT map.
type staticTXTResolver struct {
	records map[string][]string
}

func (r *staticTXTResolver) ResolveKeyTXT(_ context.Context, domain, selector string) ([]string, error) {
	return r.records[selector+"._amtpkey."+domain], nil
}

func TestKeygenRefusesOverwrite(t *testing.T) {
	outDir := t.TempDir()
	keyFile := filepath.Join(outDir, "private.pem")
	if err := os.WriteFile(keyFile, []byte("existing"), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	_, _, err := runCLI(t, "http://127.0.0.1:0", nil,
		"keygen", "--domain", "sender.example", "--out", keyFile)
	if err == nil {
		t.Fatal("expected error when output file exists")
	}
	if !strings.Contains(err.Error(), "exists") {
		t.Errorf("error should mention existing file, got: %v", err)
	}
	// The existing file must be untouched.
	data, _ := os.ReadFile(keyFile)
	if string(data) != "existing" {
		t.Error("keygen overwrote an existing file")
	}
}

func TestKeygenInvalidSelector(t *testing.T) {
	outDir := t.TempDir()
	keyFile := filepath.Join(outDir, "private.pem")
	_, _, err := runCLI(t, "http://127.0.0.1:0", nil,
		"keygen", "--domain", "sender.example", "--key-id", "Bad_Key", "--out", keyFile)
	if err == nil {
		t.Fatal("expected error for invalid selector")
	}
	if !strings.Contains(err.Error(), "key-id") && !strings.Contains(err.Error(), "selector") {
		t.Errorf("error should mention the key-id problem, got: %v", err)
	}
}

func TestKeygenInvalidDomain(t *testing.T) {
	outDir := t.TempDir()
	keyFile := filepath.Join(outDir, "private.pem")
	_, _, err := runCLI(t, "http://127.0.0.1:0", nil,
		"keygen", "--domain", "not a domain", "--out", keyFile)
	if err == nil {
		t.Fatal("expected error for invalid domain")
	}
}
