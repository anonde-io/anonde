package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/anonde-io/anonde/internal/core"
)

// bulkWrite writes many envelopes in a single transaction (one fsync) so the
// tests that need thousands of rows stay fast. exp is chosen per key by expFn.
func bulkWrite(t *testing.T, db *bolt.DB, bucket []byte, keys [][]byte, expFn func(i int) time.Time) {
	t.Helper()
	if err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		for i, key := range keys {
			enc, err := json.Marshal(envelope{Version: envelopeVersion, ExpiresAt: expFn(i), Body: []byte("x")})
			if err != nil {
				return err
			}
			if err := b.Put(key, enc); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("bulk write: %v", err)
	}
}

// TestSweepBucket_BoundedBatchesDeleteAllExpired writes more expired rows than
// a single sweep batch examines, interleaved with live rows, and verifies the
// batched sweeper reclaims every expired row while leaving live rows intact —
// i.e. batching across Cursor.Seek resume points loses nothing.
func TestSweepBucket_BoundedBatchesDeleteAllExpired(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "anonde.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	bucket := []byte(bucketVault)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	// Cross several batch boundaries: 2*sweepBatchSize plus a remainder.
	total := sweepBatchSize*2 + 250
	keys := make([][]byte, total)
	liveKeys := map[string]bool{}
	for i := 0; i < total; i++ {
		keys[i] = []byte(fmt.Sprintf("k%08d", i))
		if i%7 == 0 { // scatter live rows through the whole key range
			liveKeys[string(keys[i])] = true
		}
	}
	bulkWrite(t, db, bucket, keys, func(i int) time.Time {
		if i%7 == 0 {
			return future
		}
		return past
	})

	if err := sweepBucket(db, bucket); err != nil {
		t.Fatalf("sweepBucket: %v", err)
	}

	if got := int(bucketKeyN(db, bucket)); got != len(liveKeys) {
		t.Fatalf("post-sweep KeyN = %d, want %d live rows", got, len(liveKeys))
	}
	err = db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).ForEach(func(k, v []byte) error {
			if !liveKeys[string(k)] {
				t.Errorf("expired row survived sweep: %q", k)
			}
			var env envelope
			if json.Unmarshal(v, &env) == nil && expired(env.ExpiresAt) {
				t.Errorf("expired envelope remained on disk: %q", k)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("verify View: %v", err)
	}
}

// TestSweepBucket_AllLiveUntouched confirms a bucket with no expired rows is
// left fully intact and the batched sweep terminates.
func TestSweepBucket_AllLiveUntouched(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "anonde.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	bucket := []byte(bucketStore)
	future := time.Now().Add(time.Hour)
	n := sweepBatchSize + 10
	keys := make([][]byte, n)
	for i := 0; i < n; i++ {
		keys[i] = []byte(fmt.Sprintf("k%08d", i))
	}
	bulkWrite(t, db, bucket, keys, func(int) time.Time { return future })
	if err := sweepBucket(db, bucket); err != nil {
		t.Fatalf("sweepBucket: %v", err)
	}
	if got := int(bucketKeyN(db, bucket)); got != n {
		t.Fatalf("post-sweep KeyN = %d, want %d (all live)", got, n)
	}
}

// TestBoltVault_StatsNoTTLUsesKeyN exercises the ttl<=0 fast path: with expiry
// disabled, no row can ever be expired, so Stats() returns bbolt's KeyN
// (unique per key, so an overwrite doesn't double-count).
func TestBoltVault_StatsNoTTLUsesKeyN(t *testing.T) {
	v, _ := newTestBoltVault(t, 0)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := v.Put(ctx, "demo", core.VaultEntry{
			Token: fmt.Sprintf("T%d", i), EntityType: "EMAIL_ADDRESS", Cleartext: "x",
		}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if got := v.Stats().Entries; got != 5 {
		t.Fatalf("entries = %d, want 5", got)
	}
	// Overwriting an existing token must not change the count.
	if err := v.Put(ctx, "demo", core.VaultEntry{
		Token: "T0", EntityType: "EMAIL_ADDRESS", Cleartext: "y",
	}); err != nil {
		t.Fatalf("Put overwrite: %v", err)
	}
	if got := v.Stats().Entries; got != 5 {
		t.Fatalf("entries after overwrite = %d, want 5", got)
	}
}
