# AGENTS.md — uapi-cli

This file tells AI coding agents how to use the **official UAPI CLI** for
the [uapis.cn](https://uapis.cn) public API platform.

## What this package is

A Go-implemented command-line client for UAPI, distributed via npm using the
optional-dependency pattern (only the binary for the current platform is
downloaded at install time). It can be used both as a one-shot HTTP client
and as an interactive REPL for LLM-driven workflows.

```bash
# One-shot
npx uapi misc weather --city "北京"

# Or install globally
npm i -g uapi-cli
uapi --version
uapi misc weather --city "上海"

# Interactive REPL (good for LLM agents that need a tool)
uapi repl
```

## Common commands

```bash
# Discover endpoints
uapi list                         # list all top-level groups
uapi list misc                    # list all operations under "misc"

# Call an endpoint by operation id
uapi call get-misc-weather --city 上海

# Call by tag.method
uapi misc get-weather --city 上海

# Format/output control
uapi misc get-weather --city 上海 --output json
uapi misc get-weather --city 上海 --output table
```

## Authentication

- Many UAPI endpoints (network, text, random, convert, weather, time,
  hotboard) work with no key.
- For paid endpoints, set the key once via env var or flag:

```bash
export UAPI_KEY=…             # used as X-API-Key
uapi image post-image-ocr --url https://…/photo.png

# Or per-invocation
uapi --api-key sk_… image post-image-ocr --url https://…/photo.png
```

## Errors

Non-2xx responses bubble up the JSON body verbatim:
`{code, success: false, error, request_id?}`. The exit code is the HTTP
status divided by 100 (e.g. `4` for any 4xx).

## Rate limits

Headers `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`,
`Retry-After` are surfaced via stderr on every call. The CLI honors
`Retry-After` automatically when invoked with `--retry`.

## Discovery

Operation list comes from <https://uapis.cn/openapi.json>. The CLI caches
the spec locally under `~/.uapi/openapi.json` and refreshes once a day.

## Related repos

- MCP server: <https://github.com/AxT-Team/uapi-mcp> — same endpoints as MCP tools.
- Skills bundle: <https://github.com/AxT-Team/uapi-agent-skills>.
- Typed SDKs: `uapi-sdk-typescript`, `uapi-sdk-python`, `uapi-sdk-go`,
  `uapi-sdk-rust`, `uapi-sdk-java`, `uapi-sdk-csharp`, `uapi-sdk-cpp`,
  `uapi-sdk-php`.
