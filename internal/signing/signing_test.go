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

package signing

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/types"
)

// vectorDir holds the cross-language conformance vectors for the domain
// signatures profile.
const vectorDir = "testdata"

// vectorScenario is the fixed scenario of the conformance vectors.
const (
	vectorSender    = "orders@sender.example"
	vectorRecipient = "payments@receiver.example"
	vectorSelector  = "k1"
)

// loadVector reads a test vector file.
func loadVector(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(vectorDir, name))
	if err != nil {
		t.Fatalf("read vector %s: %v", name, err)
	}
	return raw
}

// loadDNSRecord reads a DNS TXT vector and joins its (possibly wrapped)
// lines into the single TXT string a resolver would return.
func loadDNSRecord(t *testing.T, name string) string {
	t.Helper()
	raw := loadVector(t, name)
	var sb strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		sb.WriteString(strings.TrimSpace(line))
	}
	return sb.String()
}

// staticResolver is a KeyResolver returning fixed TXT strings.
type staticResolver struct {
	txts []string
	err  error
}

func (r *staticResolver) ResolveKeyTXT(_ context.Context, _, _ string) ([]string, error) {
	return r.txts, r.err
}

// TestVectorCanonicalization checks that decoding unsigned.json and
// canonicalizing it reproduces canonical.jcs byte-for-byte, and that the
// digest matches canonical.sha256.
func TestVectorCanonicalization(t *testing.T) {
	raw := loadVector(t, "unsigned.json")
	body, err := DecodeBody(raw)
	if err != nil {
		t.Fatalf("DecodeBody: %v", err)
	}
	canonical, err := Canonicalize(body)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	expected := bytes.TrimSpace(loadVector(t, "canonical.jcs"))
	if !bytes.Equal(expected, canonical) {
		t.Errorf("canonical form mismatch:\n got: %s\nwant: %s", canonical, expected)
	}
	digest := sha256SumHex(canonical)
	expectedDigest := strings.TrimSpace(string(loadVector(t, "canonical.sha256")))
	if digest != expectedDigest {
		t.Errorf("digest mismatch: got %s want %s", digest, expectedDigest)
	}
}

// TestVectorVerifySigned checks both signed vectors verify against their
// DNS records.
func TestVectorVerifySigned(t *testing.T) {
	for _, tc := range []struct {
		body string
		dns  string
		alg  string
	}{
		{"signed-es256.json", "dns-es256.txt", AlgES256},
		{"signed-rs256.json", "dns-rs256.txt", AlgRS256},
	} {
		t.Run(tc.alg, func(t *testing.T) {
			resolver := &staticResolver{txts: []string{loadDNSRecord(t, tc.dns)}}
			verifier := NewVerifier(resolver)
			ver, err := verifier.VerifyBody(context.Background(), loadVector(t, tc.body))
			if err != nil {
				t.Fatalf("VerifyBody: %v", err)
			}
			if ver.Result != ResultVerified {
				t.Errorf("result = %q, want verified (err=%v)", ver.Result, err)
			}
			if ver.Domain != "sender.example" {
				t.Errorf("domain = %q, want sender.example", ver.Domain)
			}
			if ver.Signature == nil || ver.Signature.KeyID != vectorSelector {
				t.Errorf("signature keyid missing or wrong: %+v", ver.Signature)
			}
		})
	}
}

// TestVectorTamperCases checks every tamper vector fails verification with
// result invalid.
func TestVectorTamperCases(t *testing.T) {
	tamperFiles := []string{
		"tamper-subject.json",
		"tamper-payload.json",
		"tamper-sender.json",
		"tamper-unknown-field.json",
		"tamper-timestamp.json",
		"tamper-idempotency-key.json",
	}
	for _, file := range tamperFiles {
		t.Run(file, func(t *testing.T) {
			resolver := &staticResolver{txts: []string{loadDNSRecord(t, "dns-es256.txt")}}
			verifier := NewVerifier(resolver)
			ver, err := verifier.VerifyBody(context.Background(), loadVector(t, file))
			if err == nil {
				t.Fatalf("tampered vector verified; result=%q", ver.Result)
			}
			if ver.Result != ResultInvalid {
				t.Errorf("result = %q, want invalid (err=%v)", ver.Result, err)
			}
		})
	}
}

