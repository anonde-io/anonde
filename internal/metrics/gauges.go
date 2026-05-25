package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Stats is the snapshot shape gauge sources return. Reused for both
// the vault and the store gauges — they publish the same two
// numbers. Kept here (rather than imported from core) so the metrics
// package has no upward dependency on core; cmd/anonde adapts
// core.VaultStats/core.StoreStats values into this shape at wiring
// time. Bytes=-1 means "backend does not report" — published as -1
// so dashboards can distinguish unknown from truly zero.
type Stats struct {
	Entries int64
	Bytes   int64
}

// BuildInfo identifies the running binary. Values are set once at
// startup by cmd/anonde and stamped onto anonde_build_info as labels;
// the gauge value itself is always 1, so a PromQL query like
// `sum by (version) (anonde_build_info)` enumerates running versions.
type BuildInfo struct {
	Version   string
	BuildTags string
	Backend   string
}

// gaugesCollector implements prometheus.Collector for the gauge
// family that depends on snapshotting external state at scrape
// time. Doing it on scrape (vs. counters that mutate on every
// Service call) keeps Service / Vault / Store hot paths free of
// metrics-side locking.
type gaugesCollector struct {
	vaultStats   func() Stats
	storeStats   func() Stats
	customRecogs func() int
	nerEnabled   func() bool
	build        BuildInfo

	descVaultEntries *prometheus.Desc
	descVaultBytes   *prometheus.Desc
	descStoreEntries *prometheus.Desc
	descStoreBytes   *prometheus.Desc
	descCustomRecogs *prometheus.Desc
	descNERFlag      *prometheus.Desc
	descBuildInfo    *prometheus.Desc
}

// GaugesConfig wires the snapshot-on-scrape gauges to their sources.
// Each source is a small callback so the metrics package stays
// import-cycle-free w.r.t. internal/core. Any field left nil simply
// omits the corresponding metric — useful for tests and for backends
// that can't cheaply report their state.
type GaugesConfig struct {
	Vault             func() Stats
	Store             func() Stats
	CustomRecognizers func() int
	NEREnabled        func() bool
	Build             BuildInfo
}

// RegisterGauges builds the scrape-time gauges collector against
// cfg and registers it on reg. Returns the collector so tests can
// unregister it if they need to. Safe to call once per process —
// double registration on the same registry will panic via the
// standard Prometheus duplicate-collector check.
func RegisterGauges(reg *prometheus.Registry, cfg GaugesConfig) prometheus.Collector {
	c := &gaugesCollector{
		vaultStats:   cfg.Vault,
		storeStats:   cfg.Store,
		customRecogs: cfg.CustomRecognizers,
		nerEnabled:   cfg.NEREnabled,
		build:        cfg.Build,

		descVaultEntries: prometheus.NewDesc(
			"anonde_vault_entries",
			"Approximate number of token entries currently held by the vault.",
			nil, nil,
		),
		descVaultBytes: prometheus.NewDesc(
			"anonde_vault_bytes",
			"Approximate total bytes held by the vault (-1 = backend does not report).",
			nil, nil,
		),
		descStoreEntries: prometheus.NewDesc(
			"anonde_store_entries",
			"Approximate number of anonymization records currently held by the store.",
			nil, nil,
		),
		descStoreBytes: prometheus.NewDesc(
			"anonde_store_bytes",
			"Approximate total bytes held by the store (-1 = backend does not report).",
			nil, nil,
		),
		descCustomRecogs: prometheus.NewDesc(
			"anonde_custom_recognizers",
			"Number of recognizers currently registered (the recognizer registry size).",
			nil, nil,
		),
		descNERFlag: prometheus.NewDesc(
			"anonde_ner_enabled",
			"1 when an NER recognizer (hugot|gliner|ollama) is wired into the active analyzer engine, 0 otherwise.",
			nil, nil,
		),
		descBuildInfo: prometheus.NewDesc(
			"anonde_build_info",
			"Build identity of the running binary (gauge value is always 1).",
			[]string{"version", "build_tags", "backend"}, nil,
		),
	}
	reg.MustRegister(c)
	return c
}

// Describe — Prometheus collector contract.
func (c *gaugesCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.descBuildInfo
	if c.vaultStats != nil {
		ch <- c.descVaultEntries
		ch <- c.descVaultBytes
	}
	if c.storeStats != nil {
		ch <- c.descStoreEntries
		ch <- c.descStoreBytes
	}
	if c.customRecogs != nil {
		ch <- c.descCustomRecogs
	}
	if c.nerEnabled != nil {
		ch <- c.descNERFlag
	}
}

// Collect — Prometheus collector contract. Runs on every scrape.
// Each branch is guarded so a partially-wired collector (e.g. tests
// that pass nil for some sources) doesn't produce zero-valued series
// that misrepresent the truth.
func (c *gaugesCollector) Collect(ch chan<- prometheus.Metric) {
	// build_info is always present so dashboards have a stable
	// `up`-equivalent series to pivot off.
	ch <- prometheus.MustNewConstMetric(
		c.descBuildInfo,
		prometheus.GaugeValue,
		1,
		c.build.Version, c.build.BuildTags, c.build.Backend,
	)

	if c.vaultStats != nil {
		s := c.vaultStats()
		ch <- prometheus.MustNewConstMetric(c.descVaultEntries, prometheus.GaugeValue, float64(s.Entries))
		ch <- prometheus.MustNewConstMetric(c.descVaultBytes, prometheus.GaugeValue, float64(s.Bytes))
	}
	if c.storeStats != nil {
		s := c.storeStats()
		ch <- prometheus.MustNewConstMetric(c.descStoreEntries, prometheus.GaugeValue, float64(s.Entries))
		ch <- prometheus.MustNewConstMetric(c.descStoreBytes, prometheus.GaugeValue, float64(s.Bytes))
	}
	if c.customRecogs != nil {
		ch <- prometheus.MustNewConstMetric(c.descCustomRecogs, prometheus.GaugeValue, float64(c.customRecogs()))
	}
	if c.nerEnabled != nil {
		v := 0.0
		if c.nerEnabled() {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(c.descNERFlag, prometheus.GaugeValue, v)
	}
}
