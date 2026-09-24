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

// Package signing implements the AMTP Domain Signatures profile: RFC 8785
// (JCS) canonicalization of I-JSON message bodies, ES256/RS256 signing and
// verification, DNS key-record parsing, and selector/domain validation.
//
// The signature input is the complete top-level JSON object of the wire body
// with the "signature" member removed, canonicalized per RFC 8785. The
// verifier therefore operates on the raw request body, never on a
// re-serialized struct, so duplicate keys, unknown fields, and number
// formatting are all covered.
package signing

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ucarion/jcs"
)

// Algorithm names as they appear in the signature object.
const (
	AlgES256 = "ES256"
	AlgRS256 = "RS256"
)

// Signature object member names (exactly these three; no unknown members).
const (
	memberAlgorithm = "algorithm"
	memberKeyID     = "keyid"
	memberValue     = "value"
	memberSignature = "signature"
	memberSender    = "sender"
)

// Wire encodings.
const (
	// keyRecordVersion is the required value of the "v" parameter.
	keyRecordVersion = "amtpkey1"
	// minRSAModulusBits is the minimum RS256 modulus size.
	minRSAModulusBits = 2048
	// es256SignatureBytes is the fixed P1363 R||S length for P-256.
	es256SignatureBytes = 64
	// es256ScalarBytes is the fixed width of each P1363 integer.
	es256ScalarBytes = 32
)

// Errors returned by this package. They are intentionally coarse: callers
// map them to the protocol's verification results (verified/invalid/
// key_unavailable) and keep the details out of API responses.
var (
	// ErrInvalidSelector means the keyid is not a single DNS label.
	ErrInvalidSelector = errors.New("signing: invalid key selector")
	// ErrInvalidDomain means the sender domain is not a plain ASCII domain.
	ErrInvalidDomain = errors.New("signing: invalid sender domain")
	// ErrMalformedBody means the raw body is not a single I-JSON object.
	ErrMalformedBody = errors.New("signing: malformed message body")
	// ErrMalformedSignature means the signature object has the wrong shape.
	ErrMalformedSignature = errors.New("signing: malformed signature object")
	// ErrUnsupportedAlgorithm means the algorithm is not ES256/RS256.
	ErrUnsupportedAlgorithm = errors.New("signing: unsupported algorithm")
	// ErrAlgorithmMismatch means the record's algorithm differs from the signature's.
	ErrAlgorithmMismatch = errors.New("signing: algorithm mismatch")
	// ErrInvalidKeyRecord means the DNS TXT record is not a valid amtpkey1 record.
	ErrInvalidKeyRecord = errors.New("signing: invalid key record")
	// ErrInvalidPublicKey means the key bytes do not parse or are too weak.
	ErrInvalidPublicKey = errors.New("signing: invalid public key")
	// ErrVerificationFailed means the cryptographic check did not pass.
	ErrVerificationFailed = errors.New("signing: signature verification failed")
	// ErrInvalidSignatureEncoding means the base64url value is malformed.
	ErrInvalidSignatureEncoding = errors.New("signing: invalid signature encoding")
	// ErrKeyUnavailable means the signing key could not be retrieved from DNS.
	ErrKeyUnavailable = errors.New("signing: signing key unavailable")
	// ErrMultipleKeyRecords means the owner publishes more than one valid record.
	ErrMultipleKeyRecords = errors.New("signing: multiple valid key records")
)

// ValidateSelector checks that keyid is a single DNS label per the protocol
// (lowercase ASCII, 1-63 chars, no leading/trailing hyphen). This runs
// before any DNS query so a hostile keyid cannot steer resolution.
func ValidateSelector(selector string) error {
	if len(selector) < 1 || len(selector) > 63 {
		return fmt.Errorf("%w: %q", ErrInvalidSelector, selector)
	}
	for i := 0; i < len(selector); i++ {
		c := selector[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' && i > 0 && i < len(selector)-1:
		default:
			return fmt.Errorf("%w: %q", ErrInvalidSelector, selector)
		}
	}
	return nil
}

