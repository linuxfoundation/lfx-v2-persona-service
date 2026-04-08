// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	// freshThreshold is the age below which a cached entry is considered fresh.
	freshThreshold = 10 * time.Minute

	// Key prefixes for the persona-cache KV bucket.
	memberIDKeyPrefix    = "cdp.member_id."
	affiliationsKeyPrefix = "cdp.affiliations."
)

// Cache provides NATS KV backed caching for CDP data with
// stale-while-revalidate semantics per the architecture spec.
type Cache struct {
	kv jetstream.KeyValue
}

// NewCache creates a Cache. If kv is nil, all operations are no-ops (cache disabled).
func NewCache(kv jetstream.KeyValue) *Cache {
	return &Cache{kv: kv}
}

// Enabled returns true when the backing KV store is available.
func (c *Cache) Enabled() bool {
	return c.kv != nil
}

// CacheResult describes the outcome of a cache lookup.
type CacheResult struct {
	// Hit is true when data was found in cache.
	Hit bool
	// Stale is true when the entry is older than freshThreshold and a
	// background refresh should be triggered.
	Stale bool
}

// GetMemberID retrieves a cached CDP memberId for the given username.
func (c *Cache) GetMemberID(ctx context.Context, username string) (string, CacheResult, error) {
	if !c.Enabled() {
		return "", CacheResult{}, nil
	}

	key := memberIDKeyPrefix + username
	entry, err := c.kv.Get(ctx, key)
	if err != nil {
		// Key not found is not an error — it's a cache miss.
		if isNotFound(err) {
			return "", CacheResult{}, nil
		}
		return "", CacheResult{}, fmt.Errorf("kv get %s: %w", key, err)
	}

	age := time.Since(entry.Created())
	result := CacheResult{
		Hit:   true,
		Stale: age >= freshThreshold,
	}

	return string(entry.Value()), result, nil
}

// PutMemberID stores a CDP memberId in the cache.
func (c *Cache) PutMemberID(ctx context.Context, username, memberID string) {
	if !c.Enabled() {
		return
	}
	key := memberIDKeyPrefix + username
	if _, err := c.kv.PutString(ctx, key, memberID); err != nil {
		slog.WarnContext(ctx, "cache put member_id failed", "key", key, "error", err)
	}
}

// GetAffiliations retrieves cached project affiliations for a CDP memberId.
func (c *Cache) GetAffiliations(ctx context.Context, memberID string) ([]ProjectAffiliation, CacheResult, error) {
	if !c.Enabled() {
		return nil, CacheResult{}, nil
	}

	key := affiliationsKeyPrefix + memberID
	entry, err := c.kv.Get(ctx, key)
	if err != nil {
		if isNotFound(err) {
			return nil, CacheResult{}, nil
		}
		return nil, CacheResult{}, fmt.Errorf("kv get %s: %w", key, err)
	}

	var affiliations []ProjectAffiliation
	if err := json.Unmarshal(entry.Value(), &affiliations); err != nil {
		slog.WarnContext(ctx, "cache unmarshal affiliations failed, treating as miss", "key", key, "error", err)
		return nil, CacheResult{}, nil
	}

	age := time.Since(entry.Created())
	result := CacheResult{
		Hit:   true,
		Stale: age >= freshThreshold,
	}

	return affiliations, result, nil
}

// PutAffiliations stores project affiliations in the cache.
func (c *Cache) PutAffiliations(ctx context.Context, memberID string, affiliations []ProjectAffiliation) {
	if !c.Enabled() {
		return
	}
	key := affiliationsKeyPrefix + memberID
	data, err := json.Marshal(affiliations)
	if err != nil {
		slog.WarnContext(ctx, "cache marshal affiliations failed", "key", key, "error", err)
		return
	}
	if _, err := c.kv.Put(ctx, key, data); err != nil {
		slog.WarnContext(ctx, "cache put affiliations failed", "key", key, "error", err)
	}
}

func isNotFound(err error) bool {
	return err == jetstream.ErrKeyNotFound
}
