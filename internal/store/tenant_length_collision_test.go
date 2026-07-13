package store

import (
	"bytes"
	"context"
	"testing"

	"github.com/anonde-io/anonde/internal/core"
)

// TestCompositeKey_LengthPrefixDisambiguatesNUL proves the length-prefix
// encoding keeps distinct (tenant, field) pairs distinct even when the
// field or tenant embeds the NUL separator byte. The classic length-less
// "tenant\x00field" scheme collides here: (tenant "a", field "\x00b") and
// (tenant "a\x00", field "b") both flatten to a\x00\x00b. The 2-byte
// length prefix pins the tenant boundary, so the keys stay distinct.
func TestCompositeKey_LengthPrefixDisambiguatesNUL(t *testing.T) {
	k1 := compositeKey("a", "\x00b")
	k2 := compositeKey("a\x00", "b")
	if bytes.Equal(k1, k2) {
		t.Fatalf("NUL-embedded pairs collided: %x == %x", k1, k2)
	}
}

// TestCompositeKey_WrapIsTheSoleCollisionVector characterises exactly why
// the fix bounds identifier length at the service layer rather than
// re-encoding the on-disk key: the ONLY way to force two distinct
// (tenant, field) pairs to the same key bytes is the uint16 tenant-length
// wrap, which needs a >64 KiB tenant_id. Two tenants whose lengths differ
// by 65536 share a BE16 prefix; a crafted field then absorbs the extra
// tenant bytes. Bounding tenant_id well under 65536 (maxIdentifierBytes)
// makes this unconstructable, closing the vector with no format change.
func TestCompositeKey_WrapIsTheSoleCollisionVector(t *testing.T) {
	// t1 length 1; t2 length 65537 → both BE16-encode to 0x0001.
	t1 := "A"
	x := string(bytes.Repeat([]byte("Z"), 65535))
	t2 := "A" + "\x00" + x // len = 1 + 1 + 65535 = 65537
	f2 := "field2"
	f1 := x + "\x00" + f2

	if !bytes.Equal(compositeKey(t1, f1), compositeKey(t2, f2)) {
		t.Fatalf("expected the uint16 wrap to collide these crafted pairs; encoding assumptions changed")
	}
	// The colliding tenant is far over the service cap, so the invariant
	// (every stored tenant_id < 65536) makes the wrap unreachable in
	// practice — this test documents the vector, the service layer bounds it.
	if len(t2) <= 65535 {
		t.Fatalf("wrap-tenant should exceed the uint16 range, got %d bytes", len(t2))
	}
}

// nulPairs are two (tenant, field) pairs that a naive length-less scheme
// would smear together, plus one multi-KB (but sub-wrap) identifier, used
// to exercise every backend below.
type nulPair struct{ tenant, field, clear string }

func nulTestPairs() []nulPair {
	long := string(bytes.Repeat([]byte("L"), 8192)) // multi-KB, well under the wrap
	return []nulPair{
		{"a", "\x00b", "first"},
		{"a\x00", "b", "second"},
		{"t\x00enant", "id\x00value", "third"},
		{long, "id", "long-tenant"},
		{"tenant", long, "long-field"},
	}
}

// TestVault_NULAndLongIdentifiersDistinct round-trips NUL-embedded and
// multi-KB identifiers through both vault backends and asserts each pair
// resolves to its own cleartext — no cross-tenant PII bleed.
func TestVault_NULAndLongIdentifiersDistinct(t *testing.T) {
	ctx := context.Background()
	boltVault, _ := newTestBoltVault(t, 0)
	backends := map[string]core.Vault{
		"memory": NewMemoryVault(),
		"bolt":   boltVault,
	}
	for name, v := range backends {
		t.Run(name, func(t *testing.T) {
			for _, p := range nulTestPairs() {
				if err := v.Put(ctx, p.tenant, core.VaultEntry{Token: p.field, EntityType: "PERSON", Cleartext: p.clear}); err != nil {
					t.Fatalf("Put(%q,%q): %v", p.tenant, p.field, err)
				}
			}
			for _, p := range nulTestPairs() {
				got, err := v.Get(ctx, p.tenant, p.field)
				if err != nil {
					t.Fatalf("Get(%q,%q): %v", p.tenant, p.field, err)
				}
				if got.Cleartext != p.clear {
					t.Fatalf("Get(%q,%q).Cleartext = %q, want %q — key collision / PII bleed", p.tenant, p.field, got.Cleartext, p.clear)
				}
			}
		})
	}
}

// TestStore_NULAndLongIdentifiersDistinct is the store-side mirror.
func TestStore_NULAndLongIdentifiersDistinct(t *testing.T) {
	ctx := context.Background()
	boltStore, _ := newTestBoltStore(t, 0)
	backends := map[string]core.Store{
		"memory": NewMemoryStore(),
		"bolt":   boltStore,
	}
	for name, s := range backends {
		t.Run(name, func(t *testing.T) {
			for _, p := range nulTestPairs() {
				if err := s.Put(ctx, core.StoreRecord{TenantID: p.tenant, ID: p.field, AnonymizedContent: p.clear}); err != nil {
					t.Fatalf("Put(%q,%q): %v", p.tenant, p.field, err)
				}
			}
			for _, p := range nulTestPairs() {
				got, err := s.Get(ctx, p.tenant, p.field)
				if err != nil {
					t.Fatalf("Get(%q,%q): %v", p.tenant, p.field, err)
				}
				if got.AnonymizedContent != p.clear {
					t.Fatalf("Get(%q,%q) = %q, want %q — key collision", p.tenant, p.field, got.AnonymizedContent, p.clear)
				}
			}
		})
	}
}