// NormalizeDomain normalizes a sender domain for comparison and DNS lookup:
// trims a trailing dot and lowercases ASCII. Non-ASCII domains are rejected
// (internationalized domains are out of scope for profile v1).
func NormalizeDomain(domain string) (string, error) {
	d := strings.TrimSuffix(domain, ".")
	if d == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidDomain)
	}
	b := []byte(d)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
			b[i] = c - 'A' + 'a'
		case c >= '0' && c <= '9':
		case c == '.' || c == '-':
		default:
			return "", fmt.Errorf("%w: non-ASCII character %q", ErrInvalidDomain, string(rune(c)))
		}
	}
	return string(b), nil
}

// KeyRecord is a parsed DNS signing-key record.
type KeyRecord struct {
	Algorithm string
	PublicKey crypto.PublicKey
}

// ParseKeyRecord parses the TXT value "v=amtpkey1;alg=ES256;p=<base64>".
// Unknown parameters are ignored (forward compatibility); duplicate
// parameters are rejected. The public key is validated for the profile:
// P-256 for ES256, >=2048-bit RSA for RS256.
func ParseKeyRecord(txt string) (*KeyRecord, error) {
	seen := map[string]bool{}
	var version, alg, p string
	for _, part := range strings.Split(txt, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue // ignore malformed fragments; required params checked below
		}
		if seen[k] {
			return nil, fmt.Errorf("%w: duplicate parameter %q", ErrInvalidKeyRecord, k)
		}
		seen[k] = true
		switch k {
		case "v":
			version = v
		case "alg":
			alg = v
		case "p":
			p = v
		}
	}
	if version != keyRecordVersion {
		return nil, fmt.Errorf("%w: unsupported version %q", ErrInvalidKeyRecord, version)
	}
	if alg != AlgES256 && alg != AlgRS256 {
		return nil, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidKeyRecord, alg)
	}
	if p == "" {
		return nil, fmt.Errorf("%w: missing public key", ErrInvalidKeyRecord)
	}
	der, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return nil, fmt.Errorf("%w: bad base64 public key", ErrInvalidKeyRecord)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("%w: bad SPKI structure", ErrInvalidKeyRecord)
	}
	if err := validatePublicKey(pub, alg); err != nil {
		return nil, err
	}
	return &KeyRecord{Algorithm: alg, PublicKey: pub}, nil
}

// SelectKeyRecord applies the "exactly one valid amtpkey1 record" rule to
// the TXT strings returned for one owner name. Zero TXT strings means no
// record (key unavailable); records that exist but yield zero valid
// amtpkey1 records, or more than one, are configuration errors the verifier
// must not silently resolve (invalid).
func SelectKeyRecord(txts []string) (*KeyRecord, error) {
	if len(txts) == 0 {
		return nil, fmt.Errorf("%w: no TXT record", ErrKeyUnavailable)
	}
	var valid []*KeyRecord
	var lastParseErr error
	for _, txt := range txts {
		rec, err := ParseKeyRecord(txt)
		if err != nil {
			lastParseErr = err
			continue
		}
		valid = append(valid, rec)
	}
	switch {
	case len(valid) == 1:
		return valid[0], nil
	case len(valid) == 0:
		return nil, fmt.Errorf("%w: no valid amtpkey1 record among %d TXT strings: %v",
			ErrInvalidKeyRecord, len(txts), lastParseErr)
	default:
		return nil, fmt.Errorf("%w: %d valid records", ErrMultipleKeyRecords, len(valid))
	}
}

// validatePublicKey enforces the profile's key constraints.
func validatePublicKey(pub crypto.PublicKey, alg string) error {
	switch alg {
	case AlgES256:
		key, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: ES256 requires an EC key", ErrInvalidPublicKey)
		}
		if key.Curve != elliptic.P256() {
			return fmt.Errorf("%w: ES256 requires P-256", ErrInvalidPublicKey)
		}
	case AlgRS256:
		key, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: RS256 requires an RSA key", ErrInvalidPublicKey)
		}
		if key.N.BitLen() < minRSAModulusBits {
			return fmt.Errorf("%w: RSA modulus below %d bits", ErrInvalidPublicKey, minRSAModulusBits)
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, alg)
	}
	return nil
}

