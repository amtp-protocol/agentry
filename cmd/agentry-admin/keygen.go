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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/amtp-protocol/agentry/internal/signing"
)

// newKeygenCmd builds the `agentry-admin keygen` command, which generates a
// domain-signing key pair and prints the DNS TXT record to publish. It is a
// generation tool only: rotation orchestration is out of scope.
func newKeygenCmd(_ *Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate a domain-signing key pair",
		Long: "Generate an AMTP domain-signing key pair, write the PEM private key to --out,\n" +
			"and print the DNS TXT record to publish at <key-id>._amtpkey.<domain>.\n" +
			"ES256 (P-256) is the default; --algorithm RS256 generates a 2048-bit RSA key.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeygenCmd(cmd)
		},
	}
	cmd.Flags().String("algorithm", "ES256", "Signing algorithm: ES256 or RS256")
	cmd.Flags().String("key-id", "k1", "Key selector (DNS label)")
	cmd.Flags().String("domain", "", "Sender domain the key signs for")
	cmd.Flags().String("out", "", "Output path for the PEM private key (required)")
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func runKeygenCmd(cmd *cobra.Command) error {
	algorithm, _ := cmd.Flags().GetString("algorithm")
	keyID, _ := cmd.Flags().GetString("key-id")
	domain, _ := cmd.Flags().GetString("domain")
	outPath, _ := cmd.Flags().GetString("out")

	if algorithm != signing.AlgES256 && algorithm != signing.AlgRS256 {
		return fmt.Errorf("unsupported algorithm %q: must be ES256 or RS256", algorithm)
	}
	if err := signing.ValidateSelector(keyID); err != nil {
		return fmt.Errorf("invalid --key-id: %w", err)
	}
	normalized, err := signing.NormalizeDomain(domain)
	if err != nil {
		return fmt.Errorf("invalid --domain: %w", err)
	}

	pemBytes, err := generateKeyPEM(algorithm)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	// Refuse to clobber an existing file: a silent overwrite could destroy a
	// key that is still published in DNS.
	if _, err := os.Stat(outPath); err == nil {
		return fmt.Errorf("output file %s already exists; refusing to overwrite", outPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", outPath, err)
	}

	if dir := filepath.Dir(outPath); dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	// Private key material: 0600, no group/other access.
	if err := os.WriteFile(outPath, pemBytes, 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	// Load through the profile's own parser so a keygen bug cannot produce a
	// key the gateway would refuse at startup.
	signer, err := signing.LoadSigner(pemBytes, algorithm, keyID)
	if err != nil {
		return fmt.Errorf("generated key failed validation: %w", err)
	}
	rec, err := signer.PublicKeyRecord()
	if err != nil {
		return fmt.Errorf("derive public key record: %w", err)
	}
	txt, err := signing.FormatKeyRecord(rec)
	if err != nil {
		return fmt.Errorf("format key record: %w", err)
	}

	owner := keyID + "._amtpkey." + normalized
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Private key: %s\n", outPath)
	fmt.Fprintf(out, "Owner: %s.\n", owner)
	fmt.Fprintf(out, "TXT: %s\n", txt)
	fmt.Fprintf(out, "\nPublish the TXT record at %s. before enabling\n", owner)
	fmt.Fprintf(out, "signature.private_key_file=%s on the gateway.\n", outPath)
	return nil
}

// generateKeyPEM generates a fresh key for the given algorithm and returns it
// PEM-encoded.
func generateKeyPEM(algorithm string) ([]byte, error) {
	switch algorithm {
	case signing.AlgES256:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
	case signing.AlgRS256:
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		}), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", algorithm)
	}
}
