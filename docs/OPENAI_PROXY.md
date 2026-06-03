# Use anonde as an OpenAI proxy

The lowest-friction integration: point your existing OpenAI SDK at
anonde instead of `api.openai.com`. anonde anonymizes the prompt,
forwards it to the real provider, de-anonymizes the response, and hands
it back in OpenAI shape. No plugin, no code change beyond the base URL.
Works with the raw OpenAI SDK, LangChain, or anything that speaks the
OpenAI API.

Start the server with the upstream configured:

```bash
ANONDE_OPENAI_BASE_URL=https://api.openai.com/v1 \
ANONDE_OPENAI_API_KEY=sk-...your-real-key... \
ANONDE_ADDR=:8081 go run ./cmd/anonde/
```

Then swap the base URL in your client:

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8081/v1", api_key="unused")

resp = client.chat.completions.create(
    model="openai/gpt-4o",   # provider/model; "openai/" is optional
    messages=[{"role": "user",
               "content": "Email a summary to sarah.chen@acme.example"}],
)
# The provider only ever saw <EMAIL_ADDRESS_…>; the reply you get back
# has the real address restored.
```

The endpoint is a single OpenAI-shaped `POST /v1/chat/completions`, so
the client base URL is byte-identical to a real OpenAI swap. The
upstream provider is selected in-band (the OpenRouter convention) by a
`provider/model` prefix on the `model` field: `openai/gpt-4o` routes to
OpenAI and forwards the bare `gpt-4o` upstream. A model with no prefix
defaults to OpenAI. v0.1 proxies OpenAI only; an `anthropic/…` model is
rejected with a clear error until Anthropic routing lands in v0.2.

| Env var | Default | Purpose |
|---|---|---|
| `ANONDE_OPENAI_BASE_URL` | `https://api.openai.com/v1` | Any OpenAI-compatible endpoint, incl. a local Ollama (`http://localhost:11434/v1`). |
| `ANONDE_OPENAI_API_KEY` | _(empty)_ | Forwarded as `Authorization: Bearer`. Leave empty for keyless upstreams like Ollama. |
| `ANONDE_PROXY_TENANT` | `openai-proxy` | Vault tenant when a request carries no `X-Anonde-Tenant` header. |
| `ANONDE_PROXY_TIMEOUT` | `120s` | Upstream request timeout (shared across all proxied providers). |

**Known limitation (v0.1):** non-streaming only. A `stream: true` request
is rejected with a clear error rather than silently downgraded.
Streaming SSE de-anonymization lands in v0.1.1. Anthropic and Gemini
upstreams (selected by the `anthropic/` / `gemini/` model prefix) are on
the roadmap.
