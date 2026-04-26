package platform

import "context"

// StaticPolicy allows all detokenize requests unconditionally.
// Replace with a real implementation that checks roles, audit logs, etc.
type StaticPolicy struct{}

func (p *StaticPolicy) AllowDetokenize(_ context.Context, _ DetokenizeRequest) error {
	return nil
}
