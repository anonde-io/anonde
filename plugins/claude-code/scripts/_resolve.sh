#!/bin/sh
# Shared resolver for the anonde Claude Code plugin. Locates the
# `anonde-hook` binary, fetching it on demand via the repo installer.
#
# Resolution order:
#   1. anonde-hook already on PATH (user ran the curl installer or `go install`)
#   2. the plugin-managed cache copy
#   3. download into the cache via install.sh
#
# Everything is best-effort: callers fail open (exit 0 / no PII scan) if the
# binary can't be resolved, so a plugin/network problem never bricks a session.

ANONDE_CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/anonde"
ANONDE_INSTALL_URL="https://raw.githubusercontent.com/anonde-io/anonde/main/install.sh"

# resolve_bin prints the path to a usable anonde-hook, or returns 1.
resolve_bin() {
  if command -v anonde-hook >/dev/null 2>&1; then
    command -v anonde-hook
    return 0
  fi
  if [ -x "${ANONDE_CACHE}/anonde-hook" ]; then
    printf '%s\n' "${ANONDE_CACHE}/anonde-hook"
    return 0
  fi
  return 1
}

# ensure_bin makes anonde-hook available (downloading if needed). Returns 1 if
# it still can't be resolved afterwards (e.g. offline).
ensure_bin() {
  if resolve_bin >/dev/null 2>&1; then
    return 0
  fi
  command -v curl >/dev/null 2>&1 || return 1
  curl -fsSL "$ANONDE_INSTALL_URL" 2>/dev/null |
    ANONDE_INSTALL_DIR="$ANONDE_CACHE" ANONDE_QUIET=1 sh >/dev/null 2>&1 || true
  resolve_bin >/dev/null 2>&1
}
