# anonde — Claude Code hook

A local PII guard for [Claude Code](https://code.claude.com). It runs anonde's
detector on every prompt you submit and every action the agent is about to take
(`Bash`, `Write`, `Edit`, …), and **warns** (default) or **blocks** when
sensitive data is about to reach the model or leave your machine.

Detection runs **in-process** by default: pure-Go pattern recognizers, no
server, no network, no Python, no `jq`. Point it at a running anonde server to
upgrade to full GLiNER NER (names, places, organizations).

```text
You: email the invoice to anna@example.com, account DE89370400440532013000
     ⚠️  anonde: PII in prompt (EMAIL_ADDRESS×1, IBAN_CODE×1)
```

## Install

### Plugin (lowest friction)

```text
/plugin marketplace add anonde-io/anonde
/plugin install anonde@anonde
```

That's it — the hooks auto-register. On the next session start the plugin
fetches the `anonde-hook` binary for your OS/arch (via the repo installer, into
`~/.cache/anonde/`). If you already have `anonde-hook` on your PATH (from
`curl … | sh` or `go install`), it uses that and downloads nothing. Run
`/hooks` to confirm `anonde` is registered.

### Manual install (no plugin)

Get the binary — either way puts `anonde-hook` on disk:

```bash
curl -fsSL https://raw.githubusercontent.com/anonde-io/anonde/main/install.sh | sh
# installs into ~/.local/bin (override with ANONDE_INSTALL_DIR)

go install github.com/anonde-io/anonde/cmd/anonde-hook@latest   # needs a Go toolchain
# or, from a checkout: make build-hook  → ./bin/anonde-hook
```

Then merge this into `~/.claude/settings.json` (all projects) or
`.claude/settings.json` (this project, committable):

```jsonc
{
  "hooks": {
    "UserPromptSubmit": [
      { "hooks": [ { "type": "command", "command": "anonde-hook" } ] }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash|Write|Edit|MultiEdit|NotebookEdit",
        "hooks": [ { "type": "command", "command": "anonde-hook" } ]
      }
    ]
  }
}
```

If `anonde-hook` isn't on Claude Code's PATH, use an absolute path or
`${CLAUDE_PROJECT_DIR}/bin/anonde-hook`. Run `/hooks` to confirm.

## Configure

All optional, set in your shell or inline in the hook `command` (e.g.
`"command": "ANONDE_HOOK_MODE=block anonde-hook"`). The plugin inherits your
shell environment.

| Env var | Default | Meaning |
|---|---|---|
| `ANONDE_HOOK_MODE` | `warn` | `off` \| `warn` (surface) \| `block` (deny prompts / tool calls) |
| `ANONDE_HOOK_URL` | _(unset)_ | Point at a running anonde server (e.g. `http://localhost:8081`) for full NER. Unset = in-process patterns only. |
| `ANONDE_HOOK_TENANT` | `claude-code-hook` | Vault tenant id used in server mode. |
| `ANONDE_HOOK_LANGUAGE` | `en` | Recognizer language. Set `de` for German name/place coverage. |
| `ANONDE_HOOK_MIN_SCORE` | `0.40` | Drop findings below this confidence. |
| `ANONDE_HOOK_ENTITIES` | _(all)_ | Comma list to restrict to, e.g. `EMAIL_ADDRESS,CREDIT_CARD,IBAN_CODE,US_SSN`. |
| `ANONDE_HOOK_TIMEOUT_MS` | `1500` | Server-call timeout. |
| `ANONDE_HOOK_FAIL_OPEN` | `true` | On detector error / unreachable server: `true` = allow (never break your session), `false` = block. |

### In-process vs server mode

- **In-process (default).** Catches structured PII — emails, credit cards,
  IBANs, SSNs, phone numbers, IPs, crypto wallets, URLs, and the pattern-based
  name/place recognizers. Zero setup, instant, offline.
- **Server mode** (`ANONDE_HOOK_URL=…`). Adds GLiNER NER, so free-text names,
  places, and organizations are caught too. Start a server with
  `docker run -p 8081:8080 ghcr.io/anonde-io/anonde-ner:latest` (or `make run-ner`).

## What it can and cannot do

Claude Code's hook contract shapes the guarantees:

- ✅ **Prompts** — detected and **warned** (default) or **blocked** (`block`
  mode). The contract does not let a hook silently rewrite your prompt, so the
  honest options are "tell you" or "stop it" — not "quietly redact it".
- ✅ **Agent actions** (`Bash`, `Write`, `Edit`, `MultiEdit`, `NotebookEdit`) —
  the text the agent is about to embed in a command or file is scanned, so PII
  can't be `curl`'d to an external host, baked into a committed file, or written
  to a log without you knowing.
- ⚠️ **File reads are not scrubbed.** When the agent `Read`s a file, its
  contents go to the model and a `PostToolUse` hook cannot alter that result.
  Use anonde upstream (anonymize the data at rest) if read-path leakage is in
  your threat model.

The hook **fails open** by default: a malformed payload, a detector error, or an
unreachable server results in "allow" so a misconfiguration can never brick a
session. Flip `ANONDE_HOOK_FAIL_OPEN=false` to fail closed.

## Try it

```bash
echo '{"hook_event_name":"UserPromptSubmit","prompt":"ping john@example.com, card 4111111111111111"}' | anonde-hook
# {"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"anonde detected PII in this prompt (CREDIT_CARD×1, EMAIL_ADDRESS×1); ..."}, ...}

echo '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}' | anonde-hook
# (clean → no output, exit 0)
```

## How the plugin is wired

| File | Role |
|---|---|
| `.claude-plugin/plugin.json` | Plugin manifest; points at `hooks/hooks.json` |
| `hooks/hooks.json` | Registers `SessionStart` + `UserPromptSubmit` + `PreToolUse` |
| `scripts/ensure-binary.sh` | `SessionStart`: pre-fetches `anonde-hook` so per-call latency is zero |
| `scripts/run-hook.sh` | Resolves the binary and execs it, passing the event payload through |
| `scripts/_resolve.sh` | Shared resolver: PATH → cache → install on demand |

No binary is committed to this repo — the scripts fetch the right one from the
GitHub Release at runtime, and fail open if it can't be obtained.
