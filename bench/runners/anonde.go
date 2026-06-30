// runner_anonde reads a corpus JSONL, runs anonde on each doc, and
// writes a per-doc findings JSONL. Single source of truth across the
// bench matrix — every corpus's Makefile invokes this binary via
// `go run -tags ner ./bench/runners/anonde.go ...`.
//
// Corpus shape is the contract:
//
//   corpus.jsonl:  {"id": "...", "text": "...", "entities": [...]}
//   anonde.jsonl:  {"id": "...", "engine": "...", "findings": [...], "duration_ms": ...}
//
// Offsets in findings are codepoint indices (Python convention), so the
// comparator can compare them against gold annotations that came from
// Python-tokenised sources (GraSCCo INCEpTION JSON, ai4privacy, etc.).
//
// Backends:
//
//   patterns-only   no NER (DisableNER=true) — fastest, no model
//   gliner          GLiNER zero-shot NER, span decoder (knowledgator/gliner-pii-base-v1.0)
//   gliner-flat     GLiNER zero-shot NER, flat / token decoder
//                   (knowledgator/gliner-pii-large-v1.0 and other 4-input exports)
//
// Optional fold-for-parity mode (--fold-parity-labels) collapses
// STREET_ADDRESS / POSTAL_CODE to LOCATION; needed for the ai4privacy
// English bench whose gold buckets street + zip under LOCATION. The
// runtime keeps its precise categories for downstream consumers
// (e.g. the German corpus uses ADDRESS as a separate canonical type).

//go:build ignore

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anonde-io/anonde"
	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/analyzer/recognizers"
)

