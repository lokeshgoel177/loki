# Loki Project Roadmap: Persistent Local AI Agent Runtime (v2 Architecture)

> **Vision:** _The AI agent is a local service, not a terminal process. Code is fetched on demand, not cached in advance. Build the agent once. Render it everywhere._

This document outlines the detailed development roadmap for **Loki**, a lightweight, persistent local AI coding-agent platform. It reflects the **v2 Architecture** established in [Loki Master Architecture & System Design Document](DESIGN.md), incorporating a **zero-index, lazy-fetch, minimal-cache** philosophy while delivering daemon-based multi-client multiplexing and detached execution.

---

## 1. Architectural Principles & North Star

The roadmap is strictly guided by the architectural invariants defined in `docs/DESIGN.md`:

1. **Persistent Daemon Backend (`agentd`):** A single per-user Go service manages all sessions, tool connections, LLM communication, subprocesses, context, and state.
2. **Thin, Ephemeral Clients:** Terminal CLIs, IDE extensions (Neovim, VS Code), and future Web UIs are lightweight rendering viewports that connect, attach, detach, and reconnect via local IPC without disrupting running tasks.
3. **Zero Pre-Indexing & Lazy Code Retrieval:** No vector databases, no AST indexers, and no persistent filesystem watchers in the core retrieval path. Code is discovered on demand using fast command-line utilities (`ripgrep`, glob, line-sliced reads).
4. **Context-as-Cache with Ephemeral Prompt Caching:** The LLM's context window serves as the active working memory. Static blocks (system prompt, tool schemas, `AGENTS.md` / `Agents.md`) leverage API ephemeral prompt caching for 90% cost and 85%+ latency reductions.
5. **Spawn-and-Die Tooling & Ingestion Truncation:** Tools run as transient subprocesses or direct Go I/O that exit immediately, leaving **0 bytes on the daemon heap**. All tool output passes through an aggressive truncation gate (500 lines / 40KB cap) before entering context.
6. **Exact String Replacement File Editing:** Avoid brittle unified diff / patch generation. Edits use exact string replacement (`old_string` -> `new_string`) with uniqueness validation.
7. **Tool Danger Classification & Permission Handshake:** Safe read-only tools auto-execute; workspace modifications show diffs and require consent; shell commands use heuristic allowlists/denylists.
8. **Shared System Resources:** Multiplex connections across all clients—a single MCP manager, one LLM connection pool, and one subprocess supervisor.
9. **Decoupled Event Streaming:** Daemon activity is broadcast through a multiplexed, filterable pub/sub event broker, allowing clients to subscribe selectively to relevant streams.
10. **Capability-Gated Lua Extensibility:** Guest extensions use an embedded pure-Go Lua VM (`gopher-lua`) with strict capability sandboxing (no WASM).
11. **Durable vs. Ephemeral State Partitioning:** Full execution state, message history, checkpoints, and permissions are stored in SQLite (WAL mode), while active streaming buffers remain in daemon memory.
12. **Radically Low Resource Footprint:** Target **< 20MB idle RSS** and **< 50MB active RSS** for the daemon, with **0% idle CPU**.

---

## 2. Roadmap Overview

