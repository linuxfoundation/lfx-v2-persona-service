// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fake jetstream.KeyValue and jetstream.KeyValueEntry implementations.
// Only Get, Put, and PutString are exercised by Cache; the rest are stubs.
// ---------------------------------------------------------------------------

type fakeKVEntry struct {
	value   []byte
	created time.Time
}

func (e *fakeKVEntry) Bucket() string                  { return "test-bucket" }
func (e *fakeKVEntry) Key() string                     { return "" }
func (e *fakeKVEntry) Value() []byte                   { return e.value }
func (e *fakeKVEntry) Revision() uint64                { return 1 }
func (e *fakeKVEntry) Created() time.Time              { return e.created }
func (e *fakeKVEntry) Delta() uint64                   { return 0 }
func (e *fakeKVEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

// fakeKV is a minimal in-memory KeyValue store.
// getErr lets individual keys return a specific error on Get.
type fakeKV struct {
	data    map[string][]byte
	created map[string]time.Time
	getErr  map[string]error
	putErr  error
}

func newFakeKV() *fakeKV {
	return &fakeKV{
		data:    make(map[string][]byte),
		created: make(map[string]time.Time),
		getErr:  make(map[string]error),
	}
}

// putAt stores a key with an explicit creation time (for staleness tests).
func (f *fakeKV) putAt(key string, value []byte, created time.Time) {
	f.data[key] = value
	f.created[key] = created
}

func (f *fakeKV) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	if err, ok := f.getErr[key]; ok {
		return nil, err
	}
	v, ok := f.data[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return &fakeKVEntry{value: v, created: f.created[key]}, nil
}

func (f *fakeKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	if f.putErr != nil {
		return 0, f.putErr
	}
	f.data[key] = value
	f.created[key] = time.Now()
	return 1, nil
}

func (f *fakeKV) PutString(_ context.Context, key string, value string) (uint64, error) {
	if f.putErr != nil {
		return 0, f.putErr
	}
	f.data[key] = []byte(value)
	f.created[key] = time.Now()
	return 1, nil
}

// --- remaining interface stubs ---

func (f *fakeKV) GetRevision(_ context.Context, _ string, _ uint64) (jetstream.KeyValueEntry, error) {
	return nil, nil
}
func (f *fakeKV) Create(_ context.Context, _ string, _ []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	return 0, nil
}
func (f *fakeKV) Update(_ context.Context, _ string, _ []byte, _ uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeKV) Delete(_ context.Context, _ string, _ ...jetstream.KVDeleteOpt) error { return nil }
func (f *fakeKV) Purge(_ context.Context, _ string, _ ...jetstream.KVDeleteOpt) error  { return nil }
func (f *fakeKV) Watch(_ context.Context, _ string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}
func (f *fakeKV) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}
func (f *fakeKV) WatchFiltered(_ context.Context, _ []string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}
func (f *fakeKV) Keys(_ context.Context, _ ...jetstream.WatchOpt) ([]string, error) {
	return nil, nil
}
func (f *fakeKV) ListKeys(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	return nil, nil
}
func (f *fakeKV) ListKeysFiltered(_ context.Context, _ ...string) (jetstream.KeyLister, error) {
	return nil, nil
}
func (f *fakeKV) History(_ context.Context, _ string, _ ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	return nil, nil
}
func (f *fakeKV) Bucket() string                                                  { return "test-bucket" }
func (f *fakeKV) PurgeDeletes(_ context.Context, _ ...jetstream.KVPurgeOpt) error { return nil }
func (f *fakeKV) Status(_ context.Context) (jetstream.KeyValueStatus, error)      { return nil, nil }

// ---------------------------------------------------------------------------
// Cache.Enabled
// ---------------------------------------------------------------------------

func TestCache_EnabledWhenKVSet(t *testing.T) {
	c := NewCache(newFakeKV())
	assert.True(t, c.Enabled())
}

func TestCache_DisabledWhenKVNil(t *testing.T) {
	c := NewCache(nil)
	assert.False(t, c.Enabled())
}

// ---------------------------------------------------------------------------
// Cache.GetMemberID
// ---------------------------------------------------------------------------

func TestCache_GetMemberID_noopWhenDisabled(t *testing.T) {
	c := NewCache(nil)
	id, result, err := c.GetMemberID(context.Background(), "alice")
	require.NoError(t, err)
	assert.Empty(t, id)
	assert.False(t, result.Hit)
}

func TestCache_GetMemberID_cacheMiss(t *testing.T) {
	c := NewCache(newFakeKV())
	id, result, err := c.GetMemberID(context.Background(), "alice")
	require.NoError(t, err)
	assert.Empty(t, id)
	assert.False(t, result.Hit)
}

func TestCache_GetMemberID_cacheHit_fresh(t *testing.T) {
	kv := newFakeKV()
	kv.putAt("cdp.member_id.alice", []byte("member-123"), time.Now())

	c := NewCache(kv)
	id, result, err := c.GetMemberID(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "member-123", id)
	assert.True(t, result.Hit)
	assert.False(t, result.Stale)
}

func TestCache_GetMemberID_cacheHit_stale(t *testing.T) {
	kv := newFakeKV()
	// Entry is 11 minutes old — exceeds the 10-minute freshThreshold.
	kv.putAt("cdp.member_id.alice", []byte("member-456"), time.Now().Add(-11*time.Minute))

	c := NewCache(kv)
	id, result, err := c.GetMemberID(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "member-456", id)
	assert.True(t, result.Hit)
	assert.True(t, result.Stale)
}

