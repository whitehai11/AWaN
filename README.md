# AWaN Core

AWaN, short for Agent Without a Name, is a modular open-source AI agent runtime. The Go core is structured as a local service that GUI, TUI, and CLI clients can connect to over HTTP.

This repository implements the runtime layer responsible for:

- agent execution
- model communication
- short-term and long-term memory
- isolated agent filesystem access
- local interface APIs
- authentication plumbing for model providers

UI clients are intentionally out of scope here. The core exposes a stable local API so those interfaces can be built independently.

## Architecture

The runtime is organized under `core/`:

- `core/agent`: agent execution and the prompt -> model -> response loop
- `core/agent/loader.go`: `.awand` agent definition loading
- `core/agent/capabilities.go`: token-efficient environment ID generation
- `core/agent/system_prompt.go`: minimal system prompt and state snapshot builder
- `core/models`: model abstraction, registry, and built-in OpenAI and Ollama providers
- `core/runtime`: the central service container
- `core/memory`: in-memory and JSON-backed memory modules plus a vector-memory extension point
- `core/filesystem`: isolated storage rooted at `~/.awan/`
- `core/tools`: secure tool manifest loading and isolated plugin execution
- `core/interfaces`: local HTTP API and server bootstrap
- `core/auth`: OAuth token storage and refresh support
- `core/config`: JSON runtime config plus `.awan` profile parsing
- `core/utils`: logger
- `core/types`: shared agent and memory types

Shared internal modules:

- `internal/updater`: background GitHub-release auto updates

## Runtime Mode

Running `awan` starts the local service:

```bash
go run .
```

Expected startup logs:

```text
[AWAN] Runtime started
[AWAN] API listening on localhost:7452
```

The service exposes a local API for GUI and TUI clients.

On startup, AWaN Core also checks for updates asynchronously in the background. If a newer GitHub release is available, it downloads the matching platform binary, stages the replacement safely, and restarts.

## Optional CLI Mode

You can still run a one-shot prompt from the terminal:

```bash
go run . run "Explain quantum computing"
```

## Local API

The runtime listens on `localhost:7452` by default and exposes:

- `POST /agent/run`
- `POST /agent/chat`
- `GET /memory`
- `POST /memory/store`
- `GET /agents`
- `GET /files`
- `POST /tools/execute`

Example `POST /agent/run` body:

```json
{
  "agent": "default",
  "prompt": "Explain Rust ownership"
}
```

Example `POST /memory/store` body:

```json
{
  "agent": "default",
  "role": "note",
  "content": "User is interested in systems programming"
}
```

## Agent Filesystem

AWaN isolates runtime state under:

```text
~/.awan/
  agents/
  memory/
  files/
  config/
  tools/
  sandbox/
```

All agent file operations are expected to go through `core/filesystem/agentfs.go`. This keeps the runtime from directly touching arbitrary system paths.

The runtime only exposes file names to agents by default. File contents are not included in the prompt unless a tool explicitly reads them.

## Configuration

### Runtime JSON Config

AWaN automatically loads `awan.config.json` from the current working directory when present.

Example:

```json
{
  "defaultModel": "openai",
  "defaultAgent": "default",
  "api": {
    "host": "localhost",
    "port": 7452
  },
  "storage": {
    "rootPath": ""
  },
  "openai": {
    "model": "gpt-4o-mini",
    "baseURL": "https://api.openai.com"
  },
  "ollama": {
    "model": "llama3",
    "baseURL": "http://localhost:11434"
  }
}
```

### Custom `.awan` Config

AWaN also supports simple key-value agent profile files:

```text
agent_name = research-agent
model = openai
memory = enabled
```

These files are intended for agent-level configuration and can be parsed with the config package.

### Agent Definition Files

Runtime agents are loaded automatically from:

```text
~/.awan/agents/
```

Each agent uses the `.awand` format:

```text
name = research-agent
model = openai
memory = true
tools = browser,filesystem
description = research information
```

On runtime start, AWaN scans that directory, loads all `*.awand` files, and registers them for execution.

Tool permissions are declared per agent:

```text
name = research-agent
model = openai
memory = true
tools = browser,filesystem.read
description = research information
```

An agent can only execute tools listed in its own definition.

## Tool Plugin System

