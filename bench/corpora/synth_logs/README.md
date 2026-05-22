# bench/corpora/synth_logs — synthetic enterprise log bench

Phase 3 of the multilingual bench expansion. A synthetic corpus of
enterprise log excerpts, slot-based and **gold by construction** — every
piece of PII has a known (start, end, type).

Four log types:

| Key | Log type | Characteristic |
|---|---|---|
| `auth` | Auth-service logs | login success/failure, password reset, token issue |
| `error` | Application error logs | stack traces with embedded user context |
| `access` | HTTP access logs | Apache/gateway-style request lines |
| `audit` | Compliance audit trails | actor/action/target records |

## Why synthetic and not real

Real production logs are the worst possible thing to ship in a public
repo: they are exactly where live credentials and customer PII leak.
Slot-based synthesis is the only safe way to get a gold-labelled log
corpus in-tree.

## Language classification — English

The log **scaffolding** (timestamps, levels, field names, paths, status
codes) is English. The **embedded PII** (person names, street
addresses) is sampled across the EN/DE/ES/FR/IT locales — realistic for
a global SaaS whose users are multilingual but whose logging stack is
not. There is no single dominant PII language, so the corpus is wired
into `bench/Makefile` as an **English** corpus (`EN_CORPORA`): English
is the scaffolding language and the honest default. A separate "mixed"
matrix partition would need new plumbing for no benchmarking gain.

## PII slots — scored

The generator emits the **canonical `label_map.yaml` gold types
directly** — no label-map gold mapping is needed for these:

| Slot concept | Gold type | anonde recognizer |
|---|---|---|
| Person name (multilingual) | `PERSON` | NER / pattern |
| Street address (multilingual) | `ADDRESS` | pattern / NER |
| Email | `EMAIL` | `EmailRecognizer` |
| Phone | `PHONE` | `PhoneRecognizer` |
| URL | `URL` | `URLRecognizer` |
| IPv4 / IPv6 address | `ID` | `IPAddressRecognizer` |
| MAC address | `ID` | `MACAddressRecognizer` |
| Login username | `ID` | (account identifier) |
| Customer / account ID | `ID` | (account identifier) |

IP and MAC fold to canonical `ID` via the existing `label_map.yaml`
`gold:` entries (`IP_ADDRESS: ID`, `MAC_ADDRESS: ID`).

## SECRET slots — NOT scored today (deliberate)

The corpus also embeds **secrets**: API keys (`sk_live_...`, 40-char hex
keys), JWTs, bearer / session tokens, and OAuth client secrets.

**anonde does not ship a secret recognizer yet.** If these spans were
scored, every one would count as a leak and depress the corpus's leak
rate for a capability anonde was never built to have — an unfair number.

So every secret span is gold-tagged with a distinct type **`SECRET`**,
and `label_map.yaml` maps:

```yaml
gold:
  SECRET: ~      # secret tokens — anonde ships no secret recognizer yet
```

`~` (null) drops those spans from scoring entirely — `compare.py`'s
`_normalize` returns `None` for them, and `_gold_spans` skips `None`.
The result: a **fair PII-only leak rate** today.

**The SECRET spans stay in the gold `corpus.jsonl`.** They are not
deleted — they are parked. When anonde grows a secret recognizer, a
future phase scores them by flipping that one `label_map.yaml` line
(e.g. `SECRET: ID`, or adding `SECRET` to the `canonical:` list). No
regeneration of the corpus is needed.

If you read this corpus's `REPORT.md` and wonder why secrets do not
appear: that is by design — check the `SECRET` line in `label_map.yaml`.

## Run

```bash
make -C bench/corpora/synth_logs all
open bench/corpora/synth_logs/REPORT.md
```

Default config: 30 docs per log type = 120 docs total, deterministic
(`SEED=20260512`). Override:

```bash
make -C bench/corpora/synth_logs all PER_LOGTYPE=100  # 400 docs
make -C bench/corpora/synth_logs all SEED=42
```

## What this bench proves (and doesn't)

✅ Proves: anonde catches log-embedded PII (emails, IPs, MACs,
   usernames, account IDs, person names, addresses, URLs, phones) in
   auth / error / access / audit log shapes.

✅ Proves (negatively): the SECRET spans are a standing record of what
   anonde does NOT yet catch — a built-in regression hook for a future
   secret recognizer.

❌ Does NOT prove: anonde catches secrets. It does not — that is the
   whole point of the `SECRET: ~` mapping.

❌ Does NOT prove: anonde handles real production-log noise (truncated
   lines, multi-line JSON payloads, binary garbage). The generator is
   template-bounded.