// Canonicalize canonicalizes a parsed JSON value per RFC 8785. The input
// must already be I-JSON (see DecodeBody).
func Canonicalize(v interface{}) ([]byte, error) {
	s, err := jcs.Format(v)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// Signature is the parsed, shape-validated signature object.
type Signature struct {
	Algorithm string
	KeyID     string
	Value     string
}

// ParseSignature validates the signature object's exact shape: exactly the
// members algorithm, keyid, value; no unknown or duplicate members. The
// keyid selector syntax is validated separately (ValidateSelector) so
// callers can distinguish the failure classes.
func ParseSignature(raw map[string]interface{}) (*Signature, error) {
	if raw == nil {
		return nil, ErrMalformedSignature
	}
	if len(raw) != 3 {
		return nil, fmt.Errorf("%w: expected exactly 3 members, got %d", ErrMalformedSignature, len(raw))
	}
	algRaw, ok := raw[memberAlgorithm]
	if !ok {
		return nil, fmt.Errorf("%w: missing algorithm", ErrMalformedSignature)
	}
	alg, ok := algRaw.(string)
	if !ok || (alg != AlgES256 && alg != AlgRS256) {
		return nil, fmt.Errorf("%w: algorithm must be ES256 or RS256", ErrMalformedSignature)
	}
	keyIDRaw, ok := raw[memberKeyID]
	if !ok {
		return nil, fmt.Errorf("%w: missing keyid", ErrMalformedSignature)
	}
	keyID, ok := keyIDRaw.(string)
	if !ok || keyID == "" {
		return nil, fmt.Errorf("%w: keyid must be a non-empty string", ErrMalformedSignature)
	}
	valueRaw, ok := raw[memberValue]
	if !ok {
		return nil, fmt.Errorf("%w: missing value", ErrMalformedSignature)
	}
	value, ok := valueRaw.(string)
	if !ok || value == "" {
		return nil, fmt.Errorf("%w: value must be a non-empty string", ErrMalformedSignature)
	}
	return &Signature{Algorithm: alg, KeyID: keyID, Value: value}, nil
}

// Verify checks a signature over the canonical form of body (a parsed JSON
// object with the signature member already removed) using record.
func Verify(body map[string]interface{}, sig *Signature, record *KeyRecord) error {
	if sig.Algorithm != record.Algorithm {
		return fmt.Errorf("%w: signature says %s, record says %s", ErrAlgorithmMismatch, sig.Algorithm, record.Algorithm)
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(sig.Value)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSignatureEncoding, err)
	}
	canonical, err := Canonicalize(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedBody, err)
	}
	digest := sha256.Sum256(canonical)
	switch sig.Algorithm {
	case AlgES256:
		return verifyES256(digest[:], sigBytes, record.PublicKey)
	case AlgRS256:
		return verifyRS256(digest[:], sigBytes, record.PublicKey)
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, sig.Algorithm)
	}
}

func verifyES256(digest, sigBytes []byte, pub crypto.PublicKey) error {
	if len(sigBytes) != es256SignatureBytes {
		return fmt.Errorf("%w: ES256 signature must be %d bytes, got %d",
			ErrInvalidSignatureEncoding, es256SignatureBytes, len(sigBytes))
	}
	key, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: record key is not EC", ErrInvalidPublicKey)
	}
	r := new(big.Int).SetBytes(sigBytes[:es256ScalarBytes])
	s := new(big.Int).SetBytes(sigBytes[es256ScalarBytes:])
	if !ecdsa.Verify(key, digest, r, s) {
		return ErrVerificationFailed
	}
	return nil
}

func verifyRS256(digest, sigBytes []byte, pub crypto.PublicKey) error {
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: record key is not RSA", ErrInvalidPublicKey)
	}
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest, sigBytes); err != nil {
		return ErrVerificationFailed
	}
	return nil
}