AWaN supports external tools under:

```text
~/.awan/tools/
  browser/
    tool.json
    main.ts
  filesystem/
    tool.json
    main.ts
```

Each tool must include a `tool.json` manifest:

```json
{
  "name": "filesystem.read",
  "description": "Read a file",
  "parameters": {
    "path": "string"
  }
}
```

The runtime loads tool manifests automatically and executes tools as isolated subprocesses over JSON stdin/stdout.

### Built-in Code Runner

AWaN also includes a built-in secure code execution tool:

```text
tool name: code.execute
```

Example tool call:

```json
{
  "tool": "code.execute",
  "args": {
    "language": "python",
    "code": "print('hello world')"
  }
}
```

Example result:

```json
{
  "result": {
    "stdout": "hello world",
    "stderr": ""
  }
}
```

Supported languages:

- Python
- JavaScript
- Go

Code executes inside:

```text
~/.awan/sandbox/
```

The runner enforces:

- isolated working directories
- execution time limits
- restricted runtime environment variables
- best-effort memory limits for supported runtimes
- disabled Go module downloads

Security rules:

- tools run in separate processes
- tool permissions are enforced per agent
- file path arguments are constrained to `~/.awan/files`
- tools receive `AWAN_FILES_ROOT` rather than unrestricted filesystem paths
- file contents are never sent automatically in the base agent snapshot

This is a best-effort runtime sandbox and can be strengthened later with OS-level isolation.

## Token-Efficient Environment

AWaN now uses a token-efficient environment system for runtime prompts.

For each request, the agent receives:

- a compact environment ID
- agent name
- file names only
- memory IDs only

Example:

```text
You are an AI agent running inside AWaN.

Environment ID: 7fa2

To use tools respond with JSON.

AGENT: research-agent

FILES:
notes.md,tasks.md

MEM:
m21,m44
```

Token-optimization rules in the runtime:

- never send full file contents automatically
- never send the full memory database
- only send memory IDs in request snapshots
- only send file names in request snapshots
- only send the environment ID instead of the full environment description

## OAuth

OAuth tokens are stored locally in:

```text
~/.awan/auth.json
```

The OAuth manager supports:

- `Login(code)`
- `Logout()`
- `GetAccessToken()`
- `AuthorizationURL(state)`

OpenAI-oriented environment variables:

- `OPENAI_API_KEY`
- `OPENAI_CLIENT_ID`
- `OPENAI_CLIENT_SECRET`
- `OPENAI_REDIRECT_URI`
- `OPENAI_OAUTH_AUTH_URL`
- `OPENAI_OAUTH_TOKEN_URL`
- `OPENAI_OAUTH_SCOPES`

## Auto Updates

AWaN applications define a version constant and compare it against the latest GitHub release on startup using:

```text
https://api.github.com/repos/{repo}/releases/latest
```

Auto updates run in the background so startup is not blocked.

You can disable auto updates with:

```text
~/.awan/config/runtime.awan
```

Example:

```text
auto_update = false
```

## GitHub Releases

AWaN Core includes an automated GitHub Actions release pipeline in [build-release.yml](C:\Users\maro\Desktop\AWaN\.github\workflows\build-release.yml).

When code is pushed to `main`, GitHub Actions will:

- build a Windows executable
- build a Linux binary
- place both files in `dist/`
- create a GitHub Release named `Build <run_number>`
- publish the binaries as release assets using the tag `auto-<run_number>`

To trigger an automatic release, push or merge a commit into the `main` branch.

If you want to switch from push-based auto releases to tag-based versioning later, change the workflow trigger from:

```yaml
on:
  push:
    branches:
      - main
```

to something like:

```yaml
on:
  push:
    tags:
      - "v*"
```

Then replace the generated release tag and name with the pushed git tag, for example `${{ github.ref_name }}`.

## Current Scope

Implemented here:

- runtime service
- local API for interfaces
- model registry and built-in providers
- short-term and long-term memory
- token-efficient environment snapshots
- `.awand` agent definition loading
- secure external tool plugins
- isolated agent filesystem
- `.awan` profile parsing
- OAuth token storage and refresh plumbing
- optional CLI prompt execution

Not implemented yet:

- browser automation
- web UI
- TUI client
- advanced tool orchestration
- production vector indexing
