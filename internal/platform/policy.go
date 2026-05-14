package platform

import (
	"context"

	"github.com/anonde-io/anonde/internal/core"
)

// StaticPolicy allows all detokenize requests unconditionally.
// Replace with a real implementation that checks roles, audit logs, etc.
type StaticPolicy struct{}

func (p *StaticPolicy) AllowDetokenize(_ context.Context, _ core.DetokenizeRequest) error {
	return nil
}
