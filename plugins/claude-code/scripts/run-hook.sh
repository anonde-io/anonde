#!/bin/sh
# UserPromptSubmit / PreToolUse hook entrypoint. Resolves anonde-hook and
# execs it, passing the event payload through on stdin unchanged. If the
# binary can't be resolved (and can't be fetched), exit 0 so the session is
# never blocked — fail open, matching anonde-hook's own default posture.
. "$(dirname "$0")/_resolve.sh"

bin=$(resolve_bin 2>/dev/null) || {
  if ensure_bin; then
    bin=$(resolve_bin 2>/dev/null)
  fi
}
[ -n "${bin:-}" ] || exit 0

exec "$bin"
