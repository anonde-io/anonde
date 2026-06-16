//go:build ner

package main

// buildTagsValue reports "ner" on the NER-enabled build. See
// build_tags_default.go for the patterns-only counterpart.
func init() { buildTagsLabel = func() string { return "ner" } }
