package api

import (
	"context"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/internal/core"
	"github.com/anonde-io/anonde/internal/store"
)

// Shared test fixtures for the api package.
//
// allowAllPolicy is a permissive PolicyAuthorizer that mirrors the
// same-named helper in internal/core/service_test.go; api tests need
// their own copy because the policy package's Static type is what gets
// wired in production but, like the in-memory Vault/Store, it's a stub
// equivalent for test purposes.
type allowAllPolicy struct{}

func (allowAllPolicy) AllowDetokenize(context.Context, core.DetokenizeRequest) error {
	return nil
}

func newTestService() *core.Service {
	return core.NewService(
		anonde.DefaultAnalyzerEngine(),
		anonde.DefaultAnonymizerEngine(),
		store.NewMemoryVault(),
		store.NewMemoryStore(),
		allowAllPolicy{},
	)
}