// TestVectorNegativeCases covers the structured negative vectors.
func TestVectorNegativeCases(t *testing.T) {
	resolver := &staticResolver{txts: []string{loadDNSRecord(t, "dns-es256.txt")}}
	verifier := NewVerifier(resolver)
	ctx := context.Background()

	t.Run("algorithm-confusion", func(t *testing.T) {
		ver, err := verifier.VerifyBody(ctx, loadVector(t, "algorithm-confusion.json"))
		if err == nil {
			t.Fatalf("algorithm confusion verified; result=%q", ver.Result)
		}
		if ver.Result != ResultInvalid {
			t.Errorf("result = %q, want invalid", ver.Result)
		}
	})

	t.Run("duplicate-key", func(t *testing.T) {
		ver, err := verifier.VerifyBody(ctx, loadVector(t, "duplicate-key.json"))
		if err == nil {
			t.Fatalf("duplicate key accepted; result=%q", ver.Result)
		}
		if ver.Result != ResultInvalid {
			t.Errorf("result = %q, want invalid", ver.Result)
		}
	})

	t.Run("selector-injection", func(t *testing.T) {
		ver, err := verifier.VerifyBody(ctx, loadVector(t, "selector-injection.json"))
		if err == nil {
			t.Fatalf("selector injection accepted; result=%q", ver.Result)
		}
		if ver.Result != ResultInvalid {
			t.Errorf("result = %q, want invalid", ver.Result)
		}
	})

	t.Run("signature-unknown-member", func(t *testing.T) {
		ver, err := verifier.VerifyBody(ctx, loadVector(t, "signature-unknown-member.json"))
		if err == nil {
			t.Fatalf("unknown signature member accepted; result=%q", ver.Result)
		}
		if ver.Result != ResultInvalid {
			t.Errorf("result = %q, want invalid", ver.Result)
		}
	})
}

// TestVectorUnsigned checks a body without a signature member yields
// ResultUnsigned without touching the resolver.
func TestVectorUnsigned(t *testing.T) {
	verifier := NewVerifier(&staticResolver{err: fmt.Errorf("resolver must not be called")})
	ver, err := verifier.VerifyBody(context.Background(), loadVector(t, "unsigned.json"))
	if err != nil {
		t.Fatalf("VerifyBody: %v", err)
	}
	if ver.Result != ResultUnsigned {
		t.Errorf("result = %q, want unsigned", ver.Result)
	}
}

// TestVectorKeyUnavailable covers DNS failure and no-record cases.
func TestVectorKeyUnavailable(t *testing.T) {
	ctx := context.Background()
	signed := loadVector(t, "signed-es256.json")

	t.Run("dns-error", func(t *testing.T) {
		verifier := NewVerifier(&staticResolver{err: fmt.Errorf("SERVFAIL")})
		ver, err := verifier.VerifyBody(ctx, signed)
		if err == nil {
			t.Fatal("DNS error not surfaced")
		}
		if ver.Result != ResultKeyUnavailable {
			t.Errorf("result = %q, want key_unavailable", ver.Result)
		}
	})

	t.Run("no-record", func(t *testing.T) {
		verifier := NewVerifier(&staticResolver{})
		ver, err := verifier.VerifyBody(ctx, signed)
		if err == nil {
			t.Fatal("missing record not surfaced")
		}
		if ver.Result != ResultKeyUnavailable {
			t.Errorf("result = %q, want key_unavailable", ver.Result)
		}
	})
}

// TestSelectKeyRecord covers the exactly-one rule.
func TestSelectKeyRecord(t *testing.T) {
	good := loadDNSRecord(t, "dns-es256.txt")
	goodRS := loadDNSRecord(t, "dns-rs256.txt")

	if _, err := SelectKeyRecord(nil); err == nil {
		t.Error("nil TXT list must fail")
	}
	rec, err := SelectKeyRecord([]string{good})
	if err != nil || rec.Algorithm != AlgES256 {
		t.Errorf("single record: rec=%v err=%v", rec, err)
	}
	if _, err := SelectKeyRecord([]string{good, good}); err == nil {
		t.Error("two identical valid records must fail (ambiguous)")
	}
	if _, err := SelectKeyRecord([]string{good, goodRS}); err == nil {
		t.Error("two different valid records must fail")
	}
	if _, err := SelectKeyRecord([]string{"not-a-record"}); err == nil {
		t.Error("garbage record must fail")
	}
	// One valid + one broken: the spec counts valid records only, so this
	// succeeds (broken non-amtpkey1 strings are ignored).
	rec, err = SelectKeyRecord([]string{"v=amtpkey1;alg=ES256;p=", good})
	if err != nil || rec.Algorithm != AlgES256 {
		t.Errorf("one valid + one broken: rec=%v err=%v, want the valid record", rec, err)
	}
}

