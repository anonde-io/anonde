#!/bin/sh
# SessionStart hook: make sure anonde-hook is present so the per-prompt /
# per-tool hooks have zero download latency. Best-effort and silent — any
# failure (offline, no curl) is swallowed so it never blocks a session.
. "$(dirname "$0")/_resolve.sh"
ensure_bin || true
exit 0