type goldDoc struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type finding struct {
	Start int     `json:"start"`
	End   int     `json:"end"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

type output struct {
	ID         string    `json:"id"`
	Engine     string    `json:"engine"`
	Findings   []finding `json:"findings"`
	DurationMS float64   `json:"duration_ms"`
}

func main() {
	var (
		inPath     = flag.String("in", "", "input corpus jsonl")
		outPath    = flag.String("out", "", "output findings jsonl")
		threshold  = flag.Float64("threshold", 0.3, "score threshold")
		language   = flag.String("language", "de", "AnalysisConfig.Language")
		backend    = flag.String("backend", "patterns-only", "gliner|gliner-flat|patterns-only")
		modelsDir  = flag.String("models-dir", "", "model cache dir (default ~/.cache/anonde/models)")
		modelName  = flag.String("model", "", "gliner model id (empty = backend default)")
		onnxFile   = flag.String("onnx-file", "", "ONNX file path inside the HF repo (e.g. onnx/model_quantized.onnx); empty = repo default")
		glinerThr  = flag.Float64("gliner-threshold", 0, "gliner prediction threshold (0 = recognizer default, ~0.40)")
		ortLibPath = flag.String("ort-library", "", "onnxruntime shared library path (gliner backend; empty = system default)")
		autoDL     = flag.Bool("auto-download", true, "auto-download model on first run")
		disableNER = flag.Bool("disable-ner", false, "force DisableNER=true regardless of backend")
		foldParity = flag.Bool("fold-parity-labels", false, "fold STREET_ADDRESS + POSTAL_CODE to LOCATION (ai4privacy gold schema)")

		// labelSet picks the GLiNER label set for every NER config this run:
		// chat|clinical|finance|legal. Default is chat; each per-corpus
		// Makefile pins its own domain (LABEL_SET ?= ...) so measurement is
		// domain-appropriate. Flag wins over $GLINER_LABEL_SET; see resolveLabelSet.
		labelSet = flag.String("label-set", "", "GLiNER label set: chat|clinical|finance|legal (empty = $GLINER_LABEL_SET, then chat)")

		// Money guard is always on (mirrors the server). The opt-in
		// structural shape filter is added by --span-filter / --strict-ner.
		strictNER     = flag.Bool("strict-ner", false, "STRICT precision profile: shape filter + raised PERSON/ORG/LOCATION/NRP floors")
		spanFilter    = flag.Bool("span-filter", false, "enable the opt-in structural-shape span filter (shapes + stoplist) on top of the money guard")
		stoplistExtra = flag.String("stoplist", "", "comma-separated extra lower-cased stoplist terms appended to the default")
		personThr     = flag.Float64("person-threshold", 0, "override PERSON class threshold (0 = leave default/STRICT)")
		orgThr        = flag.Float64("org-threshold", 0, "override ORGANIZATION class threshold (0 = leave default/STRICT)")
		locThr        = flag.Float64("location-threshold", 0, "override LOCATION class threshold (0 = leave default/STRICT)")
		nrpThr        = flag.Float64("nrp-threshold", 0, "override NRP class threshold (0 = leave default/STRICT)")
	)
	flag.Parse()

	// Resolve the STRICT precision profile into a SpanFilterConfig + a
	// per-class threshold override map, shared by every NER backend below.
	nerSpanFilter, nerClassThresholds := resolveStrictProfile(
		*strictNER, *spanFilter, *stoplistExtra,
		*personThr, *orgThr, *locThr, *nrpThr,
	)
	if *inPath == "" || *outPath == "" {
		log.Fatal("--in and --out required")
	}

	nerLabels, nerLabelToEntity := resolveLabelSet(*labelSet)

	// Legal label set applies its precision profile (role stoplist +
	// statute/exhibit suppressor + shapes) by default, mirroring main.go.
	if resolveLabelSetName(*labelSet) == "legal" {
		nerSpanFilter = upgradeToLegalSpanFilter(nerSpanFilter)
	}

	var (
		engine      *analyzer.AnalyzerEngine
		engineLabel string
		nerOff      bool
	)
	switch *backend {
	case "gliner":
		engine = anonde.DefaultAnalyzerEngineWithGLiNERConfig(recognizers.GLiNERConfig{
			ModelsDir:         *modelsDir,
			ModelName:         *modelName,
			OnnxFilePath:      *onnxFile,
			AutoDownload:      *autoDL,
			Threshold:         *glinerThr,
			SharedLibraryPath: *ortLibPath,
			SpanFilter:        nerSpanFilter,
			ClassThresholds:   nerClassThresholds,
			// Resolved label set (default chat; per-corpus Makefile pins its domain).
			Labels:        nerLabels,
			LabelToEntity: nerLabelToEntity,
		})
		engineLabel = "anonde-ner"
		if *modelName != "" {
			engineLabel = "anonde-ner[" + *modelName + "]"
		}
	case "gliner-flat":
		// Flat-decoder GLiNER (token-decoder variant — 4 ONNX inputs,
		// BIO start/end/inside output). Same registry shape as `gliner`
		// (all pattern recognizers + one NER recognizer), but the NER
		// slot is GLiNERFlatRecognizer. Kept as an opt-in backend for
		// ad-hoc runs against flat-decoder GLiNER variants
		// (knowledgator/gliner-pii-large-v1.0 ships a flat decoder; the
		// span-decoder recognizer used by `gliner` cannot load it).
		engine = anonde.DefaultAnalyzerEngineWithGLiNERFlatConfig(recognizers.GLiNERConfig{
			ModelsDir:         *modelsDir,
			ModelName:         *modelName,
			OnnxFilePath:      *onnxFile,
			AutoDownload:      *autoDL,
			Threshold:         *glinerThr,
			SharedLibraryPath: *ortLibPath,
			SpanFilter:        nerSpanFilter,
			ClassThresholds:   nerClassThresholds,
			// Resolved label set (default chat; per-corpus Makefile pins its domain).
			Labels:        nerLabels,
			LabelToEntity: nerLabelToEntity,
		})
		engineLabel = "anonde-gliner-flat"
		if *modelName != "" {
			engineLabel = "anonde-gliner-flat[" + *modelName + "]"
		}
	case "patterns-only", "patterns":
		engine = anonde.DefaultAnalyzerEngine()
		nerOff = true
		engineLabel = "anonde-patterns-only"
	default:
		log.Fatalf("unknown --backend %q (valid: gliner, gliner-flat, patterns-only)", *backend)
	}
	if *disableNER {
		nerOff = true
	}

	// Fail-closed NER verification for the bench matrix. A gliner* cell that
	// falls back to patterns-only (model/ONNX/libonnxruntime didn't load) is
	// still LABELLED anonde-ner, so its leak-rate is a lie and the scorer's
	// silent-fallback canary trips nondeterministically (see synth_clinical_fr
	// / meddocan_es / synth_finance_es on main). --disable-ner is the
	// intentional patterns-only path and skips this.
	if strings.HasPrefix(*backend, "gliner") && !nerOff {
		if !analyzer.HasNERRecognizer(engine) {
			log.Fatalf("--backend %s requested but no NER recognizer registered "+
				"(is this built with -tags ner?); refusing to emit patterns-only findings under a %q label",
				*backend, engineLabel)
		}
		vctx, vcancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := analyzer.VerifyNERBackend(vctx, engine); err != nil {
			vcancel()
			log.Fatalf("--backend %s failed NER verification: %v "+
				"(refusing to emit patterns-only findings mislabelled as %q)",
				*backend, err, engineLabel)
		}
		vcancel()
		log.Printf("NER backend verified for %q", engineLabel)
	}

	in, err := os.Open(*inPath)
	if err != nil {
		log.Fatalf("open input: %v", err)
	}
	defer in.Close()
	out, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create output: %v", err)
	}
	defer out.Close()

	cfg := analyzer.AnalysisConfig{
		Language:        *language,
		ScoreThreshold:  *threshold,
		RemoveConflicts: true,
		DisableNER:      nerOff,
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	docs := 0
	for scanner.Scan() {
		var doc goldDoc
		if err := json.Unmarshal(scanner.Bytes(), &doc); err != nil {
			log.Printf("skip malformed line: %v", err)
			continue
		}
		start := time.Now()
		results, err := engine.Analyze(context.Background(), doc.Text, cfg)
		if err != nil {
			log.Printf("analyze id=%s: %v", doc.ID, err)
			continue
		}
		// anonde recognizers report byte offsets; the gold corpus uses
		// codepoint offsets per Python convention. Convert here.
		findings := make([]finding, 0, len(results))
		for _, r := range results {
			ftype := r.EntityType
			if *foldParity {
				ftype = foldForParity(ftype)
			}
			findings = append(findings, finding{
				Start: utf8.RuneCountInString(doc.Text[:clamp(r.Start, len(doc.Text))]),
				End:   utf8.RuneCountInString(doc.Text[:clamp(r.End, len(doc.Text))]),
				Type:  ftype,
				Score: r.Score,
			})
		}
		dur := time.Since(start)
		_ = enc.Encode(output{
			ID:         doc.ID,
			Engine:     engineLabel,
			Findings:   findings,
			DurationMS: float64(dur.Microseconds()) / 1000.0,
		})
		docs++
		log.Printf("doc=%d id=%s spans=%d dur=%dms", docs, doc.ID, len(findings), dur.Milliseconds())
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("scan: %v", err)
	}
	log.Printf("processed %d docs (engine=%s, language=%s)", docs, engineLabel, *language)
}

// resolveStrictProfile turns the precision-profile flags into a
// SpanFilterConfig + per-class threshold map. Mirrors main.go's
// glinerSpanFilterFromEnv + glinerClassThresholdsFromEnv so the bench
// measures what the deploy runs. The money guard is always on; the opt-in
// shape filter is added by --span-filter / --strict-ner; --strict-ner
// additionally raises PERSON 0.50 / ORG,LOC,NRP 0.55; per-class flags
// override the STRICT floor for that class.
func resolveStrictProfile(strictNER, spanFilter bool, stoplist string,
	personThr, orgThr, locThr, nrpThr float64,
) (recognizers.SpanFilterConfig, map[string]float64) {
	var sf recognizers.SpanFilterConfig
	if spanFilter || strictNER {
		var extra []string
		for _, t := range strings.Split(stoplist, ",") {
			if t = strings.TrimSpace(t); t != "" {
				extra = append(extra, t)
			}
		}
		sf = recognizers.StrictSpanFilter(extra...)
	} else {
		sf = recognizers.MoneyGuardFilter()
	}

	thresholds := map[string]float64{}
	// STRICT default floors for the noisy fuzzy types.
	if strictNER {
		thresholds["PERSON"] = 0.50
		thresholds["ORGANIZATION"] = 0.55
		thresholds["LOCATION"] = 0.55
		thresholds["NRP"] = 0.55
	}
	// Explicit per-class flags win over the STRICT default.
	if personThr > 0 {
		thresholds["PERSON"] = personThr
	}
	if orgThr > 0 {
		thresholds["ORGANIZATION"] = orgThr
	}
	if locThr > 0 {
		thresholds["LOCATION"] = locThr
	}
	if nrpThr > 0 {
		thresholds["NRP"] = nrpThr
	}
	if len(thresholds) == 0 {
		thresholds = nil
	}
	if sf.Active() || thresholds != nil {
		log.Printf("strict-profile: money_guard=%v shape_filter=%v stoplist=%d thresholds=%v",
			sf.MoneyGuard, sf.Enabled, len(sf.Stoplist), thresholds)
	}
	return sf, thresholds
}

// resolveLabelSet picks the GLiNER label list + its label→entity map for every
// NER config this run. Order: --label-set flag, then $GLINER_LABEL_SET, then
// the global DEFAULT "chat". Each per-corpus Makefile self-declares its domain
// (LABEL_SET ?= clinical|finance|legal|chat), so measurement is
// domain-appropriate; only a corpus that declares nothing falls through to
// chat. An unrecognised value falls back to chat too. Mirrors
// cmd/anonde/main.go's glinerLabelSetFromEnv.
//
//	chat, default → DefaultPIILabels  / DefaultLabelToEntity (= chat, global default)
//	clinical      → ClinicalPIILabels / ClinicalLabelToEntity
//	finance       → FinancePIILabels  / FinancePIILabelToEntity
//	legal         → LegalPIILabels    / LegalPIILabelToEntity
func resolveLabelSet(flagVal string) ([]string, map[string]string) {
	set := strings.ToLower(strings.TrimSpace(flagVal))
	if set == "" {
		set = strings.ToLower(strings.TrimSpace(os.Getenv("GLINER_LABEL_SET")))
	}
	switch set {
	case "", "chat", "default":
		log.Printf("gliner label set: chat (DefaultPIILabels; global default)")
		return recognizers.DefaultPIILabels, recognizers.DefaultLabelToEntity
	case "clinical":
		log.Printf("gliner label set: clinical (ClinicalPIILabels; AGE/PROFESSION/DATE + clinical/German-insurance)")
		return recognizers.ClinicalPIILabels, recognizers.ClinicalLabelToEntity
	case "finance":
		log.Printf("gliner label set: finance (FinancePIILabels; bank/routing/IBAN/SWIFT, card+CVV, tax IDs, account/transaction IDs)")
		return recognizers.FinancePIILabels, recognizers.FinancePIILabelToEntity
	case "legal":
		log.Printf("gliner label set: legal (LegalPIILabels; identity+geography, DATE/DOB kept, case/docket/matter/contract/bar IDs, court, parties)")
		return recognizers.LegalPIILabels, recognizers.LegalPIILabelToEntity
	default:
		log.Printf("label set %q not recognised (valid: chat, clinical, finance, legal); defaulting to chat", set)
		return recognizers.DefaultPIILabels, recognizers.DefaultLabelToEntity
	}
}

// resolveLabelSetName resolves the active label-set name (flag, then
// $GLINER_LABEL_SET, then "chat") WITHOUT loading the label slices — used to
// decide whether the legal precision profile applies. Keep the resolution
// order in lock-step with resolveLabelSet.
func resolveLabelSetName(flagVal string) string {
	set := strings.ToLower(strings.TrimSpace(flagVal))
	if set == "" {
		set = strings.ToLower(strings.TrimSpace(os.Getenv("GLINER_LABEL_SET")))
	}
	switch set {
	case "clinical", "finance", "legal":
		return set
	default:
		return "chat"
	}
}

// upgradeToLegalSpanFilter folds the legal profile into an already-enabled
// span filter: sets LegalNoise and merges the legal-role stoplist on top of
// the existing one (preserving operator --stoplist extras).
func upgradeToLegalSpanFilter(sf recognizers.SpanFilterConfig) recognizers.SpanFilterConfig {
	legal := recognizers.LegalSpanFilter()
	if !sf.Enabled {
		return legal
	}
	for k := range legal.Stoplist {
		sf.Stoplist[k] = true
	}
	sf.LegalNoise = true
	return sf
}

// foldForParity normalises anonde's address-bucket entity types to LOCATION
// to match the ai4privacy gold schema. Other types pass through unchanged.
func foldForParity(t string) string {
	switch t {
	case "STREET_ADDRESS", "POSTAL_CODE":
		return "LOCATION"
	}
	return t
}

func clamp(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}
