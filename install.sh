#!/bin/sh
# anonde-hook installer.
#
#   curl -fsSL https://raw.githubusercontent.com/anonde-io/anonde/main/install.sh | sh
#
# Downloads the prebuilt `anonde-hook` binary for your OS/arch from the
# GitHub Releases of anonde-io/anonde and installs it. The hook is a Claude
# Code PII guard — see examples/claude-code-hook/README.md.
#
# Environment overrides:
#   ANONDE_INSTALL_DIR  where to install   (default: $HOME/.local/bin)
#   ANONDE_VERSION      release tag        (default: latest, e.g. v0.1.0)
#   ANONDE_QUIET        suppress next-step output when set to 1
set -eu

REPO="anonde-io/anonde"
INSTALL_DIR="${ANONDE_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${ANONDE_VERSION:-latest}"
QUIET="${ANONDE_QUIET:-0}"

say() { [ "$QUIET" = "1" ] || printf '%s\n' "$*"; }
err() { printf 'anonde-hook installer: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || err "curl is required"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) err "unsupported architecture: $arch" ;;
esac
case "$os" in
  linux | darwin) ;;
  *) err "unsupported OS: $os (use 'go install github.com/anonde-io/anonde/cmd/anonde-hook@latest')" ;;
esac

asset="anonde-hook_${os}_${arch}"
if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

say "Downloading ${asset} (${VERSION})..."
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
curl -fSL --proto '=https' --tlsv1.2 "$url" -o "$tmp" ||
  err "download failed: $url"

mkdir -p "$INSTALL_DIR"
chmod +x "$tmp"
mv "$tmp" "${INSTALL_DIR}/anonde-hook"
trap - EXIT

say "Installed anonde-hook -> ${INSTALL_DIR}/anonde-hook"

# PATH hint + next steps.
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) say ""
     say "NOTE: ${INSTALL_DIR} is not on your PATH. Add it, e.g.:"
     say "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.profile && . ~/.profile" ;;
esac

if [ "$QUIET" != "1" ]; then
  say ""
  say "Next: register the hook in Claude Code. Either install the plugin"
  say "  /plugin marketplace add ${REPO}"
  say "  /plugin install anonde@anonde"
  say "or add the snippet from examples/claude-code-hook/settings.json to your"
  say "~/.claude/settings.json. Verify with: anonde-hook --version"
fi
