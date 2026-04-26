package operators

import "fmt"

// Replace substitutes PII with a configurable value or the entity type tag.
type Replace struct {
	// NewValue is the replacement. If empty, "<ENTITY_TYPE>" is used.
	NewValue string
}

func (r *Replace) Name() string { return "replace" }

func (r *Replace) Anonymize(_ string, entityType string) (string, error) {
	if r.NewValue != "" {
		return r.NewValue, nil
	}
	return fmt.Sprintf("<%s>", entityType), nil
}
