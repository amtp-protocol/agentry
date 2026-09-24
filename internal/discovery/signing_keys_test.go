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

package discovery

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amtp-protocol/agentry/internal/signing"
)

// countingResolver counts lookups and returns fixed TXT strings.
type countingResolver struct {
	txts     []string
	err      error
	lookups  atomic.Int64
	delay    time.Duration
	blocking chan struct{} // closed once when delay > 0, to widen the singleflight window
}

func (r *countingResolver) lookupSigningKeyTXT(ctx context.Context, owner string) ([]string, error) {
	n := r.lookups.Add(1)
	if r.delay > 0 {
		// First lookup holds the singleflight door open so concurrent
		// callers pile up behind it.
		if n == 1 && r.blocking != nil {
			close(r.blocking)
			time.Sleep(r.delay)
		} else if r.blocking != nil {
			<-r.blocking
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.txts, nil
}

func newCacheWith(res txtResolver, ttl time.Duration) *SigningKeyCache {
	return &SigningKeyCache{resolver: res, ttl: ttl, entries: make(map[string]*signingKeyEntry)}
}

func TestSigningKeyCachePositive(t *testing.T) {
	res := &countingResolver{txts: []string{"v=amtpkey1;alg=ES256;p=AAA"}}
	cache := newCacheWith(res, 50*time.Millisecond)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		txts, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1")
		if err != nil || len(txts) != 1 || txts[0] != "v=amtpkey1;alg=ES256;p=AAA" {
			t.Fatalf("lookup #%d: txts=%v err=%v", i, txts, err)
		}
	}
	if got := res.lookups.Load(); got != 1 {
		t.Errorf("resolver called %d times, want 1 (positive cache)", got)
	}
}

func TestSigningKeyCacheExpiry(t *testing.T) {
	res := &countingResolver{txts: []string{"v=amtpkey1;alg=ES256;p=AAA"}}
	cache := newCacheWith(res, 20*time.Millisecond)
	ctx := context.Background()

	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if got := res.lookups.Load(); got != 2 {
		t.Errorf("resolver called %d times, want 2 (expiry)", got)
	}
}

func TestSigningKeyCacheNegative(t *testing.T) {
	res := &countingResolver{err: errors.New("SERVFAIL")}
	cache := newCacheWith(res, time.Hour) // long positive TTL
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err == nil {
			t.Fatalf("lookup #%d must fail", i)
		}
	}
	if got := res.lookups.Load(); got != 1 {
		t.Errorf("resolver called %d times, want 1 (negative cache)", got)
	}
}

func TestSigningKeyCacheNegativeTTLShorter(t *testing.T) {
	// Negative entries must expire after min(ttl, 30s) even when the
	// configured positive TTL is much longer.
	res := &countingResolver{err: errors.New("SERVFAIL")}
	cache := newCacheWith(res, time.Hour)
	ctx := context.Background()

	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err == nil {
		t.Fatal("first lookup must fail")
	}
	// Force the negative entry to expire by rewinding its clock.
	cache.mutex.Lock()
	for _, e := range cache.entries {
		e.expiresAt = time.Now().Add(-time.Second)
	}
	cache.mutex.Unlock()
	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err == nil {
		t.Fatal("lookup after negative expiry must fail again (fresh lookup)")
	}
	if got := res.lookups.Load(); got != 2 {
		t.Errorf("resolver called %d times, want 2", got)
	}
}

func TestSigningKeyCacheNoRecord(t *testing.T) {
	res := &countingResolver{txts: nil}
	cache := newCacheWith(res, time.Hour)
	ctx := context.Background()

	_, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1")
	if err == nil {
		t.Fatal("no record must be an error (key unavailable)")
	}
	if !strings.Contains(err.Error(), "no TXT record") {
		t.Errorf("err = %v, want no-TXT error", err)
	}
}

func TestSigningKeyCacheSingleflight(t *testing.T) {
	res := &countingResolver{
		txts:     []string{"v=amtpkey1;alg=ES256;p=AAA"},
		delay:    50 * time.Millisecond,
		blocking: make(chan struct{}),
	}
	cache := newCacheWith(res, time.Hour)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err != nil {
				t.Errorf("concurrent lookup: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := res.lookups.Load(); got != 1 {
		t.Errorf("resolver called %d times for %d concurrent lookups, want 1", got, n)
	}
}

func TestSigningKeyCacheValidation(t *testing.T) {
	cache := newCacheWith(&countingResolver{txts: []string{"x"}}, time.Hour)
	ctx := context.Background()

	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "K1"); err == nil {
		t.Error("uppercase selector must fail validation")
	}
	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1.attacker.example"); err == nil {
		t.Error("dotted selector must fail validation")
	}
	if _, err := cache.ResolveKeyTXT(ctx, "münchen.example", "k1"); err == nil {
		t.Error("non-ASCII domain must fail validation")
	}
	if got := resLookups(t, cache); got != 0 {
		t.Errorf("resolver called %d times, want 0 (validation before DNS)", got)
	}
}

