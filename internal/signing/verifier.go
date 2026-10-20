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
	"context"
	"errors"
	"fmt"
)

// Result is the protocol-level verification outcome recorded on the message
// status (Section 9.3.4).
type Result string

const (
	// ResultVerified: domain signature verified successfully.
	ResultVerified Result = "verified"
	// ResultUnsigned: no signature present.
	ResultUnsigned Result = "unsigned"
	// ResultInvalid: signature present but verification failed.
	ResultInvalid Result = "invalid"
	// ResultKeyUnavailable: signing key could not be retrieved from DNS.
	ResultKeyUnavailable Result = "key_unavailable"
)

// Verification is the structured outcome of verifying one inbound body.
type Verification struct {
	Result    Result
	Signature *Signature // non-nil when a signature member was present
	Domain    string     // normalized sender domain
}

// KeyResolver retrieves the TXT strings published at
// {selector}._amtpkey.{domain}. Implementations handle DNS, caching, and
// singleflight; the signing package stays transport-agnostic.
type KeyResolver interface {
	ResolveKeyTXT(ctx context.Context, domain, selector string) ([]string, error)
}

// Verifier verifies inbound message bodies against DNS-published keys.
type Verifier struct {
	resolver KeyResolver
}

// NewVerifier creates a Verifier backed by resolver.
func NewVerifier(resolver KeyResolver) *Verifier {
	return &Verifier{resolver: resolver}
}

// VerifyBody runs the Section 9.3.4 algorithm on a raw request body:
//
//  1. Decode as I-JSON (duplicate keys, trailing data, number range).
//  2. Extract and shape-validate the signature object, if present.
//  3. Validate the keyid selector syntax (before any DNS query).
//  4. Resolve the key record and check the exactly-one rule.
//  5. Canonicalize the body minus "signature" and verify.
//
// The returned Verification carries the typed result; the error (if any) is
// the underlying cause for logging only, never for API responses.
func (v *Verifier) VerifyBody(ctx context.Context, raw []byte) (*Verification, error) {
	body, err := DecodeBody(raw)
	if err != nil {
		return &Verification{Result: ResultInvalid}, err
	}
	return v.VerifyDecodedBody(ctx, body)
}

// VerifyDecodedBody verifies an already-decoded body map. The map must come
// from DecodeBody (I-JSON validated, numbers as float64).
func (v *Verifier) VerifyDecodedBody(ctx context.Context, body map[string]interface{}) (*Verification, error) {
	domain, err := ExtractSenderDomain(body)
	if err != nil {
		return &Verification{Result: ResultInvalid, Domain: domain}, err
	}
	sigRaw, hasSig := body[memberSignature]
	if !hasSig {
		return &Verification{Result: ResultUnsigned, Domain: domain}, nil
	}
	sigMap, ok := sigRaw.(map[string]interface{})
	if !ok {
		return &Verification{Result: ResultInvalid, Domain: domain},
			fmt.Errorf("%w: signature is not an object", ErrMalformedSignature)
	}
	sig, err := ParseSignature(sigMap)
	if err != nil {
		return &Verification{Result: ResultInvalid, Domain: domain}, err
	}
	if err := ValidateSelector(sig.KeyID); err != nil {
		return &Verification{Result: ResultInvalid, Domain: domain, Signature: sig}, err
	}
	txts, err := v.resolver.ResolveKeyTXT(ctx, domain, sig.KeyID)
	if err != nil {
		return &Verification{Result: ResultKeyUnavailable, Domain: domain, Signature: sig}, err
	}
	record, err := SelectKeyRecord(txts)
	if err != nil {
		if errors.Is(err, ErrKeyUnavailable) {
			return &Verification{Result: ResultKeyUnavailable, Domain: domain, Signature: sig}, err
		}
		return &Verification{Result: ResultInvalid, Domain: domain, Signature: sig}, err
	}
	// Verify over the body with the signature member removed.
	sansSig := make(map[string]interface{}, len(body)-1)
	for k, val := range body {
		if k != memberSignature {
			sansSig[k] = val
		}
	}
	if err := Verify(sansSig, sig, record); err != nil {
		return &Verification{Result: ResultInvalid, Domain: domain, Signature: sig}, err
	}
	return &Verification{Result: ResultVerified, Domain: domain, Signature: sig}, nil
}