// TestParseKeyRecord covers record parsing edge cases.
func TestParseKeyRecord(t *testing.T) {
	good := loadDNSRecord(t, "dns-es256.txt")

	if _, err := ParseKeyRecord(good); err != nil {
		t.Errorf("good record rejected: %v", err)
	}
	// Unknown parameters ignored.
	withUnknown := strings.Replace(good, ";p=", ";future=x;p=", 1)
	if _, err := ParseKeyRecord(withUnknown); err != nil {
		t.Errorf("unknown parameter must be ignored: %v", err)
	}
	// Duplicate parameter rejected.
	dup := good + ";alg=ES256"
	if _, err := ParseKeyRecord(dup); err == nil {
		t.Error("duplicate parameter must be rejected")
	}
	// Wrong version.
	badVersion := strings.Replace(good, "amtpkey1", "amtpkey2", 1)
	if _, err := ParseKeyRecord(badVersion); err == nil {
		t.Error("wrong version must be rejected")
	}
	// Missing p.
	noP := regexpReplace(t, good, `;p=[^;]*`, "")
	if _, err := ParseKeyRecord(noP); err == nil {
		t.Error("missing p must be rejected")
	}
	// Bad base64.
	badB64 := regexpReplace(t, good, `p=[^;]*`, "p=!!!")
	if _, err := ParseKeyRecord(badB64); err == nil {
		t.Error("bad base64 must be rejected")
	}
	// Bad SPKI.
	badSPKI := regexpReplace(t, good, `p=[^;]*`, "p=AAAA")
	if _, err := ParseKeyRecord(badSPKI); err == nil {
		t.Error("bad SPKI must be rejected")
	}
	// Unsupported algorithm in record.
	badAlg := strings.Replace(good, "alg=ES256", "alg=Ed25519", 1)
	if _, err := ParseKeyRecord(badAlg); err == nil {
		t.Error("unsupported algorithm must be rejected")
	}
}

// regexpReplace is a test helper doing one regex substitution.
func regexpReplace(t *testing.T, s, pattern, repl string) string {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("compile %q: %v", pattern, err)
	}
	return re.ReplaceAllString(s, repl)
}