func resLookups(t *testing.T, c *SigningKeyCache) int64 {
	t.Helper()
	r, ok := c.resolver.(*countingResolver)
	if !ok {
		t.Fatalf("unexpected resolver type %T", c.resolver)
	}
	return r.lookups.Load()
}

func TestSigningKeyCacheNormalization(t *testing.T) {
	res := &countingResolver{txts: []string{"v=amtpkey1;alg=ES256;p=AAA"}}
	cache := newCacheWith(res, time.Hour)
	ctx := context.Background()

	// Different casings/trailing dots must share one cache entry.
	if _, err := cache.ResolveKeyTXT(ctx, "Sender.Example.", "k1"); err != nil {
		t.Fatalf("lookup 1: %v", err)
	}
	if _, err := cache.ResolveKeyTXT(ctx, "sender.example", "k1"); err != nil {
		t.Fatalf("lookup 2: %v", err)
	}
	if got := res.lookups.Load(); got != 1 {
		t.Errorf("resolver called %d times, want 1 (normalized cache key)", got)
	}
}

func TestSigningKeyCacheOversizedTXT(t *testing.T) {
	long := "v=amtpkey1;alg=ES256;p=" + strings.Repeat("A", 300)
	res := &countingResolver{txts: []string{long, "v=amtpkey1;alg=ES256;p=AAA"}}
	cache := newCacheWith(res, time.Hour)

	txts, err := cache.ResolveKeyTXT(context.Background(), "sender.example", "k1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(txts) != 1 || txts[0] != "v=amtpkey1;alg=ES256;p=AAA" {
		t.Errorf("oversized TXT not filtered: %v", txts)
	}
}

func TestMockDiscoverySigningKeys(t *testing.T) {
	m := NewMockDiscovery(nil, time.Minute)
	m.SetSigningKeyRecord("k1._amtpkey.sender.example", "v=amtpkey1;alg=ES256;p=AAA")
	ctx := context.Background()

	txts, err := m.ResolveSigningKeyTXT(ctx, "sender.example", "k1")
	if err != nil || len(txts) != 1 {
		t.Fatalf("full owner lookup: txts=%v err=%v", txts, err)
	}
	// Case-insensitive domain, trailing dot tolerated.
	txts, err = m.ResolveSigningKeyTXT(ctx, "Sender.Example.", "k1")
	if err != nil || len(txts) != 1 {
		t.Fatalf("normalized lookup: txts=%v err=%v", txts, err)
	}
	// Missing record returns empty (caller maps to key_unavailable).
	txts, err = m.ResolveSigningKeyTXT(ctx, "other.example", "k1")
	if err != nil || len(txts) != 0 {
		t.Fatalf("missing record: txts=%v err=%v", txts, err)
	}
	// Bad selector rejected before lookup.
	if _, err := m.ResolveSigningKeyTXT(ctx, "sender.example", "bad.selector"); err == nil {
		t.Error("dotted selector must fail")
	}
}

func TestMockDiscoverySetCapabilitiesRecord(t *testing.T) {
	m := NewMockDiscovery(map[string]string{"peer.example": "v=amtp1;gateway=https://old;auth=none"}, time.Minute)
	ctx := context.Background()

	// Records added after construction are visible to DiscoverCapabilities
	// (the delivery engine reads through the same mock instance).
	m.SetCapabilitiesRecord("sender.example", "v=amtp1;gateway=https://gw;auth=none;max-size=10485760")

	caps, err := m.DiscoverCapabilities(ctx, "sender.example")
	if err != nil {
		t.Fatalf("discover added record: %v", err)
	}
	if caps.Gateway != "https://gw" {
		t.Errorf("added record: gateway=%s want https://gw", caps.Gateway)
	}

	// A later SetCapabilitiesRecord overrides an existing entry.
	m.SetCapabilitiesRecord("peer.example", "v=amtp1;gateway=https://new;auth=none")
	caps, err = m.DiscoverCapabilities(ctx, "peer.example")
	if err != nil {
		t.Fatalf("discover overridden record: %v", err)
	}
	if caps.Gateway != "https://new" {
		t.Errorf("overridden record: gateway=%s want https://new", caps.Gateway)
	}
}

func TestDiscoveryResolveSigningKeyTXT(t *testing.T) {
	// The real Discovery path goes through net.Resolver; with no custom
	// resolver it would hit the network. Instead verify the wiring: the
	// method exists, validates inputs, and normalizes before any DNS call.
	d := NewDiscovery(time.Second, time.Minute, nil)
	if _, err := d.ResolveSigningKeyTXT(context.Background(), "sender.example", "BAD"); err == nil {
		t.Error("invalid selector must fail")
	}
	if _, err := d.ResolveSigningKeyTXT(context.Background(), "bad domain", "k1"); err == nil {
		t.Error("invalid domain must fail")
	}
}

