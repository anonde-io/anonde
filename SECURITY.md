# Security Policy

anonde is a local-first PII anonymization toolkit. A vulnerability here
can expose the very data the tool exists to protect, so we take reports
seriously and ask you to disclose them responsibly.

## Supported versions

anonde is pre-`1.0`. Only the latest `0.x` minor release receives
security fixes; there are no long-term support branches yet.

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

When `1.0` ships, this table will be updated with a longer support window.

## Reporting a vulnerability

**Do not open a public issue, pull request, or discussion for a security
problem.** Use one of the private channels below:

1. **GitHub private vulnerability reporting** (preferred) — open the
   [Security tab](https://github.com/anonde-io/anonde/security/advisories/new)
   and submit a draft advisory. This keeps the report private and lets us
   collaborate on a fix in the same place.
2. **Email** — <security@anonde.io>. Encrypt with our PGP key if you
   have sensitive details; ask for the key in a first, content-free
   message if needed.

Please include, as far as you can:

- The affected version, commit, or image tag.
- The build variant (patterns-only or `-tags ner` NER).
- A minimal reproduction — input, request, and observed vs. expected
  behavior. Redact any real PII from the report itself.
- Impact: what an attacker gains (e.g. plaintext leak, vault bypass,
  unauthorized reveal).

## What to expect

| Stage                      | Target                       |
| -------------------------- | ---------------------------- |
| Acknowledgement of report  | within 3 business days       |
| Initial severity assessment| within 7 business days       |
| Fix or mitigation plan     | within 30 days for High/Critical |

We will keep you updated as the fix progresses, credit you in the
release notes and advisory unless you ask otherwise, and coordinate a
disclosure date with you. We follow a **90-day coordinated disclosure**
window: if a fix is not ready by then, we will discuss next steps with
you rather than disclose unilaterally.

## Scope

In scope — issues in this repository, including:

- Incomplete or bypassable anonymization that leaks PII downstream
  (note: missed spans are a *recall* concern tracked by the benchmark,
  not a security report, unless the miss is triggerable by crafted input).
- Unauthorized reveal / de-anonymization, or bypass of the
  `actor` + `purpose` gate on the reveal path.
- Token-vault disclosure, predictable tokens, or cross-tenant leakage.
- Injection, SSRF, or request smuggling through the HTTP API or the
  OpenAI-compatible proxy.
- Secrets (API keys, upstream credentials) exposed via logs, errors,
  or responses.
- Build- or image-supply-chain issues (e.g. the bundled
  `libonnxruntime.so` in the NER image).

Out of scope:

- Vulnerabilities in third-party dependencies — report those upstream;
  tell us if anonde's default configuration makes them exploitable.
- Findings that require a misconfigured deployment the docs warn against
  (e.g. exposing the server to the public internet without auth).
- Detection-recall gaps with no security trigger — open a normal issue
  or see the benchmark suite.

## Operator hardening notes

anonde is self-hosted; some of its security posture is the operator's
responsibility:

- The HTTP API has **no built-in authentication** in `0.1`. Run it
  inside your trust boundary, behind your own gateway or mTLS. Do not
  expose it directly to the public internet.
- The token vault is reversible by design. Treat the vault store
  (in-memory or bbolt file) as containing the original cleartext, and
  protect it accordingly.
- Encryption operators use keys you supply — manage and rotate them
  with your normal secrets process.

Thank you for helping keep anonde and its users safe.
