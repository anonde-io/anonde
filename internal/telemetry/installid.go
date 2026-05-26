package telemetry

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallIDFile is the basename of the persisted install_id file.
// Kept exported so tests can clean up.
const InstallIDFile = "install_id"

// LastHeartbeatFile is the persisted RFC3339 timestamp of the most
// recent successful heartbeat. Used to skip the boot heartbeat when
// the previous one was sent < 24h ago.
const LastHeartbeatFile = "last_heartbeat"

// dataDir resolves the directory anonde uses to persist install_id
// + last_heartbeat. Order:
//
//  1. $ANONDE_DATA_DIR (anonde-specific anchor; wins outright when
//     set so containerised deployments can point a single volume at
//     both the install_id file and the bbolt DB)
//  2. $XDG_DATA_HOME/anonde (XDG spec)
//  3. $HOME/.local/share/anonde (XDG default)
//  4. $HOME/.anonde (fallback for non-XDG OSes — see fallbackDir)
//
// The ANONDE_DATA_DIR branch deliberately does NOT append "/anonde"
// — the anchor IS the anonde directory, so install_id lands at
// $ANONDE_DATA_DIR/install_id. This matches the bbolt path
// convention ($ANONDE_DATA_DIR/anonde.db) so an operator sees a flat
// layout under one volume.
//
// Darwin doesn't honour XDG by spec but its $HOME is reliable, so the
// $HOME/.local/share/anonde branch works fine there too — operators
// expecting ~/Library paths can set XDG_DATA_HOME or ANONDE_DATA_DIR
// explicitly.
func dataDir() (string, error) {
	if anchor := strings.TrimSpace(os.Getenv("ANONDE_DATA_DIR")); anchor != "" {
		return anchor, nil
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "anonde"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "anonde"), nil
}

// fallbackDir is used when the primary dataDir is unwritable; for
// example a containerised deployment whose $HOME is read-only.
func fallbackDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".anonde"), nil
}

// LoadOrCreateInstallID returns the persisted install ID, creating
// it (and the data directory) on first run. Returns the resolved
// directory alongside the ID so callers can persist last_heartbeat
// next to it without re-resolving.
//
// Fail-soft: if neither dataDir nor fallbackDir is writable the
// function returns a freshly-generated ephemeral ID with an empty
// directory and a nil error — the heartbeat still goes out, just
// without cross-restart correlation. Telemetry must never gate boot.
func LoadOrCreateInstallID() (id string, dir string, err error) {
	primary, perr := dataDir()
	if perr == nil {
		if id, ok := tryLoad(primary); ok {
			return id, primary, nil
		}
		if newID, ok := tryCreate(primary); ok {
			return newID, primary, nil
		}
	}
	fb, ferr := fallbackDir()
	if ferr == nil {
		if id, ok := tryLoad(fb); ok {
			return id, fb, nil
		}
		if newID, ok := tryCreate(fb); ok {
			return newID, fb, nil
		}
	}
	// Both locations unwritable; return an ephemeral ID. Logged by
	// the caller so an operator can see why their install_id rolls
	// every boot.
	newID, gerr := newUUIDv4()
	if gerr != nil {
		return "", "", gerr
	}
	return newID, "", nil
}

// tryLoad reads an existing install_id file. Returns ok=false on any
// I/O error or unparseable content so the caller can fall through to
// create or to the next candidate directory.
func tryLoad(dir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, InstallIDFile))
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", false
	}
	return id, true
}

// tryCreate generates a new install_id and persists it. Returns
// ok=false when the directory can't be created or the file can't be
// written; the caller then falls through to the next candidate.
func tryCreate(dir string) (string, bool) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	id, err := newUUIDv4()
	if err != nil {
		return "", false
	}
	// 0o600 because the file lives in the user's data dir; no
	// secrets in it but other local users have no business reading
	// telemetry identifiers either.
	if err := os.WriteFile(filepath.Join(dir, InstallIDFile), []byte(id+"\n"), 0o600); err != nil {
		return "", false
	}
	return id, true
}

// newUUIDv4 generates a random UUID v4. Inlined to avoid pulling in a
// dependency for one ~15-line function.
func newUUIDv4() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
