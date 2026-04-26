#!/usr/bin/env python3
"""
Presidio Python benchmark — mirrors the Go benchmark scenarios including big data.

Usage:
  python bench_python.py [--iterations N] [--big-iterations N] [--json]
"""

import argparse
import json
import statistics
import sys
import time
import threading

from presidio_analyzer import AnalyzerEngine
from presidio_anonymizer import AnonymizerEngine

# ---------------------------------------------------------------------------
# Corpus — identical to Go benchmark (pattern PII + NER entities)
# ---------------------------------------------------------------------------
CORPUS = [
    (
        "Hi, I'm Alice Johnson. My email is alice@example.com and I can be reached at +1-800-555-0199.\n"
        "My SSN is 523-45-6789. Credit card: 4111111111111111. Visit https://example.com for more info.\n"
        "Server IP: 192.168.1.100. Bitcoin wallet: 1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2.\n"
        "IBAN: GB29NWBK60161331926819. MAC: 00:1A:2B:3C:4D:5E. Born on 1990-03-15."
    ),
    (
        "Bob Smith, Senior Engineer at Microsoft, contacted support@company.org or called 020-7946-0958.\n"
        "The meeting was held in Seattle at the Amazon headquarters on March 15, 2024.\n"
        "Our server is at 10.0.0.1. Driver license: A1234567. Passport: A12345678.\n"
        "Ethereum wallet: 0xde0B295669a9FD93d5F28D9Ec85E40f4cb697BAe."
    ),
    (
        "Sarah Connor from Goldman Sachs in New York sent payment to IBAN DE89370400440532013000.\n"
        "Email her at sarah.connor@goldmansachs.com. Phone: (555) 867-5309.\n"
        "Date of service: January 15, 2024. SSN: 234-56-7890."
    ),
    "Dr. James Wilson at Johns Hopkins emailed james@jhu.edu. Server IP: 172.16.0.1.",
    (
        "No PII entities in this plain text document about software engineering and cloud architecture.\n"
        "The system processes millions of records per second and provides low latency responses."
    ),
]

SHORT_TEXT = "Contact me at user@example.com for details."
LONG_TEXT = CORPUS[0] * 100

# ---------------------------------------------------------------------------
# Realistic large-text generator (mirrors gentext_test.go)
# Produces both pattern PII and NER entities (PERSON, ORGANIZATION, LOCATION)
# ---------------------------------------------------------------------------
FIRST_NAMES = [
    "Alice", "Bob", "Carol", "David", "Emma", "Frank", "Grace", "Henry",
    "Isabel", "James", "Karen", "Liam", "Maria", "Noah", "Olivia", "Paul",
    "Quinn", "Rachel", "Samuel", "Tina", "Uma", "Victor", "Wendy", "Xavier",
]
LAST_NAMES = [
    "Johnson", "Smith", "Williams", "Brown", "Jones", "Garcia", "Miller",
    "Davis", "Wilson", "Moore", "Taylor", "Anderson", "Thomas", "Jackson",
    "White", "Harris", "Martin", "Thompson", "Lee", "Walker", "Hall", "Allen",
]
ORGS = [
    "Microsoft", "Google", "Amazon", "Apple", "Goldman Sachs", "JPMorgan",
    "IBM", "Oracle", "Salesforce", "Cisco", "Intel", "Nvidia", "Meta",
    "Netflix", "Adobe", "Spotify", "Uber", "Airbnb", "Stripe", "Twilio",
]
CITIES = [
    "Seattle", "New York", "San Francisco", "Chicago", "Boston", "Austin",
    "London", "Berlin", "Paris", "Tokyo", "Sydney", "Toronto", "Amsterdam",
    "Dublin", "Singapore", "Zurich", "Stockholm", "Copenhagen", "Oslo",
]
NER_TEMPLATES = [
    "{fn} {ln}, engineer at {org}, works remotely from {city}.",
    "{fn} {ln} joined {org} as a consultant based in {city}.",
    "The {org} office in {city} hired {fn} {ln} last quarter.",
    "{fn} {ln} from {org} presented at the {city} conference.",
    "According to {fn} {ln}, {org} expanded operations into {city}.",
]
PATTERN_FRAGMENTS = [
    ("email", lambda i: f"email {FIRST_NAMES[i%len(FIRST_NAMES)].lower()}{i%1000}@example.com"),
    ("phone", lambda i: f"phone +1-800-555-{i%10000:04d}"),
    ("ssn",   lambda i: f"SSN 5{i%100:02d}-4{i%10}-678{i%10}"),
    ("cc",    lambda i: f"credit card 4111111111111{i%1000:03d}"),
    ("ip",    lambda i: f"server 192.168.{i%256}.{(i+1)%256}"),
    ("url",   lambda i: f"visit https://service{i%1000}.example.com/account"),
    ("iban",  lambda i: f"IBAN GB{29+i%70:02d}NWBK6016133192{i%10000:04d}"),
    ("btc",   lambda i: f"wallet 1BvBMSEYstWetqTFn5Au4m4GFg7xJa{i%10000:04d}"),
    ("dob",   lambda i: f"born on 19{60+i%40:02d}-0{1+i%9}-1{i%9}"),
    ("mac",   lambda i: f"mac 00:1A:2B:3C:{i%256:02X}:{(i+7)%256:02X}"),
]
FILLER = [
    "The system processes requests asynchronously using an event-driven architecture.",
    "All financial transactions must be reviewed by the compliance department before approval.",
    "Our cloud infrastructure is deployed across multiple availability zones for redundancy.",
    "The support team processed over ten thousand tickets during the quarter with high satisfaction.",
    "Data retention policies require secure deletion of records older than seven years.",
    "The engineering team completed the migration to the new distributed storage system.",
    "Regulatory requirements mandate that all personal data be encrypted at rest and in transit.",
    "The quarterly audit revealed no significant deviations from the established security protocols.",
    "Incident response procedures were updated to reflect the latest threat intelligence findings.",
    "Customer feedback indicated a strong preference for self-service portal functionality.",
]