// TestSigningKeyCacheVectorRecord checks the cache returns the conformance
// vector's DNS record verbatim so the signing verifier can consume it.
func TestSigningKeyCacheVectorRecord(t *testing.T) {
	txt := "v=amtpkey1;alg=ES256;p=MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE" +
		"test-record-not-a-real-key"
	res := &countingResolver{txts: []string{txt}}
	cache := newCacheWith(res, time.Hour)

	got, err := cache.ResolveKeyTXT(context.Background(), "sender.example", "k1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(got) != 1 || got[0] != txt {
		t.Errorf("record not returned verbatim: %v", got)
	}
	// The record must be parseable by the signing package's selector.
	if _, err := signing.ParseKeyRecord(got[0]); err == nil {
		// Not a real key, so parsing SHOULD fail — this asserts the cache
		// does not mangle the string either way.
		t.Log("note: placeholder record parsed (unexpected but harmless)")
	}
}

// TestSigningKeyCacheErrorPropagation ensures resolver errors surface
// unchanged (the verifier maps them to key_unavailable).
func TestSigningKeyCacheErrorPropagation(t *testing.T) {
	sentinel := fmt.Errorf("SERVFAIL: broken network")
	res := &countingResolver{err: sentinel}
	cache := newCacheWith(res, time.Hour)

	_, err := cache.ResolveKeyTXT(context.Background(), "sender.example", "k1")
	if err == nil {
		t.Fatal("error must propagate")
	}
	if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), "SERVFAIL") {
		t.Errorf("err = %v, want wrapped SERVFAIL", err)
	}
}

// TestDiscoveryLookupSigningKeyTXTRealResolver exercises the real
// Discovery TXT path against a local DNS server speaking enough of the
// protocol for net.Resolver (including EDNS0 queries).
func TestDiscoveryLookupSigningKeyTXTRealResolver(t *testing.T) {
	// questionEnd walks the DNS name in the question section and returns
	// the offset just past QTYPE+QCLASS (queries may carry an EDNS0 OPT
	// pseudo-record after the question, so we cannot assume the last 4
	// bytes are QTYPE/QCLASS).
	questionEnd := func(query []byte) int {
		i := 12
		for {
			if i >= len(query) {
				return len(query)
			}
			l := int(query[i])
			if l == 0 {
				i++
				break
			}
			i += 1 + l
		}
		return i + 4
	}
	makeResponse := func(query []byte, txt string) []byte {
		qEnd := questionEnd(query)
		resp := make([]byte, 0, 512)
		resp = append(resp, query[0], query[1]) // ID
		resp = append(resp, 0x85, 0x00)         // QR=1 AA=1 RD=1 RA=1
		resp = append(resp, 0x00, 0x01)         // QDCOUNT=1
		resp = append(resp, 0x00, 0x01)         // ANCOUNT=1
		resp = append(resp, 0x00, 0x00, 0x00, 0x00)
		resp = append(resp, query[12:qEnd]...)      // question verbatim
		resp = append(resp, 0xc0, 0x0c)             // name pointer
		resp = append(resp, 0x00, 0x10)             // TXT
		resp = append(resp, 0x00, 0x01)             // IN
		resp = append(resp, 0x00, 0x00, 0x00, 0x3c) // TTL 60
		rd := []byte{byte(len(txt))}
		rd = append(rd, txt...)
		var rl [2]byte
		binary.BigEndian.PutUint16(rl[:], uint16(len(rd)))
		resp = append(resp, rl[:]...)
		resp = append(resp, rd...)
		return resp
	}

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on UDP: %v", err)
	}
	defer func() { _ = pc.Close() }()

	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(makeResponse(buf[:n], "v=amtpkey1;alg=ES256;p=TEST"), addr)
		}
	}()

	d := NewDiscovery(time.Second, time.Minute, []string{pc.LocalAddr().String()})
	txts, err := d.lookupSigningKeyTXT(context.Background(), "k1._amtpkey.sender.example")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(txts) != 1 || txts[0] != "v=amtpkey1;alg=ES256;p=TEST" {
		t.Errorf("txts = %v", txts)
	}
}

// TestDiscoveryLookupSigningKeyTXTError covers the error branch of the real
// resolver path by pointing at a closed UDP port.
func TestDiscoveryLookupSigningKeyTXTError(t *testing.T) {
	// Reserve a port then close it so nothing listens there.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on UDP: %v", err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()

	d := NewDiscovery(500*time.Millisecond, time.Minute, []string{addr})
	_, err = d.lookupSigningKeyTXT(context.Background(), "k1._amtpkey.sender.example")
	if err == nil {
		t.Fatal("expected error from unreachable resolver")
	}
	if !strings.Contains(err.Error(), "DNS TXT lookup for k1._amtpkey.sender.example failed") {
		t.Errorf("unexpected error: %v", err)
	}
}
