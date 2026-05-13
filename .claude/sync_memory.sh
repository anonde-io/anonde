#!/usr/bin/env bash
# sync_memory.sh — pull Claude's global per-project memory into the repo.
#
# Claude Code writes session memory to a global path under
# ~/.claude/projects/<encoded-cwd>/memory/. CLAUDE.md at the repo root
# tells Claude to *read* memory from .claude/memory/ in the repo, so
# future sessions on this machine pick up cross-session findings even
# when Claude's global cache is wiped (e.g. fresh install, different
# user).
#
# This is a one-way pull: global → repo. We never push back the other
# way, so anything Claude wrote to the global cache lands in the repo
# dir as a side effect of running this. Run it whenever you've had a
# substantive Claude session and want its memory entries to survive a
# machine reset.
#
# .claude/memory/ is gitignored, so the synced files stay local. If you
# want to share memory with a teammate, hand them this script + tell
# them their global cache will populate on first session.
#
# Usage:
#   .claude/sync_memory.sh           # default: pull all .md files
#   .claude/sync_memory.sh --dry-run # show what would be copied

set -euo pipefail

# Resolve repo root regardless of where the script was invoked from.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Claude encodes the project cwd as the absolute path with every "/" and
# "." replaced by "-". The leading "/" becomes a leading "-", which is
# why the encoded form starts with one dash. Mirror that here so we
# don't have to hardcode the path (works on any clone location).
encoded_cwd="$(printf '%s' "$REPO_ROOT" | sed -e 's|/|-|g' -e 's|\.|-|g')"
GLOBAL_MEMORY="$HOME/.claude/projects/${encoded_cwd}/memory"

# Fallback: if the encoding above mismatches Claude's actual path on
# this machine (encoding behaviour has changed historically), let the
# user override via env.
GLOBAL_MEMORY="${CLAUDE_PROJECT_MEMORY:-$GLOBAL_MEMORY}"

LOCAL_MEMORY="$REPO_ROOT/.claude/memory"

dry_run=0
if [[ "${1:-}" == "--dry-run" ]]; then
    dry_run=1
fi

if [[ ! -d "$GLOBAL_MEMORY" ]]; then
    echo "no global memory at $GLOBAL_MEMORY" >&2
    echo "  set CLAUDE_PROJECT_MEMORY=<path> if Claude wrote it elsewhere" >&2
    exit 1
fi

mkdir -p "$LOCAL_MEMORY"

count=0
for src in "$GLOBAL_MEMORY"/*.md; do
    [[ -f "$src" ]] || continue
    name="$(basename "$src")"
    dst="$LOCAL_MEMORY/$name"
    if [[ $dry_run -eq 1 ]]; then
        echo "would copy: $src -> $dst"
    else
        cp -v "$src" "$dst"
    fi
    count=$((count + 1))
done

if [[ $dry_run -eq 0 ]]; then
    echo "synced $count file(s) from $GLOBAL_MEMORY"
fi
