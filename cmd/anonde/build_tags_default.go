//go:build !hugot

package main

// buildTagsValue overrides the package-level buildTagsLabel callback
// in default builds. Reported via the anonde_build_info Prometheus
// gauge so a dashboard can distinguish patterns-only and NER images
// without log scraping.
func init() { buildTagsLabel = func() string { return "default" } }