def _ner_sentence(i):
    fn = FIRST_NAMES[i % len(FIRST_NAMES)]
    ln = LAST_NAMES[(i + 3) % len(LAST_NAMES)]
    org = ORGS[i % len(ORGS)]
    city = CITIES[(i + 5) % len(CITIES)]
    return NER_TEMPLATES[i % len(NER_TEMPLATES)].format(fn=fn, ln=ln, org=org, city=city)


def _pattern_fragment(i):
    _, fn = PATTERN_FRAGMENTS[i % len(PATTERN_FRAGMENTS)]
    return fn(i)


def generate_text(target_bytes, ner_interval=200):
    pattern_interval = ner_interval + 150
    parts = []
    written = 0
    filler_idx = ner_idx = pattern_idx = 0
    while written < target_bytes:
        chunk = FILLER[filler_idx % len(FILLER)] + "\n"
        parts.append(chunk)
        written += len(chunk)
        filler_idx += 1
        if written % ner_interval < len(chunk):
            line = _ner_sentence(ner_idx) + "\n"
            parts.append(line)
            written += len(line)
            ner_idx += 1
        if written % pattern_interval < len(chunk):
            line = f"Record: {_pattern_fragment(pattern_idx)}.\n"
            parts.append(line)
            written += len(line)
            pattern_idx += 1
    return "".join(parts)


def generate_batch(n, doc_bytes):
    return [generate_text(doc_bytes, 200 + i % 300) for i in range(n)]


# ---------------------------------------------------------------------------
# Engines
# ---------------------------------------------------------------------------
print("Loading Presidio engines...", file=sys.stderr, flush=True)
t0 = time.perf_counter()
analyzer = AnalyzerEngine()
anonymizer_engine = AnonymizerEngine()
print(f"  ready in {(time.perf_counter()-t0)*1000:.0f}ms", file=sys.stderr, flush=True)


CHUNK_SIZE = 50_000  # spaCy default max_length guard


def analyze(text):
    return analyzer.analyze(text=text, language="en")


def analyze_chunked(text, chunk_size=CHUNK_SIZE):
    """Analyze large text by splitting into chunks at paragraph boundaries."""
    results = []
    offset = 0
    while offset < len(text):
        end = min(offset + chunk_size, len(text))
        # Break at a newline to avoid splitting mid-sentence.
        if end < len(text):
            nl = text.rfind("\n", offset, end)
            if nl > offset:
                end = nl + 1
        chunk = text[offset:end]
        for r in analyzer.analyze(text=chunk, language="en"):
            r.start += offset
            r.end += offset
            results.append(r)
        offset = end
    return results


def analyze_and_anonymize(text):
    results = analyzer.analyze(text=text, language="en")
    anonymizer_engine.anonymize(text=text, analyzer_results=results)


# ---------------------------------------------------------------------------
# Benchmark harness
# ---------------------------------------------------------------------------
def bench(name, fn, iterations, byte_size=None):
    for _ in range(min(3, iterations)):
        fn()

    times = []
    for _ in range(iterations):
        t0 = time.perf_counter()
        fn()
        times.append(time.perf_counter() - t0)

    mean_ns = statistics.mean(times) * 1e9
    result = {
        "name": name,
        "iterations": iterations,
        "mean_ns": int(mean_ns),
        "min_ns": int(min(times) * 1e9),
        "max_ns": int(max(times) * 1e9),
        "ops_per_sec": 1e9 / mean_ns,
    }
    if byte_size:
        result["mb_per_sec"] = (byte_size / (mean_ns / 1e9)) / (1024 * 1024)
        result["byte_size"] = byte_size
    return result


