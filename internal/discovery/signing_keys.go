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
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/amtp-protocol/agentry/internal/signing"
)

// signingKeyPrefix is the DNS label prefix for domain signing keys
// (Section 3.2.3 of the protocol: <selector>._amtpkey.<sender-domain>).
const signingKeyPrefix = "_amtpkey"

// maxSigningKeyTXTBytes bounds a single TXT string we are willing to parse;
// DNS TXT strings are inherently <=255 bytes, but a hostile or broken
// resolver could return more.
const maxSigningKeyTXTBytes = 255

// negativeCacheTTL caps how long a failed lookup is remembered, so a DNS
// blip cannot stampede, while a genuinely missing record is retried soon.
const negativeCacheTTL = 30 * time.Second

// SigningKeyCache looks up and caches domain signing-key TXT records.
// Positive results are cached for the configured TTL; failures (DNS error
// or no record) are negatively cached for min(TTL, 30s). Concurrent lookups
// of the same domain+selector are collapsed via singleflight.
//
// Note: Go's net.Resolver does not expose authoritative DNS TTLs, so the
// cache duration is the configured dns.cache_ttl, not the record's own TTL.
type SigningKeyCache struct {
	resolver txtResolver
	ttl      time.Duration

	mutex   sync.Mutex
	entries map[string]*signingKeyEntry
	group   singleflight.Group
}

// txtResolver is the minimal resolver surface the cache needs; both
// Discovery and MockDiscovery satisfy it.
type txtResolver interface {
	lookupSigningKeyTXT(ctx context.Context, owner string) ([]string, error)
}

type signingKeyEntry struct {
	txts      []string
	err       error
	expiresAt time.Time
}

// NewSigningKeyCache creates a cache over the given discovery service's
// resolver.
func NewSigningKeyCache(d *Discovery, ttl time.Duration) *SigningKeyCache {
	return &SigningKeyCache{
		resolver: d,
		ttl:      ttl,
		entries:  make(map[string]*signingKeyEntry),
	}
}

// ResolveKeyTXT implements signing.KeyResolver: it returns the TXT strings
// for <selector>._amtpkey.<domain>, with caching.
func (c *SigningKeyCache) ResolveKeyTXT(ctx context.Context, domain, selector string) ([]string, error) {
	normalized, err := signing.NormalizeDomain(domain)
	if err != nil {
		return nil, err
	}
	if err := signing.ValidateSelector(selector); err != nil {
		return nil, err
	}
	owner := selector + "." + signingKeyPrefix + "." + normalized
	key := normalized + "/" + selector

	// Positive cache.
	if txts, ok := c.getCached(key); ok {
		return txts, nil
	}
	// Negative cache.
	if err := c.getNegative(key); err != nil {
		return nil, err
	}

	v, err, _ := c.group.Do(key, func() (interface{}, error) {
		txts, err := c.resolver.lookupSigningKeyTXT(ctx, owner)
		if err != nil {
			c.putNegative(key, err)
			return nil, err
		}
		txts = filterSigningKeyTXT(txts)
		if len(txts) == 0 {
			err := fmt.Errorf("no TXT record for %s", owner)
			c.putNegative(key, err)
			return nil, err
		}
		c.putPositive(key, txts)
		return txts, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

func (c *SigningKeyCache) getCached(key string) ([]string, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.err != nil || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.txts, true
}

func (c *SigningKeyCache) getNegative(key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.err == nil || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.err
}

func (c *SigningKeyCache) putPositive(key string, txts []string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.entries[key] = &signingKeyEntry{txts: txts, expiresAt: time.Now().Add(c.ttl)}
}

func (c *SigningKeyCache) putNegative(key string, err error) {
	ttl := c.ttl
	if negativeCacheTTL < ttl {
		ttl = negativeCacheTTL
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.entries[key] = &signingKeyEntry{err: err, expiresAt: time.Now().Add(ttl)}
}

// lookupSigningKeyTXT queries <selector>._amtpkey.<domain> TXT.
func (d *Discovery) lookupSigningKeyTXT(ctx context.Context, owner string) ([]string, error) {
	txts, err := d.resolver.LookupTXT(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("DNS TXT lookup for %s failed: %w", owner, err)
	}
	return txts, nil
}

// lookupSigningKeyTXT serves mock records. The mock record map is keyed by
// the full owner name "<selector>._amtpkey.<domain>".
func (m *MockDiscovery) lookupSigningKeyTXT(_ context.Context, owner string) ([]string, error) {
	m.mockMutex.RLock()
	defer m.mockMutex.RUnlock()
	if txt, ok := m.signingKeyRecords[owner]; ok {
		return []string{txt}, nil
	}
	return nil, nil
}

// filterSigningKeyTXT drops oversized strings and strips the quotes DNS
// servers sometimes add.
func filterSigningKeyTXT(txts []string) []string {
	out := make([]string, 0, len(txts))
	for _, txt := range txts {
		if len(txt) > maxSigningKeyTXTBytes {
			continue
		}
		out = append(out, strings.Trim(txt, "\""))
	}
	return out
}
