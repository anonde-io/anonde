// Package policy owns PolicyAuthorizer implementations. The interface
// itself lives in internal/core (where Service consumes it); this
// package holds the concrete implementations.
//
// Today there's only StaticPolicy, a permissive stub that allows every
// detokenize. Real RBAC / OPA / audited / org-graph variants will
// land here without changing core's contract.
package policy

import (
	"context"

	"github.com/anonde-io/anonde/internal/core"
)

// Static allows all detokenize requests unconditionally.
// Replace with a real implementation that checks roles, audit logs, etc.
type Static struct{}

func (p *Static) AllowDetokenize(_ context.Context, _ core.DetokenizeRequest) error {
	return nil
}
