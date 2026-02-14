# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
go build -o nagobot ./cmd/nagobot/

# Install to $GOPATH/bin
go install ./cmd/nagobot/

# Run tests (no test files exist yet)
go test ./...

# Vet
go vet ./...
```

Requires Go 1.22+. Dependencies: `nhooyr.io/websocket`, `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`.

## Architecture

Nagobot is a lightweight personal AI assistant framework written in Go. It supports CLI interaction and Discord as a chat channel, using a ReAct tool-calling loop to process messages through LLM providers.

### Message Flow

1. **Inbound**: Messages arrive from CLI (`ProcessDirect`) or Discord channel and are published to the `bus.MessageBus` inbound channel.
2. **Agent Loop** (`internal/agent/loop.go`): Reads from the inbound channel, builds context (system prompt + session history), and enters a ReAct loop — calling the LLM, executing tool calls, and injecting reflection prompts until the LLM responds with no tool calls.
3. **Outbound**: Final responses are published to the outbound channel, where `MessageBus.DispatchOutbound` routes them to subscribed channel handlers.

### Key Abstractions

- **`llm.Provider`** interface (`internal/llm/provider.go`): Unified `Chat()` method. Two implementations:
  - `OpenAIProvider` — for OpenAI-compatible APIs (OpenRouter, DeepSeek, etc.)
  - `AnthropicProvider` — native Anthropic Messages API. Handles conversion from the internal OpenAI-style message format to Anthropic's content-block format (system extraction, tool_use/tool_result blocks, consecutive role merging).
- **`tool.Tool`** interface (`internal/tool/tool.go`): `Name()`, `Description()`, `Parameters()` (JSON Schema), `Execute()`. Registered in a `Registry`. Tool definitions are emitted in OpenAI function-calling format.
- **`channel.Channel`** interface (`internal/channel/channel.go`): `Start()`, `Stop()`, `Send()`. Currently only Discord is implemented.
- **`bus.MessageBus`** (`internal/bus/queue.go`): Decouples channels from the agent via buffered Go channels and a pub/sub outbound dispatch.

### Context & Memory

`ContextBuilder` (`internal/agent/context.go`) assembles the system prompt from:
- Runtime identity (time, OS, workspace path)
- Bootstrap files from workspace: `AGENTS.md`, `SOUL.md`, `USER.md`, `TOOLS.md`, `IDENTITY.md`
- `MemoryStore` (`internal/agent/memory.go`): reads `memory/MEMORY.md` (long-term) and today's date file (daily notes)

### Sessions

`session.Manager` (`internal/session/manager.go`) persists conversation history as JSONL files in `~/.nagobot/sessions/`. Sessions are keyed by `channel:chatID`.

### Configuration

Config lives at `~/.nagobot/config.json`, loaded by `internal/config/loader.go`. Provider selection (`config.GetProvider`) matches the model name against provider keywords, falling back to the first provider with an API key set. The `anthropic` provider name triggers `AnthropicProvider`; all others use `OpenAIProvider`.

### CLI Commands

Entry point is `cmd/nagobot/main.go` (thin dispatcher). CLI UI lives in `internal/cli/`:
- **`chat.go`** — Interactive chat TUI (bubbletea alt-screen with viewport, textinput, spinner) and single-message mode (inline spinner).
- **`status.go`** — Styled status display (lipgloss).
- **`onboard.go`** — Onboard wizard with arrow-key selection (bubbletea) and styled output.
- **`styles.go`** — Shared lipgloss styles and constants (Logo, Version, color palette).

Agent logs redirect to `~/.nagobot/agent.log` during TUI mode.

## Missing Features (vs nanobot Python reference)

The upstream Python project (nanobot, at `/Users/joe/Documents/nanobot`) has the following features not yet ported to this Go codebase.

### Channels (8 missing)

Only Discord is implemented. Missing: **Telegram**, **WhatsApp** (Node.js bridge + QR login), **Feishu** (WebSocket), **Slack** (Socket Mode), **Email** (IMAP/SMTP), **QQ** (botpy SDK), **DingTalk** (Stream Mode), **Mochat** (Socket.IO). Also missing a **Channel Manager** for unified multi-channel lifecycle.

### Tools (2 missing)

- **Web tool** (`tools/web.py`) — web scraping / Brave Search integration
- **Cron tool** (`tools/cron.py`) — create/manage scheduled tasks from within a conversation

### Services (2 missing)

- **Cron Service** (`cron/service.py`) — scheduled task scheduler supporting cron expressions and fixed intervals
- **Heartbeat Service** (`heartbeat/service.py`) — proactive wake-up service allowing the bot to initiate conversations

### LLM Providers (10+ missing)

nagobot has two hand-written providers (OpenAI, Anthropic). nanobot uses LiteLLM + a declarative Provider Registry (`ProviderSpec`) supporting 12+ providers: OpenRouter, DeepSeek, Groq (+ Whisper voice transcription), Gemini, MiniMax, AiHubMix, DashScope/Qwen, Moonshot/Kimi, Zhipu/GLM, vLLM (local). Also missing: **voice transcription module** (`providers/transcription.py`).

### Config

nanobot uses Pydantic v2 schema validation (`config/schema.py`). nagobot only does plain JSON loading with no schema validation.

### CLI Commands (3 missing)

- `channels login` — WhatsApp QR login
- `channels status` — show channel connection status
- `cron add/list/remove` — manage scheduled tasks from CLI

### Built-in Skills (4+ missing)

nanobot bundles: **github**, **weather**, **tmux** (with scripts), **skill-creator**, **summarize**. nagobot has the skills loader but fewer bundled skills.

### Other

- **Docker**: nanobot has a Dockerfile; nagobot does not
- **Security sandbox**: nanobot has `restrictToWorkspace` config + shell command guard regex; nagobot has no equivalent
- **Tests**: nanobot has `tests/` with test files; nagobot has none
- **Context compression**: nanobot auto-summarizes old conversation segments when the context window fills; nagobot now has this (in-flight `compressMessages` in the ReAct loop + between-turn `consolidateMemory`)
- **WhatsApp bridge**: nanobot has a `bridge/` directory with a Node.js/TypeScript WhatsApp bridge