func TestCache_GetMemberID_kvError(t *testing.T) {
	kv := newFakeKV()
	kv.getErr["cdp.member_id.alice"] = errors.New("kv unavailable")

	c := NewCache(kv)
	id, result, err := c.GetMemberID(context.Background(), "alice")
	require.Error(t, err)
	assert.Empty(t, id)
	assert.False(t, result.Hit)
}

// ---------------------------------------------------------------------------
// Cache.PutMemberID
// ---------------------------------------------------------------------------

func TestCache_PutMemberID_noopWhenDisabled(t *testing.T) {
	// Must not panic when cache is disabled.
	c := NewCache(nil)
	c.PutMemberID(context.Background(), "alice", "member-789")
}

func TestCache_PutMemberID_storesValue(t *testing.T) {
	kv := newFakeKV()
	c := NewCache(kv)
	c.PutMemberID(context.Background(), "alice", "member-789")

	// Verify the value is retrievable via Get.
	id, result, err := c.GetMemberID(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "member-789", id)
	assert.True(t, result.Hit)
}

// ---------------------------------------------------------------------------
// Cache.GetAffiliations
// ---------------------------------------------------------------------------

func TestCache_GetAffiliations_noopWhenDisabled(t *testing.T) {
	c := NewCache(nil)
	affs, result, err := c.GetAffiliations(context.Background(), "member-1")
	require.NoError(t, err)
	assert.Nil(t, affs)
	assert.False(t, result.Hit)
}

func TestCache_GetAffiliations_cacheMiss(t *testing.T) {
	c := NewCache(newFakeKV())
	affs, result, err := c.GetAffiliations(context.Background(), "member-1")
	require.NoError(t, err)
	assert.Nil(t, affs)
	assert.False(t, result.Hit)
}

func TestCache_GetAffiliations_cacheHit_fresh(t *testing.T) {
	affiliations := []ProjectAffiliation{
		{ID: "aff-1", ProjectSlug: "test-project"},
	}
	data, err := json.Marshal(affiliations)
	require.NoError(t, err)

	kv := newFakeKV()
	kv.putAt("cdp.affiliations.member-1", data, time.Now())

	c := NewCache(kv)
	affs, result, err := c.GetAffiliations(context.Background(), "member-1")
	require.NoError(t, err)
	require.Len(t, affs, 1)
	assert.Equal(t, "aff-1", affs[0].ID)
	assert.True(t, result.Hit)
	assert.False(t, result.Stale)
}

func TestCache_GetAffiliations_cacheHit_stale(t *testing.T) {
	affiliations := []ProjectAffiliation{{ID: "aff-2"}}
	data, err := json.Marshal(affiliations)
	require.NoError(t, err)

	kv := newFakeKV()
	kv.putAt("cdp.affiliations.member-2", data, time.Now().Add(-15*time.Minute))

	c := NewCache(kv)
	affs, result, err := c.GetAffiliations(context.Background(), "member-2")
	require.NoError(t, err)
	require.Len(t, affs, 1)
	assert.True(t, result.Hit)
	assert.True(t, result.Stale)
}

func TestCache_GetAffiliations_malformedJSON_treatedAsMiss(t *testing.T) {
	kv := newFakeKV()
	kv.putAt("cdp.affiliations.member-3", []byte(`not-json`), time.Now())

	c := NewCache(kv)
	affs, result, err := c.GetAffiliations(context.Background(), "member-3")
	// Malformed JSON is silently treated as a cache miss — no error returned.
	require.NoError(t, err)
	assert.Nil(t, affs)
	assert.False(t, result.Hit)
}

func TestCache_GetAffiliations_kvError(t *testing.T) {
	kv := newFakeKV()
	kv.getErr["cdp.affiliations.member-4"] = errors.New("network error")

	c := NewCache(kv)
	affs, result, err := c.GetAffiliations(context.Background(), "member-4")
	require.Error(t, err)
	assert.Nil(t, affs)
	assert.False(t, result.Hit)
}

// ---------------------------------------------------------------------------
// Cache.PutAffiliations
// ---------------------------------------------------------------------------

func TestCache_PutAffiliations_noopWhenDisabled(t *testing.T) {
	// Must not panic when cache is disabled.
	c := NewCache(nil)
	c.PutAffiliations(context.Background(), "member-1", []ProjectAffiliation{{ID: "aff-1"}})
}

func TestCache_PutAffiliations_storesValue(t *testing.T) {
	kv := newFakeKV()
	c := NewCache(kv)
	input := []ProjectAffiliation{
		{ID: "aff-10", ProjectSlug: "stored-project"},
	}
	c.PutAffiliations(context.Background(), "member-10", input)

	// Verify the stored value round-trips correctly.
	affs, result, err := c.GetAffiliations(context.Background(), "member-10")
	require.NoError(t, err)
	require.Len(t, affs, 1)
	assert.Equal(t, "aff-10", affs[0].ID)
	assert.Equal(t, "stored-project", affs[0].ProjectSlug)
	assert.True(t, result.Hit)
}

// ---------------------------------------------------------------------------
// isNotFound
// ---------------------------------------------------------------------------

func TestIsNotFound_ErrKeyNotFound(t *testing.T) {
	assert.True(t, isNotFound(jetstream.ErrKeyNotFound))
}

func TestIsNotFound_OtherError(t *testing.T) {
	assert.False(t, isNotFound(errors.New("some other error")))
}

func TestIsNotFound_NilError(t *testing.T) {
	assert.False(t, isNotFound(nil))
}