// TestValidateSelector covers selector syntax.
func TestValidateSelector(t *testing.T) {
	valid := []string{"k1", "k", "a-b-c", "0", "9z", strings.Repeat("a", 63), "k-1"}
	invalid := []string{
		"", "K1", "k_1", "k.1", "k 1", "-k1", "k1-", "k..1",
		strings.Repeat("a", 64), "k1._amtpkey.sender.example.attacker", "中文",
	}
	for _, s := range valid {
		if err := ValidateSelector(s); err != nil {
			t.Errorf("ValidateSelector(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range invalid {
		if err := ValidateSelector(s); err == nil {
			t.Errorf("ValidateSelector(%q) = nil, want error", s)
		}
	}
}

// TestNormalizeDomain covers domain normalization.
func TestNormalizeDomain(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Sender.Example", "sender.example", false},
		{"sender.example.", "sender.example", false},
		{"", "", true},
		{".", "", true},
		{"m\u00fcnchen.example", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeDomain(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeDomain(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeDomain(%q) err = %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDecodeBody covers I-JSON enforcement.
func TestDecodeBody(t *testing.T) {
	valid := []string{
		`{}`,
		`{"a":1}`,
		`{"a":1,"b":[1,2,{"c":null}]}`,
		`{"a":125.5}`,
		`  {"a":1}  `,
		`{"a":1e-400}`,           // subnormal underflow -> 0, in range
		`{"a":9007199254740993}`, // precision loss canonicalizes per RFC 8785
	}
	invalid := []string{
		``,
		`null`,
		`5`,
		`"str"`,
		`[1,2]`,
		`{"a":1} {"b":2}`,
		`{"a":1}x`,
		`{"a":1,}`,
		`{"a":1,"a":2}`,
		`{"x":{"a":1,"a":2}}`,
		`{"a":1e400}`,
		`{"a":-1e400}`,
		`{"s":"a","a":1,"s":2}`,
	}
	for _, raw := range valid {
		if _, err := DecodeBody([]byte(raw)); err != nil {
			t.Errorf("DecodeBody(%q) = %v, want nil", raw, err)
		}
	}
	for _, raw := range invalid {
		if _, err := DecodeBody([]byte(raw)); err == nil {
			t.Errorf("DecodeBody(%q) = nil, want error", raw)
		}
	}
}

// TestDecodeBodyNumbers checks numbers decode to float64.
func TestDecodeBodyNumbers(t *testing.T) {
	body, err := DecodeBody([]byte(`{"i":1,"f":125.5,"neg":-3}`))
	if err != nil {
		t.Fatalf("DecodeBody: %v", err)
	}
	for k, want := range map[string]float64{"i": 1, "f": 125.5, "neg": -3} {
		got, ok := body[k].(float64)
		if !ok || got != want {
			t.Errorf("body[%q] = %#v, want %v", k, body[k], want)
		}
	}
}

// TestParseSignature covers signature object shape validation.
func TestParseSignature(t *testing.T) {
	good := map[string]interface{}{
		"algorithm": "ES256", "keyid": "k1", "value": "abc",
	}
	if _, err := ParseSignature(good); err != nil {
		t.Errorf("good signature rejected: %v", err)
	}
	bad := []map[string]interface{}{
		nil,
		{"algorithm": "ES256", "keyid": "k1"},
		{"algorithm": "ES256", "keyid": "k1", "value": "abc", "x": 1},
		{"algorithm": "Ed25519", "keyid": "k1", "value": "abc"},
		{"algorithm": 1, "keyid": "k1", "value": "abc"},
		{"algorithm": "ES256", "keyid": "", "value": "abc"},
		{"algorithm": "ES256", "keyid": "k1", "value": ""},
		{"algorithm": "ES256", "keyid": "k1", "value": 1},
	}
	for _, sig := range bad {
		if _, err := ParseSignature(sig); err == nil {
			t.Errorf("ParseSignature(%v) = nil, want error", sig)
		}
	}
}

// TestSignerRoundTrip signs a body with the vector private keys and
// verifies with the vector DNS records — proving Signer and Verifier are
// inverse operations across both algorithms.
func TestSignerRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		keyPEM string
		dns    string
		alg    string
	}{
		{"es256.pem", "dns-es256.txt", AlgES256},
		{"rs256.pem", "dns-rs256.txt", AlgRS256},
	} {
		t.Run(tc.alg, func(t *testing.T) {
			pemBytes := loadVector(t, tc.keyPEM)
			signer, err := LoadSigner(pemBytes, tc.alg, vectorSelector)
			if err != nil {
				t.Fatalf("LoadSigner: %v", err)
			}
			body, err := DecodeBody(loadVector(t, "unsigned.json"))
			if err != nil {
				t.Fatalf("DecodeBody: %v", err)
			}
			sig, err := signer.SignBody(body)
			if err != nil {
				t.Fatalf("SignBody: %v", err)
			}
			if sig.Algorithm != tc.alg || sig.KeyID != vectorSelector {
				t.Errorf("sig = %+v", sig)
			}
			// Verify with the DNS record (independent of the signer's key).
			record, err := ParseKeyRecord(loadDNSRecord(t, tc.dns))
			if err != nil {
				t.Fatalf("ParseKeyRecord: %v", err)
			}
			if err := Verify(body, sig, record); err != nil {
				t.Errorf("Verify(signer output) = %v, want nil", err)
			}
			// The signature must also verify via the full Verifier path.
			signed, err := AttachWireBody(body, sig)
			if err != nil {
				t.Fatalf("AttachWireBody: %v", err)
			}
			wire, err := MarshalWireBody(signed)
			if err != nil {
				t.Fatalf("MarshalWireBody: %v", err)
			}
			verifier := NewVerifier(&staticResolver{txts: []string{loadDNSRecord(t, tc.dns)}})
			ver, err := verifier.VerifyBody(context.Background(), wire)
			if err != nil || ver.Result != ResultVerified {
				t.Errorf("VerifyBody(signer output) = %q err=%v, want verified", ver.Result, err)
			}
		})
	}
}

// TestSignerMultipleES256 checks several signatures over the same body all
// verify (ECDSA nonces are random; each must be a valid P1363 value).
func TestSignerMultipleES256(t *testing.T) {
	signer, err := LoadSigner(loadVector(t, "es256.pem"), AlgES256, vectorSelector)
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}
	body, err := DecodeBody(loadVector(t, "unsigned.json"))
	if err != nil {
		t.Fatalf("DecodeBody: %v", err)
	}
	record, err := ParseKeyRecord(loadDNSRecord(t, "dns-es256.txt"))
	if err != nil {
		t.Fatalf("ParseKeyRecord: %v", err)
	}
	for i := 0; i < 3; i++ {
		sig, err := signer.SignBody(body)
		if err != nil {
			t.Fatalf("SignBody #%d: %v", i, err)
		}
		if err := Verify(body, sig, record); err != nil {
			t.Errorf("Verify #%d: %v", i, err)
		}
	}
}

// TestSignerErrors covers signer failure modes.
func TestSignerErrors(t *testing.T) {
	if _, err := LoadSigner(loadVector(t, "es256.pem"), "Ed25519", "k1"); err == nil {
		t.Error("bad algorithm must fail")
	}
	if _, err := LoadSigner(loadVector(t, "es256.pem"), AlgES256, "K1"); err == nil {
		t.Error("bad selector must fail")
	}
	if _, err := LoadSigner([]byte("garbage"), AlgES256, "k1"); err == nil {
		t.Error("garbage PEM must fail")
	}
	if _, err := LoadSigner(loadVector(t, "es256.pem"), AlgRS256, "k1"); err == nil {
		t.Error("EC key with RS256 must fail")
	}
	if _, err := LoadSigner(loadVector(t, "rs256.pem"), AlgES256, "k1"); err == nil {
		t.Error("RSA key with ES256 must fail")
	}
	// Non-P256 EC key.
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384: %v", err)
	}
	p384PEM := marshalECPEM(t, p384)
	if _, err := LoadSigner(p384PEM, AlgES256, "k1"); err == nil {
		t.Error("P-384 key must fail")
	}
	// RSA below 2048 bits.
	smallRSA, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate small RSA: %v", err)
	}
	smallPEM := marshalRSAPEM(t, smallRSA)
	if _, err := LoadSigner(smallPEM, AlgRS256, "k1"); err == nil {
		t.Error("1024-bit RSA must fail")
	}
	// Signing a body that already has a signature.
	signer, err := LoadSigner(loadVector(t, "es256.pem"), AlgES256, "k1")
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}
	signed, err := DecodeBody(loadVector(t, "signed-es256.json"))
	if err != nil {
		t.Fatalf("DecodeBody: %v", err)
	}
	if _, err := signer.SignBody(signed); err == nil {
		t.Error("signing an already-signed body must fail")
	}
}

