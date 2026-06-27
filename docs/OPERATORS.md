# Anonymization operators

Operators decide what to do with each detected span. `anonymizer.AnonymizerConfig`
is a struct; its `Operators` map keys on entity type (use `"*"` for a catch-all
default). The `anonymizer.Config(...)` helper builds a config from just an
operator map.

```go
anon := anonde.DefaultAnonymizerEngine()
out, _ := anon.Anonymize(text, results, anonymizer.Config(anonymizer.OperatorMap{
    "*":             &operators.Replace{},
    "EMAIL_ADDRESS": &operators.Mask{MaskingChar: "*", CharsToMask: 4, FromEnd: true},
    "CREDIT_CARD":   &operators.Synthesize{Consistent: true},
}))
```

## Replace

```go
&operators.Replace{NewValue: "<EMAIL>"}   // → <EMAIL>
&operators.Replace{}                       // → <EMAIL_ADDRESS> (entity type as tag)
```

## Redact / Mask / Hash / Encrypt

```go
&operators.Redact{}
&operators.Mask{MaskingChar: "*", CharsToMask: 4, FromEnd: true}   // +1-800-555-****
&operators.Hash{HashType: operators.HashSHA256}
&operators.Encrypt{Key: "32-byte-aes-key-……………………………"}            // AES-GCM, base64 nonce+ct
operators.Decrypt(value, key)                                       // reversible
```

## Synthesize: structurally-valid fake data

Replaces PII with realistic fakes that pass the same checksums as the original (Luhn for cards, MOD-97 for IBAN, valid SSN area codes, same IP class, etc.). The result looks real but contains no actual personal information. Useful for staging environments, test fixtures, demo videos.

```go
&operators.Synthesize{}                                         // random per call
&operators.Synthesize{Consistent: true}                         // globally deterministic: same input → same fake
&operators.Synthesize{Consistent: true, DocumentScoped: true}   // per-document aliasing; call .Reset()
```

## Keep: detect but don't anonymize

`Keep` records the span — it stays in the detection results and on any leak list —
but leaves its text **verbatim**: no replacement, no reverse-map/vault entry. Use
it for types that are worth surfacing yet harmful to rewrite: URLs and timestamps
corrupt downstream LLM prompts when tokenized, and generic IDs flood a reversible
vault with low-value entries.

Assign it per type via the operator map, or — equivalently and more concisely —
list the types in `DetectOnlyTypes`:

```go
anon.Anonymize(text, results, anonymizer.AnonymizerConfig{
    Operators:       anonymizer.OperatorMap{"*": &operators.Replace{}, "URL": &operators.Keep{}},
    DetectOnlyTypes: map[string]bool{"DATE_TIME": true, "ID": true},
})
```

## AllowList: never rewrite known-safe surfaces

`AllowList` is keyed on the span's trimmed, lower-cased **surface** (not type): any
detected span whose text matches is left verbatim — like `Keep`, but selected by
value. Use it for known-safe terms (your own company / product names) that
recognizers keep flagging.

```go
anon.Anonymize(text, results, anonymizer.AnonymizerConfig{
    Operators: anonymizer.OperatorMap{"*": &operators.Replace{}},
    AllowList: map[string]bool{"acme corp": true}, // keys must be pre-lowercased + trimmed
})
```

`DetectOnlyTypes` and `AllowList` only suppress the **rewrite** — detection still
happens, so the span stays visible to the caller and on any leak dashboard. To go
the other way and force-flag arbitrary terms (even ones the recognizers miss), add
a deny-list recognizer to the analyzer: `recognizers.NewDenyListRecognizer(terms,
"CUSTOM")` emits a finding (score 1.0) for every occurrence of each term.

## Reversibility

Replace, Mask, and Hash are one-way. Encrypt is reversible with the key. Synthesize is reversible only when paired with the anonde server's vault; anonde stores the original cleartext keyed by the minted token (`<PERSON_T1_000001>` style), separate from the operator's output shape. See [QUICKSTART.md](QUICKSTART.md) for the round-trip flow.
