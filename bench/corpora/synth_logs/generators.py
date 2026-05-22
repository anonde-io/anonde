#!/usr/bin/env python3
"""PII + secret slot generators for synthetic enterprise log text.

Scaffolding (timestamps, log levels, field names, paths, status codes)
is generated in English by templates.py — that is realistic; a global
SaaS writes its logs in English. The PII *embedded* in those logs
(person names, addresses) is sampled across the EN/DE/ES/FR/IT locales,
because a global SaaS has multilingual users.

Two kinds of slot:

  * PII slots — scored by default. Canonical label_map gold types:
    EMAIL, PERSON, URL, PHONE, and ID (which absorbs IP addresses, MAC
    addresses, usernames, and customer/account IDs — every one maps to
    canonical ID in label_map.yaml).
  * SECRET slots — API keys, JWTs, bearer/session tokens, OAuth client
    secrets. anonde ships NO secret recognizer yet, so these are tagged
    with the gold type `SECRET`, and `SECRET` maps to `~` (dropped from
    scoring) in label_map.yaml. The spans stay in the gold JSONL so a
    future phase can score them by flipping one label_map line; today
    they give a fair PII-only leak number. See the corpus README.

Each generator returns Slot(text, type).
"""

from __future__ import annotations

import random
import string
from dataclasses import dataclass


# ===========================================================================
# Multilingual name / city / street vocab (EN/DE/ES/FR/IT users).
# ===========================================================================

FIRST_NAMES = [
    # en
    "James", "Mary", "Robert", "Jennifer", "Michael", "Linda", "David",
    # de
    "Hans", "Petra", "Wolfgang", "Sabine", "Andreas", "Karin", "Stefan",
    # es
    "Antonio", "María", "Manuel", "Carmen", "Javier", "Ana", "Carlos",
    # fr
    "Jean", "Marie", "Pierre", "Nathalie", "Nicolas", "Isabelle", "Laurent",
    # it
    "Giuseppe", "Anna", "Giovanni", "Giulia", "Francesco", "Lucia", "Marco",
]

LAST_NAMES = [
    # en
    "Smith", "Johnson", "Williams", "Brown", "Jones", "Miller", "Davis",
    # de
    "Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Wagner", "Becker",
    # es
    "García", "Rodríguez", "González", "Fernández", "López", "Martínez",
    # fr
    "Martin", "Bernard", "Dubois", "Durand", "Moreau", "Lefebvre", "Roux",
    # it
    "Rossi", "Russo", "Ferrari", "Esposito", "Bianchi", "Romano", "Greco",
]

# Full street-address strings sampled across locales (the address values
# are localised; the surrounding log field name is English).
ADDRESSES = [
    "42 High Street, London SW1 2AB",
    "118 Park Avenue, Boston M4 7QR",
    "Hauptstraße 17, 10115 Berlin",
    "Bahnhofstraße 88, 80331 München",
    "Calle Mayor 23, 28013 Madrid",
    "Gran Vía 105, 08001 Barcelona",
    "12 rue de la République, 75001 Paris",
    "47 avenue Victor Hugo, 69002 Lyon",
    "Via Roma 31, 00184 Roma",
    "Corso Italia 9, 20122 Milano",
]

# Realistic-looking SaaS / API hostnames for URL slots.
URL_HOSTS = [
    "app.example.com", "api.example.com", "dashboard.acme-corp.io",
    "portal.globex.net", "console.initech.cloud", "admin.umbrella.dev",
    "auth.example.com", "files.acme-corp.io",
]

URL_PATHS = [
    "/v2/users/profile", "/account/settings", "/api/orders/4471",
    "/admin/audit", "/auth/callback", "/billing/invoices",
    "/reports/export", "/v1/sessions",
]

# OAuth scopes / vendor names for secret-bearing log lines.
OAUTH_VENDORS = ["stripe", "github", "okta", "auth0", "twilio", "sendgrid"]


# ===========================================================================
# Slot dataclass + helpers
# ===========================================================================


@dataclass
class Slot:
    """One slot fill: surface string + gold type (canonical, or SECRET)."""

    text: str
    type: str


def _pick(rng: random.Random, xs: list):
    return xs[rng.randrange(len(xs))]


def _b64ish(rng: random.Random, n: int) -> str:
    """Random URL-safe-base64-ish token body of length n."""
    alpha = string.ascii_letters + string.digits + "-_"
    return "".join(alpha[rng.randrange(len(alpha))] for _ in range(n))


def _hexish(rng: random.Random, n: int) -> str:
    return "".join("0123456789abcdef"[rng.randrange(16)] for _ in range(n))


# ===========================================================================
# PII generators — canonical gold types
# ===========================================================================


def gen_person(rng: random.Random) -> Slot:
    return Slot(f"{_pick(rng, FIRST_NAMES)} {_pick(rng, LAST_NAMES)}", "PERSON")


def gen_address(rng: random.Random) -> Slot:
    return Slot(_pick(rng, ADDRESSES), "ADDRESS")


