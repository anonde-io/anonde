package analyzer

import "sync"

// RecognizerRegistry manages the set of active recognizers.
type RecognizerRegistry struct {
	mu          sync.RWMutex
	recognizers []EntityRecognizer
}

// NewRecognizerRegistry returns an empty registry.
func NewRecognizerRegistry() *RecognizerRegistry {
	return &RecognizerRegistry{}
}

// Add registers one or more recognizers.
func (r *RecognizerRegistry) Add(recognizers ...EntityRecognizer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recognizers = append(r.recognizers, recognizers...)
}

// Remove unregisters a recognizer by name.
func (r *RecognizerRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.recognizers[:0]
	for _, rec := range r.recognizers {
		if rec.Name() != name {
			kept = append(kept, rec)
		}
	}
	r.recognizers = kept
}

// GetByEntity returns all recognizers that support the given entity type.
func (r *RecognizerRegistry) GetByEntity(entity string) []EntityRecognizer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []EntityRecognizer
	for _, rec := range r.recognizers {
		for _, e := range rec.SupportedEntities() {
			if e == entity {
				out = append(out, rec)
				break
			}
		}
	}
	return out
}

// GetByLanguage returns recognizers that support the given language.
func (r *RecognizerRegistry) GetByLanguage(language string) []EntityRecognizer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []EntityRecognizer
	for _, rec := range r.recognizers {
		for _, l := range rec.SupportedLanguages() {
			if l == language || l == "*" {
				out = append(out, rec)
				break
			}
		}
	}
	return out
}

// All returns a snapshot of all registered recognizers.
func (r *RecognizerRegistry) All() []EntityRecognizer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]EntityRecognizer, len(r.recognizers))
	copy(out, r.recognizers)
	return out
}
