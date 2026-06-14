# bench/probes/span_shape — structural-shape span-filter precision probe

Model-free precision probe for the GLiNER structural-shape post-filter
(`analyzer/recognizers/span_shape_filter.go`). It proves the two claims the
filter makes, without needing CGO / libonnxruntime / the 700 MB model:

1. **Structural false positives are killed.** Model slugs (`gpt-4o`),
   UUIDs, locale codes (`en-US`), version strings (`v1.2.3`), hex/base64
   blobs, `SCREAMING_SNAKE` identifiers, dotted paths, and pure
   digit/punct surfaces that the GLiNER LARGE/flat decoder tags as
   PERSON/ORG/LOCATION/NRP/PROFESSION get rejected.
2. **Real PII recall does not regress.** Names, orgs, places,
   nationalities, professions, and ages (across EN/DE/ES/FR/asian scripts)
   are never dropped. A single dropped real-PII surface is a leak and the
   probe exits non-zero (CI canary).

This is NOT in the matrix (`make -C bench matrix`); it tests the *decision
function*, not end-to-end model output. The matrix bench measures the
model + filter together on real corpora; run the matrix engines with
`--strict-ner` (bench runner) or `GLINER_STRICT=1` (server) to measure the
filter's effect on live GLiNER output.

## Run

```
go run ./bench/probes/span_shape            # text report
go run ./bench/probes/span_shape -json       # machine-readable
go run ./bench/probes/span_shape -stoplist=acmeproduct,internalcodename
```

## Fixture

Real-PII surfaces (gold: redact) paired with structural FPs (gold: keep)
drawn from surfaces GLiNER is observed to mislabel. "before" = filter OFF;
"after" = `StrictSpanFilter`.
