# Anonymization operators

Operators decide what to do with each detected span. Configure per-entity-type via `anonymizer.AnonymizerConfig`. Use `"*"` for a catch-all default.

```go
anon := anonde.DefaultAnonymizerEngine()
out, _ := anon.Anonymize(text, results, anonymizer.AnonymizerConfig{
    "*":             &operators.Replace{},
    "EMAIL_ADDRESS": &operators.Mask{MaskingChar: "*", CharsToMask: 4, FromEnd: true},
    "CREDIT_CARD":   &operators.Synthesize{Consistent: true},
})
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

## Synthesize — structurally-valid fake data

Replaces PII with realistic fakes that pass the same checksums as the original (Luhn for cards, MOD-97 for IBAN, valid SSN area codes, same IP class, etc.). The result looks real but contains no actual personal information — useful for staging environments, test fixtures, demo videos.

```go
&operators.Synthesize{}                                         // random per call
&operators.Synthesize{Consistent: true}                         // globally deterministic: same input → same fake
&operators.Synthesize{Consistent: true, DocumentScoped: true}   // per-document aliasing; call .Reset()
```

## Reversibility

Replace, Mask, and Hash are one-way. Encrypt is reversible with the key. Synthesize is reversible only when paired with the anonde server's vault — anonde stores the original cleartext keyed by the minted token (`<PERSON_T1_000001>` style), separate from the operator's output shape. See [QUICKSTART.md](QUICKSTART.md) for the round-trip flow.