```text
┌──────────────────────────────────────────────────────────────────────────────────┐
│                                LOKI ROADMAP (v2)                                 │
└──────────────────────────────────────────────────────────────────────────────────┘
  Phase 0: Foundation & Core Skeleton (v0.1.0)
    │  ├── Go workspace & modular package scaffolding (per v2 package layout)
    │  ├── Cross-platform IPC transport (Unix Sockets & Windows Named Pipes)
    │  └── Length-prefixed framed JSON protocol & handshake
    ▼
  Phase 1: MVP Daemon, Lazy Tools & CLI Client (v0.2.0 – v0.3.0)
    │  ├── Background daemon lifecycle & auto-spawn supervisor
    │  ├── In-memory event broker with wildcard subscriptions
    │  ├── Session engine & ReAct agent loop (goroutine per active session)
    │  ├── Anthropic LLM provider with ephemeral prompt caching breakpoints
    │  ├── Lazy retrieval & editing tools (Read, Glob, Grep/ripgrep, List, Edit, Write, Bash)
    │  ├── Ingestion output truncation gate (500 lines / 40KB cap)
    │  ├── Context manager with AGENTS.md project loader & auto-compaction
    │  ├── SQLite persistence (WAL mode, CGO-free) & artifact disk store
    │  └── Minimal terminal CLI thin client with live markdown streaming
    ▼
  Phase 2: Multi-Client Multiplexing, MCP & Lua Extensibility (v0.4.0 – v0.6.0)
    │  ├── Multi-client simultaneous session attachment & live rehydration
    │  ├── Detached execution & graceful re-attachment (in-memory ring buffer)
    │  ├── Shared MCP manager (single instance per server across sessions)
    │  ├── Tool danger classification & interactive permission handshake
    │  ├── Embedded Lua runtime (gopher-lua) with capability API bindings
    │  └── CLI session management commands (attach, sessions, kill, logs, compact)
    ▼
  Phase 3: IDE Integrations & Rich Terminal UI (v0.7.0 – v0.8.0)
    │  ├── Interactive Bubble Tea / Lipgloss terminal UI (split-view, virtual scroll, diffs)
    │  ├── Neovim Lua plugin (loki.nvim) via local IPC
    │  └── VS Code extension (loki-vscode) via local IPC
    ▼
  Phase 4: Multi-Agent Orchestration & Web/Remote Interfaces (v0.9.0 – v1.0.0)
    │  ├── Multi-agent coordination (planner, coder, test runner, reviewer subagents)
    │  ├── Workspace file reservation & subagent context isolation
    │  ├── Advanced multi-turn context compaction
    │  └── Local Web dashboard & authenticated remote gateway
    ▼
  Phase 5: Production Hardening, Ecosystem & Scaling (Post-v1.0)
    │  ├── Benchmarking & memory optimization (<20MB daemon idle RSS target)
    │  ├── OS packaging (Homebrew, Scoop, AUR, systemd/Windows services)
    │  └── Compact wire protocol evaluation (Protobuf/FlatBuffers if needed)
```

---

## 3. Detailed Phase Breakdown

### Phase 0: Foundation & Core Skeleton

**Objective:** Lay down the Go project architecture, standard tooling, and establish a cross-platform local IPC transport with framed messaging.

#### Milestone 0.1: Project Scaffolding & Tooling

- [x] Initialize Go module (`go.mod`, Go 1.23+) at repository root.
- [x] Implement directory structure matching `docs/DESIGN.md` Section 5.1 (Package Layout):
  ```text
  cmd/
    agentd/             # Daemon entrypoint
    loki/               # CLI thin client entrypoint
  internal/
    daemon/             # Lifecycle, startup, shutdown, PID management
    agent/              # ReAct execution loop, turn orchestration
    session/            # Session lifecycle, state machine, goroutine management
    context/            # Token budget, auto-compaction, Agents.md discovery
    llm/                # Model provider interface and adapters (Anthropic/OpenAI)
    tools/              # Tool registry, dispatch, and built-in tool implementations
    truncation/         # Ingestion output truncation gate
    permission/         # Danger classification, approval handshake
    mcp/                # MCP client manager, tool discovery
    process/            # Subprocess spawn-and-die, process group isolation
    events/             # In-memory pub/sub broker
    ipc/                # Transport abstraction (Unix sockets, Windows named pipes)
    protocol/           # Wire framing, envelope encoding, versioning
    persistence/        # SQLite operations, schema migrations, artifact disk store
    config/             # Configuration loading, permission policies
  plugins/
    api/                # Capability API contracts for guest plugins
    lua/                # Embedded gopher-lua VM, capability export bindings
  pkg/
    client/             # Public client SDK for IDE extensions and external tools
  ```