// TestVerifyErrors covers verification failure classification.
func TestVerifyErrors(t *testing.T) {
	body, err := DecodeBody(loadVector(t, "unsigned.json"))
	if err != nil {
		t.Fatalf("DecodeBody: %v", err)
	}
	record, err := ParseKeyRecord(loadDNSRecord(t, "dns-es256.txt"))
	if err != nil {
		t.Fatalf("ParseKeyRecord: %v", err)
	}
	rsRecord, err := ParseKeyRecord(loadDNSRecord(t, "dns-rs256.txt"))
	if err != nil {
		t.Fatalf("ParseKeyRecord: %v", err)
	}

	// Algorithm mismatch between signature and record.
	sig := &Signature{Algorithm: AlgRS256, KeyID: "k1", Value: base64.RawURLEncoding.EncodeToString(make([]byte, 64))}
	if err := Verify(body, sig, record); err == nil {
		t.Error("algorithm mismatch must fail")
	}
	// Bad base64url.
	sig = &Signature{Algorithm: AlgES256, KeyID: "k1", Value: "!!!"}
	if err := Verify(body, sig, record); err == nil {
		t.Error("bad base64url must fail")
	}
	// Wrong length ES256 signature.
	sig = &Signature{Algorithm: AlgES256, KeyID: "k1", Value: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
	if err := Verify(body, sig, record); err == nil {
		t.Error("short ES256 signature must fail")
	}
	// Valid-format but wrong signature.
	sig = &Signature{Algorithm: AlgES256, KeyID: "k1", Value: base64.RawURLEncoding.EncodeToString(make([]byte, 64))}
	if err := Verify(body, sig, record); err == nil {
		t.Error("zero signature must fail")
	}
	// ES256 signature against RSA record.
	if err := Verify(body, sig, rsRecord); err == nil {
		t.Error("ES signature against RSA record must fail")
	}
}

// TestExtractSenderDomain covers sender domain extraction.
func TestExtractSenderDomain(t *testing.T) {
	cases := []struct {
		sender  string
		want    string
		wantErr bool
	}{
		{"orders@sender.example", "sender.example", false},
		{"orders@Sender.Example.", "sender.example", false},
		{"orders@", "", true},
		{"no-at-sign", "", true},
	}
	for _, c := range cases {
		body := map[string]interface{}{"sender": c.sender}
		got, err := ExtractSenderDomain(body)
		if c.wantErr {
			if err == nil {
				t.Errorf("ExtractSenderDomain(%q) = %q, want error", c.sender, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ExtractSenderDomain(%q) = %q err=%v, want %q", c.sender, got, err, c.want)
		}
	}
	if _, err := ExtractSenderDomain(map[string]interface{}{}); err == nil {
		t.Error("missing sender must fail")
	}
	if _, err := ExtractSenderDomain(map[string]interface{}{"sender": 5}); err == nil {
		t.Error("non-string sender must fail")
	}
}

// TestBuildWireBody checks the wire body includes every protocol field,
// notably workflow_id.
func TestBuildWireBody(t *testing.T) {
	msg := testMessage()
	body, err := BuildWireBody(msg, vectorRecipient)
	if err != nil {
		t.Fatalf("BuildWireBody: %v", err)
	}
	wantFields := []string{
		"version", "message_id", "idempotency_key", "timestamp", "sender",
		"recipients", "subject", "schema", "payload", "workflow_id",
	}
	for _, f := range wantFields {
		if _, ok := body[f]; !ok {
			t.Errorf("wire body missing field %q", f)
		}
	}
	// Optional metadata fields are omitted when empty.
	for _, f := range []string{"in_reply_to", "response_type", "attachments"} {
		if _, ok := body[f]; ok {
			t.Errorf("wire body must omit empty optional field %q", f)
		}
	}
	if got := body["recipients"].([]interface{}); len(got) != 1 || got[0] != vectorRecipient {
		t.Errorf("recipients = %v, want [%s]", got, vectorRecipient)
	}
	if body["workflow_id"] != msg.WorkflowID {
		t.Errorf("workflow_id = %v, want %q", body["workflow_id"], msg.WorkflowID)
	}
	if _, ok := body["coordination"]; !ok {
		t.Error("coordination missing")
	}
	if _, ok := body["headers"]; !ok {
		t.Error("headers missing")
	}
}

// TestBuildWireBodyRejectsSigned checks relay re-signing is refused at the
// builder level.
func TestBuildWireBodyRejectsSigned(t *testing.T) {
	msg := testMessage()
	msg.Signature = &types.MessageSignature{Algorithm: AlgES256, KeyID: "k1", Value: "x"}
	if _, err := BuildWireBody(msg, vectorRecipient); err == nil {
		t.Error("building wire body for an already-signed message must fail")
	}
}

// TestBuildWireBodyPayloadRange checks out-of-range payload numbers are
// rejected rather than silently dropped.
func TestBuildWireBodyPayloadRange(t *testing.T) {
	msg := testMessage()
	msg.Payload = json.RawMessage(`{"amount":1e400}`)
	if _, err := BuildWireBody(msg, vectorRecipient); err == nil {
		t.Error("out-of-range payload number must fail")
	}
}

// TestBuildWireBodyParityWithVector checks the builder reproduces the
// vector's canonical form when given the vector's message — the explicit
// conversion test the plan requires to prevent field drift.
func TestBuildWireBodyParityWithVector(t *testing.T) {
	msg := vectorMessage(t)
	body, err := BuildWireBody(msg, vectorRecipient)
	if err != nil {
		t.Fatalf("BuildWireBody: %v", err)
	}
	canonical, err := Canonicalize(body)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	expected := bytes.TrimSpace(loadVector(t, "canonical.jcs"))
	if !bytes.Equal(canonical, expected) {
		t.Errorf("wire body canonical form mismatch:\n got: %s\nwant: %s", canonical, expected)
	}
}

// TestAttachAndMarshalWireBody covers signature insertion and marshaling.
func TestAttachAndMarshalWireBody(t *testing.T) {
	body, err := BuildWireBody(vectorMessage(t), vectorRecipient)
	if err != nil {
		t.Fatalf("BuildWireBody: %v", err)
	}
	sig := &Signature{Algorithm: AlgES256, KeyID: "k1", Value: "abc"}
	signed, err := AttachWireBody(body, sig)
	if err != nil {
		t.Fatalf("AttachWireBody: %v", err)
	}
	if _, err := AttachWireBody(signed, sig); err == nil {
		t.Error("double attach must fail")
	}
	wire, err := MarshalWireBody(signed)
	if err != nil {
		t.Fatalf("MarshalWireBody: %v", err)
	}
	var check map[string]interface{}
	if err := json.Unmarshal(wire, &check); err != nil {
		t.Fatalf("marshal output is not JSON: %v", err)
	}
	if _, ok := check["signature"]; !ok {
		t.Error("signature missing from marshaled wire body")
	}
}

// TestFormatKeyRecord checks the TXT formatter round-trips with the parser.
func TestFormatKeyRecord(t *testing.T) {
	for _, dns := range []string{"dns-es256.txt", "dns-rs256.txt"} {
		txt := loadDNSRecord(t, dns)
		rec, err := ParseKeyRecord(txt)
		if err != nil {
			t.Fatalf("ParseKeyRecord(%s): %v", dns, err)
		}
		out, err := FormatKeyRecord(rec)
		if err != nil {
			t.Fatalf("FormatKeyRecord: %v", err)
		}
		if out != txt {
			t.Errorf("round-trip mismatch:\n got: %s\nwant: %s", out, txt)
		}
	}
}

// --- helpers ---

// sha256SumHex returns the hex SHA-256 of b.
func sha256SumHex(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)
}

// vectorMessage constructs the types.Message corresponding to the
// conformance vector scenario.
func vectorMessage(t *testing.T) *types.Message {
	t.Helper()
	body, err := DecodeBody(loadVector(t, "unsigned.json"))
	if err != nil {
		t.Fatalf("DecodeBody: %v", err)
	}
	ts, err := time.Parse(time.RFC3339, body["timestamp"].(string))
	if err != nil {
		t.Fatalf("timestamp: %v", err)
	}
	return &types.Message{
		Version:        body["version"].(string),
		MessageID:      body["message_id"].(string),
		IdempotencyKey: body["idempotency_key"].(string),
		Timestamp:      ts,
		Sender:         body["sender"].(string),
		Recipients:     []string{body["recipients"].([]interface{})[0].(string)},
		Subject:        body["subject"].(string),
		Schema:         body["schema"].(string),
		Payload:        mustMarshal(t, body["payload"]),
		WorkflowID:     body["workflow_id"].(string),
	}
}

// testMessage builds a fully-populated message for wire tests.
func testMessage() *types.Message {
	return &types.Message{
		Version:        "1.0",
		MessageID:      "01987654-3210-7654-8765-432198765432",
		IdempotencyKey: "01234567-89ab-4cde-8123-456789abcdef",
		Timestamp:      time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC),
		Sender:         vectorSender,
		Recipients:     []string{vectorRecipient},
		Subject:        "Invoice 42",
		Schema:         "agntcy:commerce.order.v1",
		Coordination: &types.CoordinationConfig{
			Type:              "parallel",
			Timeout:           60,
			RequiredResponses: []string{vectorRecipient},
		},
		Headers:      map[string]interface{}{"x-test": "1"},
		Payload:      json.RawMessage(`{"order_id":"ORD-42","total":125.5,"currency":"USD"}`),
		WorkflowID:   "01987654-3210-7654-8765-432198765999",
		InReplyTo:    "",
		ResponseType: "",
	}
}

// mustMarshal marshals v or fails the test.
func mustMarshal(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// marshalECPEM encodes an EC private key as SEC1 PEM.
func marshalECPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal EC: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// marshalRSAPEM encodes an RSA private key as PKCS#1 PEM.
func marshalRSAPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// TestBuildWireBodyFullMetadata covers attachments, coordination (all
// branches), in_reply_to, and response_type conversion.
func TestBuildWireBodyFullMetadata(t *testing.T) {
	msg := testMessage()
	msg.InReplyTo = "01987654-3210-7654-8765-432198765111"
	msg.ResponseType = "workflow_response"
	msg.Attachments = []types.Attachment{{
		Filename:    "invoice.pdf",
		ContentType: "application/pdf",
		Size:        1024000,
		Hash:        "sha256:abc123",
		URL:         "https://attachments.sender.example/xyz789",
	}}
	msg.Coordination = &types.CoordinationConfig{
		Type:              "sequential",
		Timeout:           3600,
		RequiredResponses: []string{vectorRecipient},
		OptionalResponses: []string{"audit@receiver.example"},
		Sequence:          []string{vectorRecipient, "audit@receiver.example"},
		StopOnFailure:     true,
		Conditions: []types.ConditionalRule{{
			If:   "amount > 1000",
			Then: []string{"payments@receiver.example"},
			Else: []string{"auto@receiver.example"},
		}},
	}
	body, err := BuildWireBody(msg, vectorRecipient)
	if err != nil {
		t.Fatalf("BuildWireBody: %v", err)
	}
	if body["in_reply_to"] != msg.InReplyTo {
		t.Errorf("in_reply_to = %v", body["in_reply_to"])
	}
	if body["response_type"] != msg.ResponseType {
		t.Errorf("response_type = %v", body["response_type"])
	}
	atts, ok := body["attachments"].([]interface{})
	if !ok || len(atts) != 1 {
		t.Fatalf("attachments = %#v", body["attachments"])
	}
	att, ok := atts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("attachment = %#v", atts[0])
	}
	if att["size"] != float64(1024000) {
		t.Errorf("attachment size = %#v, want float64(1024000)", att["size"])
	}
	coord, ok := body["coordination"].(map[string]interface{})
	if !ok {
		t.Fatalf("coordination = %#v", body["coordination"])
	}
	wantCoord := map[string]interface{}{
		"type": "sequential", "timeout": float64(3600),
		"required_responses": []interface{}{vectorRecipient},
		"optional_responses": []interface{}{"audit@receiver.example"},
		"sequence":           []interface{}{vectorRecipient, "audit@receiver.example"},
		"stop_on_failure":    true,
	}
	for k, want := range wantCoord {
		if !reflect.DeepEqual(coord[k], want) {
			t.Errorf("coordination[%q] = %#v, want %#v", k, coord[k], want)
		}
	}
	conds, ok := coord["conditions"].([]interface{})
	if !ok || len(conds) != 1 {
		t.Fatalf("conditions = %#v", coord["conditions"])
	}
	cond, ok := conds[0].(map[string]interface{})
	if !ok {
		t.Fatalf("condition = %#v", conds[0])
	}
	if cond["if"] != "amount > 1000" {
		t.Errorf("condition if = %v", cond["if"])
	}
	// The fully-populated body must still canonicalize (all types jcs-safe).
	if _, err := Canonicalize(body); err != nil {
		t.Errorf("Canonicalize(full body): %v", err)
	}
}

// TestBuildWireBodyNilMessage covers the nil guard.
func TestBuildWireBodyNilMessage(t *testing.T) {
	if _, err := BuildWireBody(nil, vectorRecipient); err == nil {
		t.Error("nil message must fail")
	}
}

// TestSignerAccessorsAndRecord covers KeyID, PublicKeyRecord, and the
// FormatKeyRecord error paths.
func TestSignerAccessorsAndRecord(t *testing.T) {
	signer, err := LoadSigner(loadVector(t, "es256.pem"), AlgES256, vectorSelector)
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}
	if signer.KeyID() != vectorSelector {
		t.Errorf("KeyID() = %q", signer.KeyID())
	}
	if signer.Algorithm() != AlgES256 {
		t.Errorf("Algorithm() = %q", signer.Algorithm())
	}
	rec, err := signer.PublicKeyRecord()
	if err != nil {
		t.Fatalf("PublicKeyRecord: %v", err)
	}
	if rec.Algorithm != AlgES256 {
		t.Errorf("record algorithm = %q", rec.Algorithm)
	}
	txt, err := FormatKeyRecord(rec)
	if err != nil {
		t.Fatalf("FormatKeyRecord: %v", err)
	}
	if _, err := ParseKeyRecord(txt); err != nil {
		t.Errorf("own record does not parse: %v", err)
	}
	// FormatKeyRecord rejects unsupported algorithms and bad keys.
	if _, err := FormatKeyRecord(&KeyRecord{Algorithm: "Ed25519"}); err == nil {
		t.Error("unsupported algorithm must fail")
	}
	if _, err := FormatKeyRecord(&KeyRecord{Algorithm: AlgES256, PublicKey: "not-a-key"}); err == nil {
		t.Error("bad public key must fail")
	}
}

