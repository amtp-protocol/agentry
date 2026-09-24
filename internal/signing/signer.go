/*
 * Copyright 2025 Cong Wang
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

package signing

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
)

// Errors specific to signing (as opposed to verification).
var (
	// ErrInvalidPrivateKey means the PEM key could not be parsed or violates
	// the profile's key constraints.
	ErrInvalidPrivateKey = errors.New("signing: invalid private key")
	// ErrAlreadySigned means the body already carries a signature member.
	ErrAlreadySigned = errors.New("signing: body already signed")
)

// Signer signs message bodies with the gateway's domain key.
type Signer struct {
	algorithm string
	keyID     string
	priv      crypto.PrivateKey
}

// LoadSigner parses a PEM-encoded private key (EC SEC1, RSA PKCS#1, or
// PKCS#8) and validates it against the profile: ES256 requires a P-256 EC
// key, RS256 requires an RSA key of at least 2048 bits. keyID must be a
// valid selector.
func LoadSigner(pemBytes []byte, algorithm, keyID string) (*Signer, error) {
	if algorithm != AlgES256 && algorithm != AlgRS256 {
		return nil, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidPrivateKey, algorithm)
	}
	if err := ValidateSelector(keyID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block found", ErrInvalidPrivateKey)
	}
	priv, err := parsePrivateKey(block)
	if err != nil {
		return nil, err
	}
	switch key := priv.(type) {
	case *ecdsa.PrivateKey:
		if algorithm != AlgES256 {
			return nil, fmt.Errorf("%w: EC key cannot sign %s", ErrInvalidPrivateKey, algorithm)
		}
		if key.Curve != elliptic.P256() {
			return nil, fmt.Errorf("%w: ES256 requires P-256", ErrInvalidPrivateKey)
		}
	case *rsa.PrivateKey:
		if algorithm != AlgRS256 {
			return nil, fmt.Errorf("%w: RSA key cannot sign %s", ErrInvalidPrivateKey, algorithm)
		}
		if key.N.BitLen() < minRSAModulusBits {
			return nil, fmt.Errorf("%w: RSA modulus below %d bits", ErrInvalidPrivateKey, minRSAModulusBits)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported key type %T", ErrInvalidPrivateKey, priv)
	}
	return &Signer{algorithm: algorithm, keyID: keyID, priv: priv}, nil
}

// parsePrivateKey handles the three PEM private-key encodings.
func parsePrivateKey(block *pem.Block) (crypto.PrivateKey, error) {
	switch block.Type {
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
		}
		return key, nil
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("%w: unsupported PEM block %q", ErrInvalidPrivateKey, block.Type)
	}
}

// Algorithm returns the signing algorithm.
func (s *Signer) Algorithm() string { return s.algorithm }

// KeyID returns the key selector.
func (s *Signer) KeyID() string { return s.keyID }

// SignBody signs the RFC 8785 canonical form of body, which must NOT
// contain a "signature" member. The returned Signature carries the
// algorithm, selector, and base64url-encoded signature value.
func (s *Signer) SignBody(body map[string]interface{}) (*Signature, error) {
	if _, exists := body[memberSignature]; exists {
		return nil, ErrAlreadySigned
	}
	canonical, err := Canonicalize(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedBody, err)
	}
	digest := sha256.Sum256(canonical)
	var value string
	switch s.algorithm {
	case AlgES256:
		value, err = s.signES256(digest[:])
	case AlgRS256:
		value, err = s.signRS256(digest[:])
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, s.algorithm)
	}
	if err != nil {
		return nil, err
	}
	return &Signature{Algorithm: s.algorithm, KeyID: s.keyID, Value: value}, nil
}

func (s *Signer) signES256(digest []byte) (string, error) {
	key, ok := s.priv.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("%w: not an EC key", ErrInvalidPrivateKey)
	}
	r, ss, err := ecdsa.Sign(rand.Reader, key, digest)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
	}
	sig := make([]byte, es256SignatureBytes)
	r.FillBytes(sig[:es256ScalarBytes])
	ss.FillBytes(sig[es256ScalarBytes:])
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Signer) signRS256(digest []byte) (string, error) {
	key, ok := s.priv.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("%w: not an RSA key", ErrInvalidPrivateKey)
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
	}
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

// ToMap renders the signature as the wire "signature" member value.
func (sig *Signature) ToMap() map[string]interface{} {
	return map[string]interface{}{
		memberAlgorithm: sig.Algorithm,
		memberKeyID:     sig.KeyID,
		memberValue:     sig.Value,
	}
}

// PublicKeyRecord derives the DNS key record for this signer's key. Used by
// tooling (keygen) to print the TXT value a domain must publish.
func (s *Signer) PublicKeyRecord() (*KeyRecord, error) {
	return &KeyRecord{
		Algorithm: s.algorithm,
		PublicKey: s.publicKey(),
	}, nil
}

func (s *Signer) publicKey() crypto.PublicKey {
	switch key := s.priv.(type) {
	case *ecdsa.PrivateKey:
		return &key.PublicKey
	case *rsa.PrivateKey:
		return &key.PublicKey
	}
	return nil
}

// FormatKeyRecord renders a KeyRecord as the DNS TXT value
// "v=amtpkey1;alg=...;p=..." — the inverse of ParseKeyRecord.
func FormatKeyRecord(rec *KeyRecord) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(rec.PublicKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidKeyRecord, err)
	}
	return fmt.Sprintf("v=%s;alg=%s;p=%s", keyRecordVersion, rec.Algorithm,
		base64.StdEncoding.EncodeToString(der)), nil
}

// bigIntBytes is used by tests to reconstruct P1363 components.
func bigIntBytes(i *big.Int) []byte {
	b := make([]byte, es256ScalarBytes)
	i.FillBytes(b)
	return b
}

// KeysEqual reports whether two public keys of the profile are the same key.
// It supports the ECDSA and RSA keys allowed by this profile; anything else
// compares false.
func KeysEqual(a, b crypto.PublicKey) bool {
	switch ka := a.(type) {
	case *ecdsa.PublicKey:
		kb, ok := b.(*ecdsa.PublicKey)
		return ok && ka.Equal(kb)
	case *rsa.PublicKey:
		kb, ok := b.(*rsa.PublicKey)
		return ok && ka.Equal(kb)
	default:
		return false
	}
}

// DetectPEMAlgorithm inspects a PEM private key and returns "ES256" for
// P-256 EC keys, "RS256" for RSA keys of at least 2048 bits, and an error
// otherwise. It lets callers load a key without knowing its type upfront.
func DetectPEMAlgorithm(pemBytes []byte) (string, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return "", fmt.Errorf("%w: no PEM block found", ErrInvalidPrivateKey)
	}
	priv, err := parsePrivateKey(block)
	if err != nil {
		return "", err
	}
	switch key := priv.(type) {
	case *ecdsa.PrivateKey:
		if key.Curve != elliptic.P256() {
			return "", fmt.Errorf("%w: ES256 requires P-256", ErrInvalidPrivateKey)
		}
		return AlgES256, nil
	case *rsa.PrivateKey:
		if key.N.BitLen() < minRSAModulusBits {
			return "", fmt.Errorf("%w: RSA modulus below %d bits", ErrInvalidPrivateKey, minRSAModulusBits)
		}
		return AlgRS256, nil
	default:
		return "", fmt.Errorf("%w: unsupported key type %T", ErrInvalidPrivateKey, priv)
	}
}