def bench_parallel(name, fn, iterations, n_threads=14, byte_size=None):
    for _ in range(min(3, iterations)):
        fn()

    times = []
    for _ in range(iterations):
        barrier = threading.Barrier(n_threads)

        def worker():
            barrier.wait()
            fn()

        threads = [threading.Thread(target=worker) for _ in range(n_threads)]
        t_start = time.perf_counter()
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        times.append(time.perf_counter() - t_start)

    mean_ns = statistics.mean(times) * 1e9
    result = {
        "name": name,
        "iterations": iterations,
        "mean_ns": int(mean_ns),
        "min_ns": int(min(times) * 1e9),
        "max_ns": int(max(times) * 1e9),
        "ops_per_sec": 1e9 / mean_ns,
        "threads": n_threads,
    }
    if byte_size:
        result["mb_per_sec"] = (byte_size / (mean_ns / 1e9)) / (1024 * 1024)
        result["byte_size"] = byte_size
    return result


def fmt_ns(ns):
    if ns >= 1_000_000_000:
        return f"{ns/1_000_000_000:.3f}s"
    if ns >= 1_000_000:
        return f"{ns/1_000_000:.2f}ms"
    if ns >= 1_000:
        return f"{ns/1_000:.2f}µs"
    return f"{ns}ns"


def run(iterations, big_iterations):
    print("  [1/11] corpus analyze...", file=sys.stderr, flush=True)
    r1 = bench("BenchmarkAnalyzeOnly", lambda: [analyze(t) for t in CORPUS], iterations)
    print("  [2/11] corpus analyze+anonymize...", file=sys.stderr, flush=True)
    r2 = bench("BenchmarkAnalyzeAndAnonymize", lambda: [analyze_and_anonymize(t) for t in CORPUS], iterations)
    print("  [3/11] short text...", file=sys.stderr, flush=True)
    r3 = bench("BenchmarkAnalyzeShort", lambda: analyze(SHORT_TEXT), iterations)
    print("  [4/11] long text (100x)...", file=sys.stderr, flush=True)
    r4 = bench("BenchmarkAnalyzeLong", lambda: analyze(LONG_TEXT), iterations)
    print("  [5/11] no-PII...", file=sys.stderr, flush=True)
    r5 = bench("BenchmarkAnalyzeNoPII", lambda: analyze(CORPUS[4]), iterations)
    print("  [6/11] parallel corpus...", file=sys.stderr, flush=True)
    r6 = bench_parallel("BenchmarkAnalyzeParallel", lambda: [analyze(t) for t in CORPUS], iterations)

    # Large data
    print("  Generating large texts...", file=sys.stderr, flush=True)
    text_100kb = generate_text(100 * 1024)
    text_1mb = generate_text(1024 * 1024)
    batch_100 = generate_batch(100, 1024)
    batch_1000 = generate_batch(1000, 1024)
    batch_total = sum(len(d) for d in batch_1000)

    print("  [7/11] 100 KB document...", file=sys.stderr, flush=True)
    r7 = bench("BenchmarkBulk_100KB", lambda: analyze_chunked(text_100kb), big_iterations,
               byte_size=len(text_100kb))
    print("  [8/11] 1 MB document...", file=sys.stderr, flush=True)
    r8 = bench("BenchmarkBulk_1MB", lambda: analyze_chunked(text_1mb), big_iterations,
               byte_size=len(text_1mb))
    print("  [9/11] batch 100 docs...", file=sys.stderr, flush=True)
    r9 = bench("BenchmarkBulk_Batch100", lambda: [analyze(d) for d in batch_100],
               big_iterations, byte_size=sum(len(d) for d in batch_100))
    print("  [10/11] batch 1000 docs...", file=sys.stderr, flush=True)
    r10 = bench("BenchmarkBulk_Batch1000", lambda: [analyze(d) for d in batch_1000],
                big_iterations, byte_size=batch_total)
    print("  [11/11] batch 1000 parallel...", file=sys.stderr, flush=True)
    r11 = bench_parallel("BenchmarkBulk_Batch1000Parallel",
                         lambda: [analyze(d) for d in batch_1000],
                         big_iterations, byte_size=batch_total)

    return [r1, r2, r3, r4, r5, r6, r7, r8, r9, r10, r11]


def print_table(results):
    print(f"\n{'Benchmark':<40} {'mean':>12} {'MB/s':>8} {'ops/s':>10}")
    print("-" * 74)
    for r in results:
        mb = f"{r['mb_per_sec']:.2f}" if "mb_per_sec" in r else "-"
        print(f"{r['name']:<40} {fmt_ns(r['mean_ns']):>12} {mb:>8} {r['ops_per_sec']:>10.2f}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--iterations", type=int, default=20)
    parser.add_argument("--big-iterations", type=int, default=5)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    results = run(args.iterations, args.big_iterations)

    if args.json:
        print(json.dumps(results, indent=2))
    else:
        print_table(results)
