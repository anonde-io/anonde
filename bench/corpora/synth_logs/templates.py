#!/usr/bin/env python3
"""Templates for synthetic enterprise log documents.

Four log types: auth logs, application error logs, HTTP access logs,
audit trails. Scaffolding (timestamps, levels, field names, paths,
status codes) is English — realistic for a global SaaS. Embedded PII
slots resolve via generators.py and are sampled across EN/DE/ES/FR/IT.

A "document" is a multi-line log excerpt: a small header line plus a
body of N log lines sampled (with replacement — real logs repeat line
shapes) from the type's line pool.

Slots:
  * `{SLOT}`        — a PII or SECRET slot filled by generators.py.
  * `{TS}`          — ISO-ish timestamp, non-PII scaffolding.
  * `{LEVEL}`       — log level, non-PII.
  * `{STATUS}`      — HTTP status code, non-PII.
  * `{LATENCY}`     — request latency ms, non-PII.
  * `{METHOD}`      — HTTP method, non-PII.
  * `{ERRCODE}`     — internal error code, non-PII.
"""

from __future__ import annotations


# -- Auth logs -----------------------------------------------------------

AUTH_HEADER = "=== auth-service log excerpt — node auth-{ERRCODE} ===\n"

AUTH_LINES = [
    "{TS} {LEVEL} login.success user={USERNAME} email={EMAIL} ip={IP} session={BEARER}",
    "{TS} {LEVEL} login.failure user={USERNAME} ip={IP} reason=bad_password attempts=3",
    "{TS} {LEVEL} login.success user={USERNAME} ip={IP} mfa=passed device_mac={MAC}",
    "{TS} {LEVEL} password.reset.requested email={EMAIL} ip={IP} token={BEARER}",
    "{TS} {LEVEL} password.reset.completed user={USERNAME} email={EMAIL} ip={IP}",
    "{TS} {LEVEL} token.issued account={ACCOUNT_ID} scope=read_write jwt={JWT}",
    "{TS} {LEVEL} token.refresh account={ACCOUNT_ID} ip={IP} jwt={JWT}",
    "{TS} {LEVEL} api.key.used account={ACCOUNT_ID} key={API_KEY} endpoint={URL}",
    "{TS} {LEVEL} login.locked user={USERNAME} email={EMAIL} ip={IP} after=5 failures",
    "{TS} {LEVEL} oauth.exchange vendor=stripe account={ACCOUNT_ID} client_secret={OAUTH_SECRET}",
    "{TS} {LEVEL} session.created user={USERNAME} ip={IP} ua=Mozilla/5.0 session={BEARER}",
    "{TS} {LEVEL} mfa.enrolled user={USERNAME} phone={PHONE} ip={IP}",
]

# -- Application error logs (stack traces with user context) -------------

ERROR_HEADER = "=== app-service error log — build {ERRCODE} ===\n"

ERROR_LINES = [
    "{TS} {LEVEL} unhandled exception in OrderController for user={USERNAME} account={ACCOUNT_ID}",
    "  at com.acme.billing.charge(Billing.java:204) account={ACCOUNT_ID}",
    "  request from ip={IP} email={EMAIL} failed with {ERRCODE}",
    "{TS} {LEVEL} NullPointerException processing profile for {PERSON} email={EMAIL}",
    "  context: user={USERNAME} ip={IP} api_key={API_KEY}",
    "{TS} {LEVEL} payment gateway timeout account={ACCOUNT_ID} ip={IP} latency={LATENCY}ms",
    "{TS} {LEVEL} failed to render invoice for {PERSON} at address {ADDRESS}",
    "{TS} {LEVEL} downstream call to {URL} returned {STATUS} for account={ACCOUNT_ID}",
    "  retry with bearer={BEARER} jwt={JWT}",
    "{TS} {LEVEL} email delivery bounced recipient={EMAIL} user={USERNAME}",
    "{TS} {LEVEL} db connection lost host={IP} during request for user={USERNAME}",
    "  affected customer {PERSON} phone={PHONE}",
]

# -- HTTP access logs ----------------------------------------------------

ACCESS_HEADER = "=== gateway access log — pop {ERRCODE} ===\n"

ACCESS_LINES = [
    '{IP} - {USERNAME} [{TS}] "{METHOD} {URL}" {STATUS} {LATENCY}',
    '{IP} - {USERNAME} [{TS}] "{METHOD} {URL}" {STATUS} {LATENCY} ref={URL}',
    '{IP} - - [{TS}] "{METHOD} {URL}" {STATUS} {LATENCY} auth=Bearer:{BEARER}',
    '{IP} - {USERNAME} [{TS}] "{METHOD} {URL}" {STATUS} {LATENCY} apikey={API_KEY}',
    '{IP} - {USERNAME} [{TS}] "GET {URL}" {STATUS} {LATENCY} account={ACCOUNT_ID}',
    '{IP} - - [{TS}] "POST {URL}" {STATUS} {LATENCY} jwt={JWT}',
    '{IP} - {USERNAME} [{TS}] "{METHOD} {URL}" {STATUS} {LATENCY} email={EMAIL}',
    '{IP} - {USERNAME} [{TS}] "DELETE {URL}" {STATUS} {LATENCY} mac={MAC}',
]

# -- Audit trails --------------------------------------------------------

AUDIT_HEADER = "=== compliance audit trail — tenant {ACCOUNT_ID} ===\n"

AUDIT_LINES = [
    "{TS} {LEVEL} audit actor={PERSON} ({EMAIL}) action=user.export target={USERNAME}",
    "{TS} {LEVEL} audit actor={USERNAME} action=role.grant target_account={ACCOUNT_ID} ip={IP}",
    "{TS} {LEVEL} audit actor={PERSON} action=settings.update from_ip={IP}",
    "{TS} {LEVEL} audit data.access actor={USERNAME} record={PERSON} address={ADDRESS}",
    "{TS} {LEVEL} audit api.key.rotated actor={EMAIL} old_key={API_KEY} new_key={API_KEY}",
    "{TS} {LEVEL} audit actor={PERSON} action=invoice.view target_account={ACCOUNT_ID}",
    "{TS} {LEVEL} audit actor={USERNAME} action=token.revoke jwt={JWT} ip={IP}",
    "{TS} {LEVEL} audit consent.update subject={PERSON} email={EMAIL} phone={PHONE}",
    "{TS} {LEVEL} audit actor={EMAIL} action=oauth.app.create client_secret={OAUTH_SECRET}",
    "{TS} {LEVEL} audit login.as actor={PERSON} impersonated={USERNAME} from_ip={IP}",
]


# logtype -> (header_template, line_pool, (lines_lo, lines_hi))
LOGTYPES = {
    "auth":   (AUTH_HEADER,   AUTH_LINES,   (8, 16)),
    "error":  (ERROR_HEADER,  ERROR_LINES,  (6, 12)),
    "access": (ACCESS_HEADER, ACCESS_LINES, (10, 20)),
    "audit":  (AUDIT_HEADER,  AUDIT_LINES,  (6, 14)),
}

# Fixed order so doc ids are deterministic.
LOGTYPE_ORDER = ["auth", "error", "access", "audit"]