- [x] Configure structured logging with standard library `log/slog`.
- [x] Configure configuration management (JSON/TOML loader with OS-specific default paths: `~/.config/loki/` on Linux/macOS, `%APPDATA%\Loki\` on Windows).
- [x] Set up GitHub Actions CI with cross-platform matrix testing (`ubuntu-latest`, `macos-latest`, `windows-latest`).

#### Milestone 0.2: Cross-Platform IPC Transport Layer

- [x] Define abstract transport interfaces:
  ```go
  type Transport interface {
      Listen(addr string) (net.Listener, error)
      Dial(addr string) (net.Conn, error)
  }
  ```
- [x] Implement Unix Domain Socket transport for Linux and macOS (`$XDG_RUNTIME_DIR/loki/agentd.sock` or `~/.loki/agentd.sock`) with POSIX permission checks (`0600`).
- [x] Implement Windows Named Pipe transport (`\\.\pipe\loki-agentd-<username>`) with secure DACLs (restricted to current user token).
- [x] Implement length-prefixed message framing layer:
  - 4-byte big-endian payload length header.
  - JSON-encoded message envelope: `version`, `id`, `type`, `payload`, `timestamp`.
  - Handle partial reads, write buffering, and frame reassembly.
- [x] Write integration test verifying bidirectional streaming and 10,000 framed message exchanges across Unix sockets and Windows pipes.

---

### Phase 1: MVP Daemon, Lazy Tools & CLI Client

**Objective:** Deliver an end-to-end coding agent using the Claude Code lazy retrieval model: a background `agentd` daemon that handles a session, streams LLM completions with prompt caching, executes on-demand search and edit tools, truncates bloated output, persists data in SQLite, and communicates with a minimal terminal CLI client.

#### Milestone 1.1: Daemon Lifecycle & Auto-Spawn Supervisor

- [ ] Implement `agentd` background process management:
  - PID file management with stale lock detection (`~/.loki/agentd.pid`).
  - Graceful shutdown on `SIGINT`/`SIGTERM` closing active sessions and flushing DB.
- [ ] Implement client auto-spawn supervisor in `loki` CLI:
  - Client attempts IPC connection.
  - If daemon is not running, client launches `agentd` detached as a background subprocess, polls socket availability with exponential backoff (up to 3 seconds), and completes handshake.

#### Milestone 1.2: Multiplexed Event Broker

- [ ] Implement thread-safe in-memory publish/subscribe broker (`internal/events`):
  - Topic/pattern subscription system supporting exact matching (`session.abc123.tool.started`) and wildcards (`session.abc123.*`, `*.delta`).
  - Buffered client subscriber channels with drop/backpressure metrics and slow consumer protection.
- [ ] Define canonical core event taxonomy:
  - `session.created`, `session.state_changed`, `session.finished`, `session.failed`, `session.compacted`
  - `message.delta`, `message.completed`
  - `tool.started`, `tool.output`, `tool.completed`, `tool.error`
  - `permission.requested`, `permission.resolved`

#### Milestone 1.3: Session Engine & ReAct Agent Loop

- [ ] Implement session state machine (`internal/session`):
  - States: `Idle`, `Running`, `Executing`, `AwaitingPermission`, `Terminated`.
  - Dedicated goroutine per active session. Idle sessions consume zero CPU.
  - Context cancellation (`context.WithCancel`) tied to stop/abort requests.
- [ ] Implement ReAct execution loop (`internal/agent`):
  - Ingests user prompt -> updates context -> calls LLM streaming API.
  - Dispatches `tool_use` blocks to tool executor.
  - Streams `message.delta` and `tool.output` events in real-time.
  - Ingests truncated `tool_result` payloads back into context.
  - Loops until LLM returns `stop_reason: "end_turn"` or error.

#### Milestone 1.4: LLM Provider with Ephemeral Prompt Caching

- [ ] Define generic LLM provider interface (`internal/llm`):
  ```go
  type Provider interface {
      StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
  }
  ```
- [ ] Implement Anthropic Claude Messages API adapter:
  - Server-Sent Events (SSE) parser emitting chunks and token counters.
  - **Ephemeral Prompt Caching:** Place `cache_control: {"type": "ephemeral"}` breakpoint on the static prefix (system instructions + tool schemas + `AGENTS.md`).
  - Error recovery: transient network retry with jittered exponential backoff.

#### Milestone 1.5: Lazy Retrieval & Editing Tool Set

- [ ] Implement stateless, spawn-and-die built-in tools (`internal/tools`):
  - `ReadTool`: Direct `os.ReadFile` with line slicing (`start_line`, `end_line`), capped at 500 lines or 40KB.
  - `GlobTool`: Fast file pattern matching via `filepath.WalkDir`, capped at 100 results.
  - `GrepTool`: Regex search spawning native `ripgrep` (`rg --json`) as a transient subprocess, capped at 50 matches (file, line, trimmed content). Subprocess terminates in milliseconds.
  - `ListTool`: Directory listing with depth limit and child summary.
  - `EditTool`: Exact string replacement (`old_string` -> `new_string`) with uniqueness validation (errors on 0 or >1 matches) and atomic file writes.
  - `WriteTool`: Atomic file creation and overwrite with automatic directory scaffolding.
  - `BashTool`: Subprocess command execution via `os/exec.CommandContext`, isolated with process groups (`Setpgid` / Windows Job Objects) and configurable execution timeouts.

#### Milestone 1.6: Ingestion Truncation Gate, Context Management & `AGENTS.md`

- [ ] Implement output truncation gate (`internal/truncation`):
  - Intercepts all tool stdout/stderr before context insertion.
  - Rules: Max 500 lines, max 40KB. If exceeded, retains first 200 + last 100 lines with `[truncated: N lines omitted]` indicator.
- [ ] Implement project context loader (`internal/context`):
  - Discovery hierarchy: `~/.config/loki/AGENTS.md` -> `<repo>/AGENTS.md` -> `<repo>/.loki/AGENTS.md` -> `<cwd>/AGENTS.md` (supports `AGENTS.md`, `Agents.md`, and `agents.md`).
  - Injects parsed guidelines into the cached static system prompt prefix.
- [ ] Implement context compaction engine:
  - Token budget tracker with model-specific counters.
  - Auto-compaction: Triggers when session token count reaches 75% of context window capacity.
  - Compaction loop: Pauses agent loop, asks LLM to produce structured summary (goals, files modified, test results, next steps), and compresses history down to `[Cached Prefix] + [Summary] + [Last 2 Turns]`.
  - Manual `/compact` slash command support.

#### Milestone 1.7: SQLite Persistence & Artifact Disk Store

- [ ] Embed SQLite engine (using CGO-free `modernc.org/sqlite`).
- [ ] Enable WAL mode (`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`).
- [ ] Database schema migrations:
  - `sessions` (id, working_dir, git_branch, state, metadata, created_at, updated_at)
  - `messages` (id, session_id, role, content, token_count, created_at)
  - `tool_calls` (id, session_id, message_id, tool_name, input_json, output_text, output_ref, status, exit_code, duration_ms, created_at)
  - `permissions` (id, session_id, tool_name, command, decision, created_at)
  - `compactions` (id, session_id, summary, old_tokens, new_tokens, created_at)
- [ ] Artifact disk manager: Tool outputs exceeding 64KB are written to `~/.loki/artifacts/<session_id>/<hash>.txt` and referenced by `output_ref` in SQLite.

#### Milestone 1.8: Minimal Terminal CLI Thin Client (`loki`)

- [ ] Implement thin CLI client:
  - Command flags: `loki [prompt]`, `loki --new`, `loki --session <id>`.
  - Connect to `agentd` IPC, authenticate, request or resume session.
  - Live stream rendering of markdown chunks to stdout.
  - Visual indicator for active tools with elapsed runtime.
  - Handle `Ctrl+C`: Send cancellation signal to session without terminating the daemon.

---

### Phase 2: Multi-Client Multiplexing, MCP & Lua Extensibility

**Objective:** Evolve the runtime to support multiple simultaneous thin clients connecting to the same daemon/sessions, detached execution and re-attachment, shared MCP servers, tool danger classification permissions, and an embedded Lua plugin engine.

#### Milestone 2.1: Multi-Client Session Multiplexing & Detached Execution

- [ ] Implement client registry in `agentd`:
  - Track active client connections, IDs, and subscribed sessions.
  - Multiple clients can subscribe to the exact same `session_id`.
  - Broadcast session deltas to all subscribed clients concurrently.
- [ ] Detached lifecycle:
  - When all clients disconnect from a running session, the session continues uninterrupted in the daemon.
- [ ] In-memory ring buffer & session rehydration:
  - Maintain ring buffer of last 100 events per session in memory.
  - Re-attachment protocol: Client sends `session.attach`; daemon responds with catch-up state snapshot from SQLite + recent event ring buffer, then transitions to live stream.
- [ ] CLI Session Commands:
  - `loki sessions`: List active and historical sessions, status, and directory.
  - `loki attach <id>`: Attach to a running or completed session.
  - `loki kill <id>`: Abort and cancel a running session.
  - `loki logs <id>`: Print execution transcript and tool events.
  - `loki compact [--focus=<area>]`: Trigger context compaction on active session.
  - `loki status`: Display daemon uptime, active sessions, and memory consumption.

#### Milestone 2.2: Shared MCP (Model Context Protocol) Manager

- [ ] Implement central MCP Manager (`internal/mcp`):
  - Read user MCP configuration (`~/.config/loki/mcp.json`).
  - Manage MCP server lifecycles over standard I/O (stdio) and Server-Sent Events (SSE).
  - Single server instance shared across multiple agent sessions (e.g., one GitHub MCP server instance serving Terminal 1 and Neovim).
  - Dynamic tool discovery: Query MCP servers for available tools and register them into the agent tool catalog.
  - Resilient supervision: Automatic restart on server crash with exponential backoff.

#### Milestone 2.3: Centralized Resource Pooling & Memory Management

- [ ] Shared LLM Connection Pooling (`internal/llm`):
  - Shared HTTP transport with connection pooling and keep-alive optimization.
- [ ] Session Memory Eviction:
  - Inactive session eviction: Sessions idle for >30 minutes have message arrays evicted from heap; rehydrated on demand from SQLite when a client attaches.
  - Memory bounds verification ensuring idle daemon stays strictly under 20MB RSS.

#### Milestone 2.4: Tool Danger Classification & Permission Handshake

- [ ] Implement danger classification engine (`internal/permission`):
  - Safe (read-only): `ReadTool`, `GlobTool`, `GrepTool`, `ListTool` -> auto-approved.
  - Workspace-modify: `EditTool`, `WriteTool` -> diff preview generated -> prompt client.
  - Shell execution: `BashTool` categorized via command heuristics (e.g. `go test`, `npm test` auto-approved; `rm`, `git reset`, `npm publish` require prompt).
  - Policy modes: `strict` (always prompt), `workspace` (auto-allow safe commands in project), `trusted` (auto-allow all).
- [ ] Interactive Permission Handshake Protocol:
  - Daemon transitions session to `AwaitingPermission` and emits `permission.requested` event with command and justification.
  - Attached clients render interactive approval prompt (`[Allow once] [Allow for session] [Deny]`).
  - Client sends `permission.resolved` event back.
  - Timeout fallback: Auto-deny or pause if no client is attached.

#### Milestone 2.5: Capability-Based Lua Plugin Engine

- [ ] Embed pure-Go Lua VM (`github.com/yuin/gopher-lua`). Zero CGO, no WASM overhead.
- [ ] Establish strict capability isolation: Lua code has **zero** direct access to Go memory or OS primitives.
- [ ] Implement Lua Capability API bindings (`plugins/lua`):
  - `loki.events.on(pattern, callback)`: Listen to daemon events.
  - `loki.ui.notify(message, level)`: Send notifications to attached clients.
  - `loki.tools.register(spec, callback)`: Register custom user tools in Lua.
  - `loki.bash.execute(command)`: Capability-checked shell execution.
- [ ] Plugin discovery and loading:
  - Search paths: `~/.config/loki/plugins/*.lua` and `<workspace>/.loki/plugins/*.lua`.
  - Execution step limits (`SetExecutionLimit`) to prevent infinite loops.

---

### Phase 3: IDE Integrations & Rich Terminal UI

**Objective:** Deliver first-class interactive user interfaces across terminal and code editors: a feature-rich Bubble Tea terminal UI, a native Neovim plugin, and a dedicated VS Code extension, all attaching to the same `agentd` backend via local IPC.

#### Milestone 3.1: Rich Terminal User Interface (TUI)

- [ ] Build high-performance TUI client using `bubbletea`, `lipgloss`, and `bubbles`:
  - **Split-View Workspace:** Conversation viewport, live tool execution panel, file diff viewer.
  - **Virtual Scrolling:** Viewport rendering handling 50,000+ line outputs without UI lag.
  - **Interactive Diffs:** Syntax-highlighted patch and diff preview before applying `EditTool` modifications.
  - **Command Palette (`Ctrl+P` / `/`):** Quick session switching, tool inspection, model selection.
  - **Interactive Permission Dialogs:** Interactive modal for permission prompts and multi-choice selections.

#### Milestone 3.2: Neovim Integration Plugin (`loki.nvim`)

- [ ] Create native Neovim plugin written in Lua:
  - Direct IPC connection to `agentd` via Unix socket / Windows named pipe.
  - Session attachment inside Neovim split buffers.
  - Inline code actions: Send visual selection to Loki as context.
  - Buffer diff review: Apply agent-generated code changes directly into active Neovim buffers with undo history.
  - Floating status window and background task indicator in statusline.

#### Milestone 3.3: VS Code Extension (`loki-vscode`)

- [ ] Create VS Code extension (TypeScript):
  - Connects to `agentd` IPC socket.
  - Primary Sidebar Webview for agent chat, session list, and tool streaming.
  - Multi-file diff integration using VS Code's native diff editor.
  - Editor context providers: Send active file, cursor position, diagnostics, and workspace symbols.
  - Status bar item showing daemon state, active sessions, and quick attach menu.

---

### Phase 4: Multi-Agent Orchestration & Web/Remote Interfaces

**Objective:** Coordinate parallel specialized agents under the daemon, implement workspace conflict prevention, and provide an embedded local web dashboard with secure remote access.

#### Milestone 4.1: Multi-Agent Orchestration Engine

- [ ] Multi-Agent Coordination Protocol within `agentd`:
  - Role-based subagents:
    - **Planner:** Breaks requirements into dependency graphs and milestones.
    - **Coder:** Implements file edits and structural modifications.
    - **Test Runner:** Executes test suites, analyzes failures, and reports back.
    - **Reviewer:** Validates diffs against security and code quality rules.
  - Subagent lifecycle supervision: Daemon spawns, tracks, cancels, and aggregates subagents.
  - Workspace file reservation: Prevent parallel agents from creating colliding edits on the same file.
  - Inter-agent messaging bus for delegation, handoff, and consensus.

#### Milestone 4.2: Advanced Multi-Turn Context Optimization

- [ ] Sliding-window summarization refinements:
  - Subagent context isolation: Subagents run in private, lightweight context windows; only their final artifacts/summaries are merged into the parent session.
  - Pruning redundant tool invocation traces from older conversation turns while keeping error and resolution checkpoints.

#### Milestone 4.3: Local Web Dashboard & Remote Gateway

- [ ] Embedded Web Server inside `agentd`:
  - Lightweight embedded HTTP/WebSocket server.
  - Modern web dashboard (React/Svelte compiled into static Go assets with `embed`).
  - View all daemon sessions, active tool executions, system metrics, and memory usage.
- [ ] Secure Remote Gateway:
  - Optional TLS/mTLS and bearer token authentication for remote connections.
  - Enable attaching to local `agentd` from mobile devices, remote terminals, or cloud workstations over Tailscale or SSH tunnels.

---

### Phase 5: Production Hardening, Ecosystem & Scaling

**Objective:** Optimize resource consumption to achieve industry-leading performance, stabilize public APIs, and establish cross-platform distribution channels.

#### Milestone 5.1: Performance Profiling & Optimization

- [ ] Target verification:
  - Daemon idle footprint: **< 20MB RSS**.
  - Daemon active footprint (1 session): **< 50MB RSS**.
  - Thin CLI client idle footprint: **< 10MB RSS**.
  - IPC round-trip latency: **< 1 millisecond**.
- [ ] Verification of spawn-and-die model: Confirm search tools (`GrepTool`, `GlobTool`) leave 0 residual heap bytes upon termination.
- [ ] Profile goroutine allocations using `net/http/pprof` and automated 24-hour soak benchmark tests.

#### Milestone 5.2: Compact Wire Protocol Evaluation (Optional)

- [ ] Benchmark framed JSON vs. Protocol Buffers / FlatBuffers:
  - If JSON marshaling overhead becomes a bottleneck under high event streaming volume, introduce Protobuf v2 wire protocol with handshake version negotiation.

#### Milestone 5.3: OS Service Integration & Distribution

- [ ] Service installation commands:
  - `loki service install` / `uninstall`:
    - Linux: `systemd` user service (`~/.config/systemd/user/agentd.service`).
    - macOS: `launchd` user agent (`~/Library/LaunchAgents/com.loki.agentd.plist`).
    - Windows: Windows Background Service or Startup Task.
- [ ] Cross-platform release packaging via GoReleaser:
  - Binaries for Linux (`amd64`, `arm64`), macOS (`darwin/amd64`, `darwin/arm64`), Windows (`windows/amd64`, `windows/arm64`).
  - Package registries: Homebrew tap, Scoop bucket, Arch AUR, Debian `.deb`, RPM `.rpm`.

---

## 4. Architecture & Package Alignment Matrix

| Package Path           | Primary Subsystem      | Corresponding Milestone | Key Responsibilities                                                              |
| :--------------------- | :--------------------- | :---------------------- | :-------------------------------------------------------------------------------- |
| `cmd/agentd`           | Daemon Entrypoint      | 0.1, 1.1                | Process bootstrap, CLI flags, signal handling, graceful shutdown                  |
| `cmd/loki`             | Thin CLI Client        | 0.1, 1.8, 3.1           | User input parsing, auto-spawn supervisor, terminal streaming, TUI                |
| `internal/daemon`      | Lifecycle Supervision  | 1.1, 5.3                | PID management, OS service integration, client connection listener                |
| `internal/agent`       | ReAct Execution Loop   | 1.3, 4.1                | Turn orchestration, tool dispatch, model streaming, cancellation                  |
| `internal/session`     | Session Engine         | 1.3, 2.1, 4.1           | Session state machine, goroutine supervision, multi-agent coordination            |
| `internal/context`     | Context Manager        | 1.4, 1.6, 4.2           | Token budget tracking, auto-compaction, Agents.md discovery & injection           |
| `internal/llm`         | Model Client           | 1.4, 2.3                | Provider adapters (Claude Messages API), ephemeral prompt caching                 |
| `internal/tools`       | Tool Execution         | 1.5, 2.2                | Tool catalog, built-ins (`Read`, `Glob`, `Grep`, `List`, `Edit`, `Write`, `Bash`) |
| `internal/truncation`  | Output Truncation Gate | 1.6                     | Line/byte caps (500 lines / 40KB) on tool outputs prior to context entry          |
| `internal/permission`  | Permission Gate        | 2.4                     | Danger classification, command heuristics, interactive approval handshake         |
| `internal/process`     | Subprocess Manager     | 1.5                     | Spawn-and-die process execution, process group killing, output capture            |
| `internal/mcp`         | MCP Manager            | 2.2                     | MCP client lifecycle (stdio/SSE), tool registration, connection sharing           |
| `internal/events`      | Event Broker           | 1.2, 2.1                | Fan-out pub/sub broker, wildcard topic filtering, subscriber buffers              |
| `internal/ipc`         | Local Transport        | 0.2                     | Unix domain socket & Windows named pipe implementations                           |
| `internal/protocol`    | Wire Protocol          | 0.2, 5.2                | Length-prefixed framed JSON encoding/decoding, version negotiation                |
| `internal/persistence` | SQLite Storage         | 1.7                     | Schema migrations, session/message records, artifact disk manager                 |
| `internal/config`      | Configuration          | 0.1, 1.6, 2.4           | Config file loader, permission rules, Agents.md search order                      |
| `plugins/api`          | Plugin Interface       | 2.5                     | Capability API contracts for guest Lua plugins                                    |
| `plugins/lua`          | Lua Runtime            | 2.5                     | Embedded `gopher-lua` VM, capability export bindings, hook runner                 |
| `pkg/client`           | Public Client SDK      | 2.1, 3.2, 3.3           | Go client library for third-party tools, IDEs, and external integrations          |

---

## 5. Technical Risk Analysis & Mitigation Matrix

| Risk Category                        | Potential Issue                                                                                 | Impact | Mitigation Strategy                                                                                                                                               |
| :----------------------------------- | :---------------------------------------------------------------------------------------------- | :----- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Output Context Flooding**          | Large command outputs (e.g. `go test ./...` with 50k lines) quickly exhaust LLM context window. | High   | Enforce mandatory Truncation Gate at ingestion: cap outputs at 500 lines / 40KB, preserving the first 200 and last 100 lines.                                     |
| **Edit Hallucination / Alignment**   | LLMs struggle to produce valid unified diffs or correctly apply patches on large files.         | High   | Use `EditTool` with exact string replacement (`old_string` -> `new_string`) and uniqueness validation (fail on 0 or >1 matches).                                  |
| **Event Replay / Re-attachment Gap** | A client attaching mid-stream might miss critical state or receive duplicate deltas.            | High   | Combine SQLite durable history with an in-memory ring buffer (last 100 events) in `agentd`. Send catch-up snapshot followed by live event cutover.                |
| **Orphaned Subprocesses**            | Long-running shell commands or MCP servers leak if the daemon or client crashes.                | Medium | Use OS process group isolation (`Setpgid` on Unix, Job Objects on Windows) to terminate all descendant processes on timeout or cancellation.                      |
| **SQLite Concurrency Contention**    | Multiple parallel agents and clients writing simultaneously could cause `SQLITE_BUSY`.          | Medium | Configure WAL mode (`PRAGMA journal_mode=WAL`), set busy timeouts to 5000ms, and dedicate a single serialized writer queue in Go.                                 |
| **Lua Execution Starvation**         | Buggy user Lua plugins running infinite loops or consuming excessive memory.                    | Low    | Configure `SetExecutionLimit` step trap in `gopher-lua` to preempt infinite loops and enforce memory bounds per VM instance.                                      |
| **Daemon Resource Creep**            | Over time, long-running daemons accumulate excessive memory from historical sessions.           | High   | Evict inactive session message arrays from heap after 30 minutes of idle time. Reload on-demand from SQLite when attached. Transient tools leave 0 bytes on heap. |

---

## 6. Definition of Done (DoD) & Quality Gates

Every milestone must satisfy the following criteria prior to merging and release:

1. **Unit & Package Testing:** Minimum 80% line test coverage on core packages (`internal/ipc`, `internal/protocol`, `internal/session`, `internal/permission`, `internal/events`, `internal/truncation`).
2. **Deterministic Concurrency:** All multithreaded and channel-based code must pass tests with the Go race detector enabled (`go test -race ./...`).
3. **Cross-Platform Verification:** Automated green CI runs on Ubuntu (x86_64), macOS (ARM64 & x86_64), and Windows (x86_64).
4. **Detachment & Rehydration Integrity:** Integration test demonstrating client disconnect during an active task, followed by successful re-attachment and output verification.
5. **Memory Footprint Budget:** Daemon idle RSS must remain strictly **< 20MB** verified across 24-hour soak tests.
6. **Zero Search Tool Leakage:** Automated verification ensuring `GrepTool` and `GlobTool` executions return all memory to the OS immediately after completion.

---

## 7. Versioning & Milestone Timeline

| Version    | Codename    | Target Milestones       | Primary Deliverable                                         | Exit Criteria                                                           |
| :--------- | :---------- | :---------------------- | :---------------------------------------------------------- | :---------------------------------------------------------------------- |
| **v0.1.0** | _Spine_     | Phase 0 (0.1, 0.2)      | Go skeleton, IPC transport layer, framed protocol           | Cross-platform IPC benchmark passes 10k messages without error          |
| **v0.2.0** | _Spark_     | Phase 1 (1.1 – 1.5)     | Single-session daemon, prompt caching, lazy retrieval tools | `agentd` streams Claude responses, runs ripgrep search & exact edits    |
| **v0.3.0** | _Anchor_    | Phase 1 (1.6 – 1.8)     | Truncation gate, context compaction, AGENTS.md, SQLite, CLI | Full conversation survives client restart; auto-compaction functions    |
| **v0.4.0** | _Multiplex_ | Phase 2 (2.1, 2.3)      | Multi-client attachment, detached execution, idle eviction  | Two terminal clients stream same session simultaneously; idle RAM <20MB |
| **v0.5.0** | _Sentry_    | Phase 2 (2.2, 2.4)      | Shared MCP manager, permission handshake protocol           | Single MCP server shared; risky shell/edit tools prompt for approval    |
| **v0.6.0** | _Script_    | Phase 2 (2.5)           | Capability-based embedded Lua plugin engine                 | Lua plugin intercepts events and registers custom tool                  |
| **v0.7.0** | _Canvas_    | Phase 3 (3.1)           | Rich interactive Bubble Tea terminal UI (TUI)               | Split-view TUI with virtual scroll, diffs, and interactive modals       |
| **v0.8.0** | _Bridge_    | Phase 3 (3.2, 3.3)      | First-party Neovim and VS Code plugins                      | Neovim & VS Code attach to `agentd` and apply buffer diffs via IPC      |
| **v0.9.0** | _Legion_    | Phase 4 (4.1, 4.2)      | Multi-agent coordination, workspace reservation             | Parallel coder and tester agents collaborate on task                    |
| **v1.0.0** | _Omni_      | Phase 4 (4.3) + Phase 5 | Web UI, production hardening, OS packaging                  | Production release; idle daemon <20MB; full platform packaging          |

---

## 8. Development Workflow & Contribution

- **Branching Strategy:** Feature branches branched from `master` (`feat/<feature-name>`, `fix/<bug-name>`).
- **Code Standards:** Strictly formatted with `gofmt`, checked with `golangci-lint` (including `govet`, `staticcheck`, `errcheck`, and `gosec`).
- **Documentation Updates:** Any architectural changes must be reflected in `docs/DESIGN.md` and tracked in `docs/roadmap.md`.
