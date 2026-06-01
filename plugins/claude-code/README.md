# anonde — Claude Code plugin

A local PII guard for [Claude Code](https://code.claude.com), packaged as a
plugin. Installing it auto-registers two hooks:

- **`UserPromptSubmit`** — scans the prompt you submit and **warns** (default)
  or **blocks** when it contains PII.
- **`PreToolUse`** (`Bash`, `Write`, `Edit`, `MultiEdit`, `NotebookEdit`) —
  scans the text the agent is about to put into an action, so PII can't be
  `curl`'d out, committed, or logged without you knowing.

Detection runs locally: pure-Go pattern recognizers **in-process** by default
(no server), or full GLiNER NER if you point it at a running anonde server.

## Install

```text
/plugin marketplace add anonde-io/anonde
/plugin install anonde@anonde
```

That's it. On the next session start the plugin fetches the `anonde-hook`
binary for your OS/arch (via the repo installer, into
`~/.cache/anonde/`) and the hooks begin firing. If you already have
`anonde-hook` on your PATH (from `curl … | sh` or `go install`), the plugin
uses that instead and downloads nothing.

Run `/hooks` to confirm `anonde` is registered.

## Configure

The hooks honour the same environment variables as the standalone binary —
set them in your shell (the plugin inherits your environment):

| Env var | Default | Meaning |
|---|---|---|
| `ANONDE_HOOK_MODE` | `warn` | `off` \| `warn` \| `block` |
| `ANONDE_HOOK_URL` | _(unset)_ | Point at a running anonde server for full NER |
| `ANONDE_HOOK_LANGUAGE` | `en` | Recognizer language (`de` for German names/places) |
| `ANONDE_HOOK_ENTITIES` | _(all)_ | Restrict to e.g. `EMAIL_ADDRESS,CREDIT_CARD,IBAN_CODE,US_SSN` |
| `ANONDE_HOOK_FAIL_OPEN` | `true` | On detector error: allow (`true`) vs block (`false`) |

Full list and behaviour: [`../../examples/claude-code-hook/README.md`](../../examples/claude-code-hook/README.md).

## What it can and cannot do

The Claude Code hook contract shapes the guarantees: prompts can be **warned or
blocked** but not silently rewritten, and **`Read` results cannot be scrubbed**
(a `PostToolUse` hook can't alter tool output). Use anonde upstream
(anonymize at rest) if read-path leakage is in your threat model. See the
examples README for the full discussion.

## How it's wired

| File | Role |
|---|---|
| `.claude-plugin/plugin.json` | Plugin manifest; points at `hooks/hooks.json` |
| `hooks/hooks.json` | Registers `SessionStart` + `UserPromptSubmit` + `PreToolUse` |
| `scripts/ensure-binary.sh` | `SessionStart`: pre-fetches `anonde-hook` so per-call latency is zero |
| `scripts/run-hook.sh` | Resolves the binary and execs it, passing the event payload through |
| `scripts/_resolve.sh` | Shared resolver: PATH → cache → install on demand |

No binary is committed to this repo — the scripts fetch the right one from the
GitHub Release at runtime, and fail open if it can't be obtained.