def gen_email(rng: random.Random) -> Slot:
    """Corporate-style email — first.last@company.tld."""
    def fold(s: str) -> str:
        table = {"ü": "ue", "ö": "oe", "ä": "ae", "ß": "ss", "á": "a",
                 "é": "e", "í": "i", "ó": "o", "ú": "u", "ñ": "n", "ç": "c",
                 "à": "a", "è": "e", "ì": "i", "ò": "o", "ù": "u", "â": "a",
                 "ê": "e", "î": "i", "ô": "o", "û": "u"}
        return "".join(table.get(c, c) for c in s)
    first = fold(_pick(rng, FIRST_NAMES)).lower()
    last = fold(_pick(rng, LAST_NAMES)).lower().replace(" ", "")
    domain = _pick(rng, ["example.com", "acme-corp.io", "globex.net",
                         "initech.cloud", "umbrella.dev"])
    return Slot(f"{first}.{last}@{domain}", "EMAIL")


def gen_phone(rng: random.Random) -> Slot:
    """International phone number — appears in account-context log lines."""
    cc = _pick(rng, ["+1", "+44", "+49", "+34", "+33", "+39"])
    return Slot(f"{cc} {rng.randint(100, 999)} {rng.randint(100000, 9999999)}",
                "PHONE")


def gen_ip(rng: random.Random) -> Slot:
    """IPv4 (75%) or IPv6 (25%) address. Canonical type ID (label_map
    folds IP_ADDRESS -> ID)."""
    if rng.random() < 0.75:
        return Slot(
            ".".join(str(rng.randint(1, 254)) for _ in range(4)), "ID")
    groups = [f"{rng.randrange(0x10000):x}" for _ in range(8)]
    return Slot(":".join(groups), "ID")


def gen_mac(rng: random.Random) -> Slot:
    """MAC address, colon-separated. Canonical type ID."""
    return Slot(
        ":".join(f"{rng.randrange(256):02x}" for _ in range(6)), "ID")


def gen_username(rng: random.Random) -> Slot:
    """Login username — letters + digits, sometimes a dotted handle.
    Canonical type ID (a username is an account identifier)."""
    first = _pick(rng, FIRST_NAMES).lower()
    last = _pick(rng, LAST_NAMES).lower().replace(" ", "")
    # Strip non-ascii so the handle is a realistic login string.
    first = "".join(c for c in first if c.isascii() and c.isalpha())
    last = "".join(c for c in last if c.isascii() and c.isalpha())
    if not first:
        first = "user"
    if not last:
        last = "acct"
    style = rng.random()
    if style < 0.4:
        return Slot(f"{first}.{last}", "ID")
    if style < 0.7:
        return Slot(f"{first[0]}{last}{rng.randint(1, 99)}", "ID")
    return Slot(f"{first}_{last}", "ID")


def gen_account_id(rng: random.Random) -> Slot:
    """Customer / account identifier. Canonical type ID."""
    style = rng.random()
    if style < 0.35:
        return Slot(f"usr_{_hexish(rng, 16)}", "ID")
    if style < 0.7:
        return Slot(f"cust-{rng.randint(100000, 9999999)}", "ID")
    return Slot(f"ACC{rng.randint(10000000, 999999999)}", "ID")


def gen_url(rng: random.Random) -> Slot:
    """Full URL referenced in a log line. Canonical type URL."""
    scheme = "https"
    host = _pick(rng, URL_HOSTS)
    path = _pick(rng, URL_PATHS)
    if rng.random() < 0.4:
        path = f"{path}?id={rng.randint(1000, 999999)}"
    return Slot(f"{scheme}://{host}{path}", "URL")


# ===========================================================================
# SECRET generators — gold type SECRET (dropped from scoring via label_map)
# ===========================================================================


def gen_api_key(rng: random.Random) -> Slot:
    """Vendor-style API key, e.g. sk_live_... or a 40-char hex key."""
    style = rng.random()
    if style < 0.5:
        env = _pick(rng, ["live", "test"])
        return Slot(f"sk_{env}_{_b64ish(rng, 24)}", "SECRET")
    return Slot(_hexish(rng, 40), "SECRET")


def gen_jwt(rng: random.Random) -> Slot:
    """JSON Web Token — three dot-separated base64url segments."""
    return Slot(
        f"{_b64ish(rng, 36)}.{_b64ish(rng, 80)}.{_b64ish(rng, 43)}",
        "SECRET")


def gen_bearer_token(rng: random.Random) -> Slot:
    """Opaque bearer / session token."""
    style = rng.random()
    if style < 0.5:
        return Slot(_b64ish(rng, 32), "SECRET")
    return Slot(f"sess_{_hexish(rng, 32)}", "SECRET")


def gen_oauth_secret(rng: random.Random) -> Slot:
    """OAuth client secret — vendor-prefixed opaque string."""
    vendor = _pick(rng, OAUTH_VENDORS)
    return Slot(f"{vendor}_cs_{_b64ish(rng, 28)}", "SECRET")


# ===========================================================================
# Dispatch tables
# ===========================================================================

# PII slots — scored by default.
PII_GENERATORS = {
    "PERSON": gen_person,
    "ADDRESS": gen_address,
    "EMAIL": gen_email,
    "PHONE": gen_phone,
    "IP": gen_ip,
    "MAC": gen_mac,
    "USERNAME": gen_username,
    "ACCOUNT_ID": gen_account_id,
    "URL": gen_url,
}

# SECRET slots — gold type SECRET, dropped from scoring (label_map: SECRET ~).
SECRET_GENERATORS = {
    "API_KEY": gen_api_key,
    "JWT": gen_jwt,
    "BEARER": gen_bearer_token,
    "OAUTH_SECRET": gen_oauth_secret,
}

# Combined view used by generate.py.
GENERATORS = {**PII_GENERATORS, **SECRET_GENERATORS}
