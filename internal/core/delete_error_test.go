package core

import (
	"context"
	"errors"
	"testing"

	"github.com/anonde-io/anonde/internal/metrics"
)

// erroringStore is a Store whose Get returns a real backend error rather
// than a not-found sentinel, standing in for a closed DB / corruption /
// I/O failure.
type erroringStore struct{ err error }

func (s erroringStore) Put(context.Context, StoreRecord) error { return s.err }
func (s erroringStore) Get(context.Context, string, string) (StoreRecord, error) {
	return StoreRecord{}, s.err
}
func (s erroringStore) Delete(context.Context, string, string) (bool, error) { return false, s.err }
func (s erroringStore) Stats() StoreStats                                    { return StoreStats{} }

// TestDeleteAnonymization_RealGetErrorPropagates is the regression for
// "Delete treats every store Get error as missing": a genuine backend
// failure must surface, not be masked as an idempotent no-op delete that
// leaves records + vault entries behind.
func TestDeleteAnonymization_RealGetErrorPropagates(t *testing.T) {
	backendErr := errors.New("db unavailable")
	svc := NewService(nil, nil, newTestVault(), erroringStore{err: backendErr}, allowAllPolicy{}, metrics.NewNoop())

	res, err := svc.DeleteAnonymization(context.Background(), "acme", "doc-1")
	if err == nil {
		t.Fatalf("expected the backend error to propagate, got nil (silent no-op masks failure)")
	}
	if !errors.Is(err, backendErr) {
		t.Fatalf("expected wrapped backend error, got %v", err)
	}
	if res.Deleted {
		t.Fatalf("expected Deleted=false on error path, got true")
	}
}

// TestDeleteAnonymization_NotFoundIsNoOp keeps the idempotent contract:
// a genuinely missing record (ErrRecordNotFound) is a clean no-op.
func TestDeleteAnonymization_NotFoundIsNoOp(t *testing.T) {
	svc := NewService(nil, nil, newTestVault(), newTestStore(), allowAllPolicy{}, metrics.NewNoop())

	res, err := svc.DeleteAnonymization(context.Background(), "acme", "never-existed")
	if err != nil {
		t.Fatalf("missing-record delete should be a no-op, got %v", err)
	}
	if res.Deleted {
		t.Fatalf("expected Deleted=false for missing record")
	}
}
