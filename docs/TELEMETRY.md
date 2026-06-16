# Telemetry

anonde sends an anonymous heartbeat once every 24 hours so we can see
which deployment shapes (patterns vs NER, OS / arch, backend mix) and
which entity types are in active use, and prioritise the roadmap
against real signal rather than guesswork.

Disable with `ANONDE_TELEMETRY=off` or `ANONDE_OFFLINE=1`.

A random install ID is persisted at `$XDG_DATA_HOME/anonde/install_id`
(fallback `~/.anonde/install_id`) and reused across restarts.

Fields sent:

| Field | Example |
|---|---|
| `install_id` | `8a17c…b3` |
| `version` | `f684298` |
| `build_tag` | `default` or `ner` |
| `os` / `arch` | `linux` / `amd64` |
| `backend` | `patterns`, `gliner`, … |
| `uptime_seconds` | `86400` |
| `request_count` | `1245` |
| `error_count` | `3` |
| `entity_counts` | `{"PERSON": 412, "EMAIL_ADDRESS": 89}` |
| `p95_latency_ms` | `42.1` |

No input text, output text, token values, vault contents, IP
addresses, hostnames, tenant IDs, document IDs, actors, or purposes
are sent. The wire payload is defined in
[`internal/telemetry/payload.go`](../internal/telemetry/payload.go) and
enforced by a unit test.

Override the endpoint with `ANONDE_TELEMETRY_URL`.