// TestBigIntBytes covers the P1363 scalar encoding helper.
func TestBigIntBytes(t *testing.T) {
	v := new(big.Int).SetBytes([]byte{1, 2, 3})
	if got := bigIntBytes(v); !bytes.Equal(got, append(make([]byte, 29), 1, 2, 3)) {
		t.Errorf("bigIntBytes = %x", got)
	}
}

// TestDetectPEMAlgorithm covers ES256, RS256, unsupported curves, weak RSA,
// and non-PEM input.
func TestDetectPEMAlgorithm(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	ecDER, err := x509.MarshalECPrivateKey(ecKey)
	if err != nil {
		t.Fatalf("marshal EC key: %v", err)
	}
	ecPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	rsaPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})

	weakRSA, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate weak RSA key: %v", err)
	}
	weakPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(weakRSA)})

	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384 key: %v", err)
	}
	p384DER, err := x509.MarshalECPrivateKey(p384Key)
	if err != nil {
		t.Fatalf("marshal P-384 key: %v", err)
	}
	p384PEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: p384DER})

	tests := []struct {
		name    string
		pem     []byte
		want    string
		wantErr bool
	}{
		{"es256", ecPEM, AlgES256, false},
		{"rs256", rsaPEM, AlgRS256, false},
		{"weak rsa", weakPEM, "", true},
		{"p384 unsupported", p384PEM, "", true},
		{"not pem", []byte("garbage"), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectPEMAlgorithm(tt.pem)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
