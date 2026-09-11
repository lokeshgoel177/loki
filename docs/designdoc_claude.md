# Loki — Persistent Local AI Agent Runtime (v2 Design)

> **The AI agent is a local service, not a terminal process.**
> **Code is fetched on demand, not cached in advance.**
> **Build the agent once. Render it everywhere.**

---

## 1. Executive Summary

Loki is a lightweight, persistent local AI coding-agent platform. A single Go daemon (`agentd`) owns all agent execution, sessions, tool dispatch, LLM communication, and state. Terminals, editors, and future UIs are thin ephemeral clients that attach, detach, and reconnect via local IPC without disrupting running tasks.

This design document supersedes the original by adopting a **zero-index, lazy-fetch, minimal-cache** philosophy inspired by Claude Code's architecture, while preserving Loki's core daemon-based multi-client advantage that Claude Code fundamentally lacks.

### What changed from v1

| Dimension           | v1 Design                                   | v2 Design (this document)                                                           |
| :------------------ | :------------------------------------------ | :---------------------------------------------------------------------------------- |
| Code discovery      | Filesystem watchers + repository indexes    | **Lazy fetch:** `ripgrep`, `glob`, line-sliced `read` on demand                     |
| Caching             | Persistent caches, indexes, semantic search | **Context window is the cache.** No vector DBs, no AST graphs                       |
| Tool footprint      | Persistent tool processes                   | **Spawn-and-die:** transient subprocesses that exit immediately                     |
| File editing        | Unspecified                                 | **Exact string replacement** with uniqueness checks                                 |
| Context management  | Unspecified compaction                      | **Auto-compaction** with LLM summarization at token budget thresholds               |
| Project orientation | None                                        | **`AGENTS.md` / `Agents.md`** — global standard project cheat-sheet loaded into context |
| Output handling     | Full tool outputs in context                | **Aggressive truncation** at ingestion — cap lines/bytes before entering context    |
| Permission model    | Capability-based (plugin-focused)           | **Tool danger classification** — read-only auto-approve, modifying requires consent |
| Plugin runtime      | Lua phase 1 → WASM phase 2                  | **Lua only** — WASM deferred indefinitely                                           |
| Prompt caching      | Not addressed                               | **Ephemeral prompt cache breakpoints** on system prompt + tool schemas              |
| Idle RAM target     | <30 MB                                      | **<20 MB** — achievable without indexes/watchers/vector stores                      |

---

## 2. Problem

Most AI coding assistants are architectured in one of two wasteful patterns:

### Pattern A: Heavy IDE agents (Cursor, Copilot, Cody)

```text
IDE Process
   ├── Electron/Chromium runtime         (500+ MB)
   ├── Language Server Protocol daemons   (200+ MB)
   ├── Vector embedding database          (100+ MB)
   ├── AST index / symbol graph           (50+ MB)
   ├── Background filesystem watchers     (continuous CPU)
   └── Per-tab agent instances            (duplicated state)
```

RAM: **1.5–5 GB**. CPU: **2–10%** idle. Startup: **10–30 seconds**.

### Pattern B: Lean CLI agents (Claude Code)

```text
Terminal Process
   ├── Node.js runtime                    (~50 MB)
   ├── Lazy ripgrep/glob tools            (spawn & die)
   ├── Session JSON in memory             (~10–50 MB)
   └── Anthropic API streaming            (zero local compute)
```

RAM: **50–150 MB**. CPU: **~0%** idle. Startup: **<1 second**.

But Claude Code has critical limitations:

- **Single terminal process** — closing the terminal kills the agent.
- **No multi-client** — can't view the same session from Neovim and terminal simultaneously.
- **No detached execution** — can't start a refactor and walk away.
- **No resource sharing** — each `claude` invocation is a separate Node process with separate connections.

### Loki's thesis

Combine Claude Code's **zero-index, lazy-fetch, minimal-RAM** efficiency with a **persistent daemon** that supports multiple simultaneous clients and detached execution.

```text
Claude Code's efficiency  +  Daemon persistence  =  Loki
      (low RAM)               (multi-client)
```

---

## 3. Goals

### 3.1 Extremely low resource footprint

| Metric                                | Target       |
| :------------------------------------ | :----------- |
| Daemon idle RSS                       | **< 20 MB**  |
| Daemon active RSS (1 session)         | **< 50 MB**  |
| Thin CLI client idle RSS              | **< 10 MB**  |
| CPU idle                              | **0%**       |
| Startup time (daemon already running) | **< 100 ms** |
| Startup time (cold daemon launch)     | **< 500 ms** |

Achieved by:

- No persistent filesystem watchers.
- No code indexes or vector databases.
- No AST graphs or symbol tables.
- No resident tool processes.
- Spawn-and-die subprocesses for search tools.
- Aggressive eviction of inactive session data from heap.
- On-demand SQLite loading for historical sessions.

### 3.2 Persistent daemon, ephemeral clients

One `agentd` process per user. Clients connect and disconnect freely.

```text
agentd (persistent, ~20 MB idle)
  ├── Terminal 1      (attach / detach)
  ├── Terminal 2      (attach / detach)
  ├── Neovim          (attach / detach)
  ├── VS Code         (attach / detach)
  └── Web UI          (attach / detach)
```

### 3.3 Lazy code fetching (zero pre-indexing)

Code is never pre-scanned, pre-indexed, or cached in memory. The LLM navigates the codebase the way a human developer does in a terminal:

```text
[User prompt]
       │
       ▼
[LLM forms hypothesis]
       │
       ▼
[GlobTool: find files matching pattern]  ──► transient subprocess, exits immediately
       │
       ▼
[GrepTool: ripgrep for symbols/patterns] ──► transient subprocess, exits immediately
       │
       ▼
[ReadTool: inspect specific line ranges]  ──► direct file I/O, capped output
       │
       ▼
[LLM synthesizes & acts]
```

### 3.4 Shared resources across all clients

Instead of N processes each with their own connections:

```text
Terminal 1 ─┐
Terminal 2 ─┼──→ agentd ──→ single LLM connection pool
Neovim     ─┤              single MCP manager
VS Code    ─┘              single process supervisor
```

### 3.5 Detached execution

Sessions survive client disconnection.

```text
User: "Refactor this module and run the tests."
  │
  ├── agentd accepts task
  ├── User closes terminal
  ├── agentd continues: editing, testing, iterating
  │
  └── User re-attaches from any client → sees results
```

### 3.6 Extensibility via Lua plugins

Capability-gated Lua scripting for hooks, commands, automation. No WASM.

---

## 4. Non-Goals

- Pre-indexing, vector embeddings, AST parsing, or semantic search for code retrieval.
- Running a local LLM (Ollama etc.) — Loki is an API-first agent runtime.
- WASM plugin runtime (deferred indefinitely).
- Cloud/multi-user deployment.
- Replacing full IDEs.
- Building a browser UI before daemon/client protocol stabilizes.

---

## 5. High-Level Architecture

```text
                        ┌─────────────────────────────────────────┐
                        │               agentd                     │
                        │                                          │
                        │  ┌─────────────────────────────────────┐ │
                        │  │         Agent Engine                 │ │
                        │  │                                     │ │
                        │  │  ReAct Loop ◄──► LLM Streaming     │ │
                        │  │       │                              │ │
                        │  │       ▼                              │ │
                        │  │  Tool Dispatcher                    │ │
                        │  │  ├── ReadTool   (fs.ReadFile)       │ │
                        │  │  ├── GlobTool   (spawn: glob)       │ │
                        │  │  ├── GrepTool   (spawn: ripgrep)    │ │
                        │  │  ├── EditTool   (string replace)    │ │
                        │  │  ├── WriteTool  (atomic write)      │ │
                        │  │  ├── BashTool   (spawn: shell)      │ │
                        │  │  ├── ListTool   (fs.ReadDir)        │ │
                        │  │  └── MCP Tools  (MCP client)        │ │
                        │  └─────────────────────────────────────┘ │
                        │                                          │
                        │  Session Manager     (goroutine per)     │
                        │  Context Manager     (token budget)      │
                        │  Event Broker        (pub/sub)           │
                        │  Permission Gate     (danger levels)     │
                        │  MCP Manager         (shared lifecycle)  │
                        │  Process Supervisor  (spawn & reap)      │
                        │  Persistence         (SQLite WAL)        │
                        │  Plugin Engine       (Lua VM)            │
                        │  Agents.md Loader    (project context)   │
                        │                                          │
                        └────────────────┬────────────────────────┘
                                         │
                                    Local IPC
                          (Unix socket / Windows named pipe)
                                         │
                  ┌──────────────────────┼──────────────────────┐
                  │                      │                      │
                  ▼                      ▼                      ▼
             Terminal               Neovim                  VS Code
              client                client                   client
           (< 10 MB)             (Lua plugin)            (TS extension)
```

---

## 6. The Lazy Fetch Model (Core Innovation)

This is the most critical architectural departure from the v1 design. It eliminates the single largest source of RAM, CPU, and startup overhead in AI coding tools.

### 6.1 Why no indexing

| Pre-Indexed Approach                               | Lazy Fetch Approach                                 |
| :------------------------------------------------- | :-------------------------------------------------- |
| Startup: scan repo, chunk files, embed vectors     | Startup: **instant** (nothing to scan)              |
| RAM: vector DB + symbol graph (~200–500 MB)        | RAM: **0 bytes** (no persistent state)              |
| CPU: continuous reindexing on file changes         | CPU: **0%** (no watchers)                           |
| Staleness: index drifts from actual files          | Staleness: **impossible** (always reads live files) |
| Complexity: embedding model, vector store, ranking | Complexity: **trivial** (shell out to ripgrep)      |

The LLM's 200k token context window serves as the "working memory." Files are read into context when needed and evicted via compaction when no longer relevant.

### 6.2 Core retrieval tools

All retrieval tools are **stateless** — they take inputs, produce outputs, and hold no persistent state. Search tools are **transient subprocesses** that spawn, execute in milliseconds, and exit.

#### `ReadTool`

Read a file or a line range. Output is capped.

```text
Input:  { path: "src/server.go", start_line: 100, end_line: 200 }
Output: { content: "...", total_lines: 450, truncated: false }

Constraints:
  - Max 500 lines per read (configurable)
  - Max 40 KB per read
  - If exceeded: return truncated output + instruction to use narrower range
```

Implementation: Direct `os.ReadFile` + line slicing in Go. No subprocess needed.

#### `GlobTool`

Find files by pattern.

```text
Input:  { pattern: "**/*.go", root: "/project" }
Output: { matches: ["src/main.go", "src/server.go", ...], count: 42, capped: false }

Constraints:
  - Max 100 results (configurable)
  - If exceeded: return first 100 + warning to narrow pattern
```

Implementation: Go `filepath.WalkDir` with glob matching. No subprocess needed.

#### `GrepTool`

Search file contents via regex.

```text
Input:  { pattern: "func.*Handler", path: "/project/src", include: ["*.go"] }
Output: { matches: [{ file: "server.go", line: 42, content: "func UserHandler(..." }], count: 15 }

Constraints:
  - Max 50 matches (configurable)
  - Each match: file path + line number + line content (trimmed to 200 chars)
```

Implementation: Spawn `ripgrep` (`rg --json`) as transient subprocess. Parse JSON output. Subprocess exits immediately after search completes. Memory returned to OS.

> **Why ripgrep as a subprocess?**
> `rg` searches millions of lines in milliseconds, handles `.gitignore`, and is available on every platform. Spawning it costs ~2–5 ms. Keeping a resident search daemon would waste RAM for zero benefit.

#### `ListTool`

List directory contents.

```text
Input:  { path: "/project/src", max_depth: 2 }
Output: { entries: [{ name: "main.go", type: "file", size: 1234 }, ...] }
```

Implementation: `os.ReadDir` in Go. No subprocess.

#### `BashTool`

Execute shell commands.

```text
Input:  { command: "go test ./...", timeout_seconds: 120, working_dir: "/project" }
Output: { stdout: "...", stderr: "...", exit_code: 0, truncated: true }

Constraints:
  - stdout/stderr capped at 10,000 lines or 100 KB
  - If exceeded: keep first 200 + last 200 lines, insert "[truncated: N lines omitted]"
  - Configurable timeout with process group kill on expiry
```

Implementation: `os/exec.CommandContext` with process group isolation (`Setpgid` on Unix, Job Objects on Windows).

#### `EditTool` (Exact String Replacement)

This is a deliberate design choice. LLMs are unreliable at generating correct unified diffs. Exact string replacement is far more robust.

```text
Input:  { path: "src/server.go", old_string: "func oldHandler(", new_string: "func newHandler(" }

Procedure:
  1. Read file content
  2. Count occurrences of old_string
  3. If 0 occurrences → error: "Target string not found in file"
  4. If >1 occurrences → error: "Ambiguous: N matches found. Provide more surrounding context"
  5. If exactly 1 → replace, write atomically (write to .tmp, rename)
```

Implementation: Pure Go string operations + atomic file write.

#### `WriteTool`

Create new files or overwrite existing files entirely.

```text
Input:  { path: "src/new_file.go", content: "package main\n...", create_dirs: true }
```

Implementation: `os.MkdirAll` + atomic write.

### 6.3 Tool output truncation pipeline

**Every tool output passes through a truncation gate before entering the LLM context.** This is critical to preventing context window exhaustion.

```text
[Raw Tool Output]
       │
       ▼
┌─────────────────────────┐
│   Truncation Gate       │
│                         │
│   Rules:                │
│   - Max lines: 500      │
│   - Max bytes: 40 KB    │
│   - If exceeded:        │
│     keep first 200      │
│     + "[truncated]"     │
│     + last 100 lines    │
└─────────────┬───────────┘
              │
              ▼
[Truncated Tool Result → LLM Context]
```

This means a `go test ./...` that produces 50,000 lines of output only consumes ~300 lines of context, not 50,000.

---

## 7. Context Management

### 7.1 Context is the cache

The LLM's context window (typically 200k tokens) serves as the only "cache" in the system. There is no separate caching layer for code, symbols, or search results.

```text
┌─────────────────────────────────────────────────┐
│              Context Window (~200k tokens)        │
│                                                   │
│  ┌──────────────────────────────────────────────┐ │
│  │  STATIC PREFIX (cached by API provider)      │ │
│  │                                              │ │
│  │  System Prompt              (~2k tokens)     │ │
│  │  Tool Schemas               (~3k tokens)     │ │
│  │  AGENTS.md (project context)(~1-5k tokens)   │ │
│  │                                              │ │
│  │  cache_control: ephemeral ◄── breakpoint     │ │
│  └──────────────────────────────────────────────┘ │
│                                                   │
│  ┌──────────────────────────────────────────────┐ │
│  │  DYNAMIC TURNS                               │ │
│  │                                              │ │
│  │  User messages                               │ │
│  │  Assistant responses                         │ │
│  │  tool_use blocks                             │ │
│  │  tool_result blocks (truncated)              │ │
│  └──────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

### 7.2 Prompt caching breakpoints

The static prefix (system prompt + tool schemas + `AGENTS.md`) is identical across every turn within a session. By placing an ephemeral cache breakpoint after this block, the LLM API provider caches it server-side.

Benefits:
- **90% cost reduction** on cached prefix tokens (Anthropic pricing).
- **85%+ latency reduction** on cache-hit prefill (Anthropic benchmarks).
- Zero local storage cost.

Implementation in the LLM adapter:

```go
messages := []Message{
    {
        Role: "system",
        Content: systemPrompt + toolSchemas + agentsMD,
        CacheControl: &CacheControl{Type: "ephemeral"},  // ← breakpoint
    },
    // ... dynamic conversation turns follow
}
```

### 7.3 Auto-compaction

When total conversation tokens approach a configurable high-water mark (default: 75% of context window capacity), the context manager triggers automatic compaction.

```text
[Token counter hits 150k / 200k threshold]
       │
       ▼
[Pause agent loop]
       │
       ▼
[Send compaction prompt to LLM]
  "Summarize the current session state:
   - Original user objective
   - Files examined, edited, created
   - Key architectural findings
   - Errors encountered and resolutions
   - Test results
   - Immediate next steps"
       │
       ▼
[LLM returns structured summary (~2-5k tokens)]
       │
       ▼
[Replace conversation history with:]
  [System Prompt (cached)]
  [Compaction Summary]
  [Last 2-3 active turns]
       │
       ▼
[Resume agent loop at ~20k tokens]
```

This allows sessions to run indefinitely without hitting context limits.

### 7.4 Manual compaction

Users can trigger compaction via a `/compact` command through any attached client. Optionally with focus instructions:

```text
/compact focus=testing
```

This tells the compaction prompt to preserve testing-related context in detail while aggressively summarizing other areas.

---

## 8. AGENTS.md / Agents.md — Global Standard Project Orientation File

Adheres to the global `AGENTS.md` standard (also compatible with Claude Code's `CLAUDE.md`). This file eliminates blind codebase scanning at session start by giving the agent immediate project conventions, commands, and architecture guidelines.

### Search order (first found wins per level, all levels merged; supports `AGENTS.md`, `Agents.md`, and `agents.md`):

```text
1. ~/.config/loki/AGENTS.md        (global user preferences)
2. <repo-root>/AGENTS.md           (project-wide standard)
3. <repo-root>/.loki/AGENTS.md     (project-wide, hidden)
4. <cwd>/AGENTS.md                 (subdirectory overrides)
```

### Recommended contents:

```markdown
# Project: payments-service

## Build & Test

- Build: `go build ./...`
- Test: `go test ./... -race -count=1`
- Lint: `golangci-lint run`

## Architecture

- Clean architecture: handlers → services → repositories
- All database access via `internal/db/` repository interfaces
- HTTP handlers in `internal/api/handlers/`

## Conventions

- Error wrapping: always use `fmt.Errorf("operation: %w", err)`
- Context propagation: every function takes `ctx context.Context` as first param
- Table-driven tests preferred

## Known Issues

- The `legacy/` directory is deprecated, do not modify
- `internal/auth/` is being migrated to OAuth2, see tracking issue #142
```

This file is loaded into the static cached prefix of every session context, giving the LLM immediate project fluency without any file scanning.

---

## 9. The ReAct Agent Loop

The core execution model is a **ReAct loop** (Reasoning + Acting) running as a goroutine per session inside `agentd`.

```text
                    ┌───────────────────┐
                    │   User Prompt     │
                    │   (via any client)│
                    └────────┬──────────┘
                             │
                             ▼
              ┌──────────────────────────────┐
              │      Context Manager          │
              │                               │
              │  Append user message           │
              │  Check token budget            │
              │  Auto-compact if needed        │
              └──────────────┬────────────────┘
                             │
                             ▼
              ┌──────────────────────────────┐
         ┌───►│      LLM Streaming Call       │
         │    │                               │
         │    │  POST /v1/messages             │
         │    │  Stream SSE chunks             │
         │    │  Emit message.delta events     │
         │    └──────────────┬────────────────┘
         │                   │
         │                   ▼
         │    ┌──────────────────────────────┐
         │    │    Response Parser            │
         │    │                               │
         │    │  content_block = text?         │
         │    │    → stream to clients         │
         │    │                               │
         │    │  content_block = tool_use?     │
         │    │    → dispatch to Tool Runner   │
         │    └──────────────┬────────────────┘
         │                   │
         │                   ▼
         │    ┌──────────────────────────────┐
         │    │    Permission Gate            │
         │    │                               │
         │    │  Read-only tool?               │
         │    │    → auto-approve              │
         │    │                               │
         │    │  Modifying tool?               │
         │    │    → emit permission.requested │
         │    │    → wait for client response  │
         │    │    → approved? execute         │
         │    │    → denied? return error      │
         │    └──────────────┬────────────────┘
         │                   │
         │                   ▼
         │    ┌──────────────────────────────┐
         │    │    Tool Executor              │
         │    │                               │
         │    │  Spawn transient subprocess   │
         │    │  or direct Go I/O             │
         │    │  Stream tool.output events    │
         │    │  Collect result               │
         │    └──────────────┬────────────────┘
         │                   │
         │                   ▼
         │    ┌──────────────────────────────┐
         │    │    Truncation Gate            │
         │    │                               │
         │    │  Cap output to budget          │
         │    │  Insert [truncated] markers    │
         │    └──────────────┬────────────────┘
         │                   │
         │                   ▼
         │    ┌──────────────────────────────┐
         │    │  Append tool_result to context│
         │    └──────────────┬────────────────┘
         │                   │
         │         ┌─────────┴─────────┐
         │         │                   │
         │    stop_reason:         stop_reason:
         │    "tool_use"           "end_turn"
         │         │                   │
         └─────────┘                   ▼
                              ┌────────────────┐
                              │  Final Response │
                              │  → stream to    │
                              │    all clients   │
                              └────────────────┘
```

### Key properties:

- **Session-scoped goroutine:** One goroutine per active session. Idle sessions consume zero CPU.
- **Streaming-first:** Every LLM token and tool output line is streamed to subscribed clients in real-time via the event broker.
- **Self-correcting:** If a tool returns an error (file not found, ambiguous edit, non-zero exit code), the error is fed back to the LLM which adjusts its approach.
- **Cancellable:** `context.WithCancel` — any client can send a cancel signal. The session goroutine tears down gracefully, killing any running subprocesses.

---

## 10. Permission & Danger Classification

Tools are classified into danger levels. This replaces the v1 design's plugin-focused capability model with a simpler, tool-focused permission system.

### 10.1 Danger levels

| Level                | Tools                                          | Default Policy                                 |
| :------------------- | :--------------------------------------------- | :--------------------------------------------- |
| **Safe (read-only)** | `ReadTool`, `GlobTool`, `GrepTool`, `ListTool` | Auto-approve always                            |
| **Workspace-modify** | `EditTool`, `WriteTool`                        | Show diff preview → prompt client for approval |
| **Shell execution**  | `BashTool`                                     | Classify by command heuristics                 |
| **MCP tools**        | Dynamic (from MCP servers)                     | Per-tool configuration                         |

### 10.2 Bash command classification

```text
Low-risk (auto-approvable):
  go test, go build, go vet, npm test, pytest, cargo test,
  git status, git log, git diff, ls, cat, head, tail, wc

High-risk (always prompt):
  rm, git reset, git push, git checkout, npm publish,
  curl, wget, chmod, chown, sudo, docker, kill
```

Users configure this via `~/.config/loki/permissions.toml`:

```toml
[permissions]
mode = "workspace"   # "strict" | "workspace" | "trusted"

[permissions.auto_approve]
commands = ["go test", "go build", "npm test", "pytest", "cargo test"]

[permissions.always_deny]
commands = ["rm -rf /", "sudo"]
```

### 10.3 Permission handshake protocol

```text
agentd                          Client
  │                               │
  │  permission.requested         │
  │  {                            │
  │    tool: "BashTool",          │
  │    command: "go test ./...",  │
  │    risk_level: "low",         │
  │    justification: "...",      │
  │    session_id: "abc"          │
  │  }                            │
  │ ─────────────────────────────►│
  │                               │
  │                               │  User sees: [Allow] [Allow for session] [Deny]
  │                               │
  │  permission.resolved          │
  │  {                            │
  │    decision: "allow_session", │
  │    session_id: "abc"          │
  │  }                            │
  │ ◄─────────────────────────────│
  │                               │
  │  (execute tool)               │
```

If no client is attached (detached execution), the daemon uses the configured default policy. If `mode = "strict"`, it pauses and waits for a client to attach.

---

## 11. Session Model

### 11.1 Session lifecycle

```text
         ┌──────────┐
         │  Created  │
         └─────┬─────┘
               │ user sends prompt
               ▼
         ┌──────────┐
    ┌───►│ Running   │◄───────────────────────────────┐
    │    └─────┬─────┘                                │
    │          │                                      │
    │    ┌─────┴──────────────────────────┐           │
    │    │                                │           │
    │    ▼                                ▼           │
    │ ┌─────────────┐           ┌──────────────────┐  │
    │ │ Executing   │           │ AwaitingPermission│  │
    │ │ (LLM+tools) │           │ (blocked on user) │  │
    │ └─────┬───────┘           └────────┬─────────┘  │
    │       │                            │ approved    │
    │       │ tool_use loop              │             │
    │       └────────────────────────────┘             │
    │                                                  │
    │       │ end_turn                                 │
    │       ▼                                          │
    │ ┌──────────┐     user sends new prompt           │
    │ │  Idle    │─────────────────────────────────────┘
    │ └─────┬────┘
    │       │ explicit kill / error
    │       ▼
    │ ┌──────────────┐
    │ │ Terminated   │
    │ └──────────────┘
    │
    │ cancel signal
    └── (from any state)
```

### 11.2 Session state (daemon-owned)

```go
type Session struct {
    ID            string
    WorkingDir    string
    GitBranch     string
    State         SessionState
    Messages      []Message           // conversation history (in memory for active sessions)
    TokenCount    int                 // running total
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

### 11.3 Active vs. inactive sessions

- **Active session:** Messages array in heap. Goroutine alive (running or idle, waiting for input).
- **Inactive session:** Messages evicted from heap. Data lives only in SQLite. Reloaded on demand when a client attaches.
- **Eviction policy:** Sessions idle for >30 minutes are evicted from memory. Configurable.

### 11.4 Session rehydration (client re-attachment)

When a client sends `session.attach`:

```text
1. Is session active in memory?
   YES → subscribe client to event stream, send current state snapshot
   NO  → load session from SQLite into memory, resume goroutine, subscribe client

2. Send catch-up payload:
   {
     state: "idle" | "running",
     messages: [...last N messages...],
     recent_events: [...ring buffer of last 100 events...]
   }

3. Client is now live-streaming events
```

---

## 12. Event-Driven Architecture

### 12.1 Event taxonomy

```text
Session events:
  session.created
  session.state_changed    { old_state, new_state }
  session.finished
  session.failed           { error }
  session.compacted        { old_tokens, new_tokens }

Message events:
  message.delta            { session_id, content_chunk }
  message.completed        { session_id, message_id, role }

Tool events:
  tool.started             { session_id, tool_name, input_summary }
  tool.output              { session_id, line }
  tool.completed           { session_id, tool_name, exit_code, duration_ms }
  tool.error               { session_id, tool_name, error }

Permission events:
  permission.requested     { session_id, tool_name, risk_level, command }
  permission.resolved      { session_id, decision }

Client events:
  client.connected         { client_id }
  client.disconnected      { client_id }
```

### 12.2 Subscription model

Clients subscribe selectively:

```json
{
  "type": "subscribe",
  "session_id": "abc123",
  "events": ["message.*", "tool.*", "permission.*"]
}
```

Or subscribe to all events for a session:

```json
{
  "type": "subscribe",
  "session_id": "abc123"
}
```

### 12.3 Event broker implementation

- In-memory fan-out with buffered Go channels per subscriber.
- Wildcard topic matching (`session.abc.*`).
- Backpressure: if a client's channel buffer is full, events are dropped for that client (slow consumer protection) with a `client.events_dropped` warning.
- No persistent event queue — events are ephemeral. Durable state is in SQLite.

---

## 13. IPC Protocol

### 13.1 Transport

| Platform      | Transport          | Path                                                         |
| :------------ | :----------------- | :----------------------------------------------------------- |
| Linux / macOS | Unix domain socket | `$XDG_RUNTIME_DIR/loki/agentd.sock` or `~/.loki/agentd.sock` |
| Windows       | Named pipe         | `\\.\pipe\loki-agentd-<username>`                            |

Hidden behind a `Transport` interface:

```go
type Transport interface {
    Listen(addr string) (net.Listener, error)
    Dial(addr string) (net.Conn, error)
}
```

### 13.2 Wire format

Length-prefixed framed JSON (simple, debuggable, sufficient for local IPC):

```text
┌──────────────┬─────────────────────────────┐
│ 4 bytes      │ N bytes                      │
│ payload len  │ JSON payload                 │
│ (big-endian) │                              │
└──────────────┴─────────────────────────────┘
```

Envelope:

```json
{
  "version": 1,
  "id": "req-001",
  "type": "session.create",
  "payload": { ... },
  "timestamp": "2025-01-15T10:30:00Z"
}
```

### 13.3 Protocol upgrade path

Start with JSON. If profiling shows serialization overhead matters (unlikely for local IPC), migrate to Protocol Buffers with version negotiation in the handshake.

---

## 14. Client Lifecycle

### 14.1 Cold start (no daemon running)

```text
$ loki "refactor the auth module"
  │
  ├── Dial IPC socket → connection refused
  ├── Fork/exec agentd as detached background process
  ├── Poll socket with exponential backoff (max 3s)
  ├── Handshake + authenticate
  ├── session.create { prompt: "refactor the auth module", working_dir: "/project" }
  ├── subscribe { session_id: "..." }
  └── Stream events → render to terminal
```

### 14.2 Warm start (daemon already running)

```text
$ loki "add unit tests for the handler"
  │
  ├── Dial IPC socket → connected immediately
  ├── Handshake
  ├── session.create { prompt: "...", working_dir: "/project" }
  ├── subscribe
  └── Stream → render
```

### 14.3 Re-attach to existing session

```text
$ loki attach abc123
  │
  ├── Dial → connected
  ├── session.attach { session_id: "abc123" }
  ├── Receive catch-up payload (history + ring buffer)
  ├── Render historical output
  └── Stream live events
```

### 14.4 CLI commands

```text
loki <prompt>                    Start new session with prompt
loki --new                       Start interactive session
loki --session <id>              Resume session
loki attach <id>                 Attach to running/completed session
loki sessions                    List all sessions
loki kill <id>                   Abort a running session
loki logs <id>                   Print session transcript
loki compact [--focus=<area>]    Trigger context compaction
loki status                      Show daemon status, active sessions, memory
```

---

## 15. Persistence

### 15.1 SQLite (CGO-free)

Using `modernc.org/sqlite` for zero-CGO cross-platform binary.

WAL mode for concurrent readers:

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
```

### 15.2 Schema

```sql
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    working_dir TEXT NOT NULL,
    git_branch  TEXT,
    state       TEXT NOT NULL DEFAULT 'idle',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    metadata    TEXT  -- JSON blob
);

CREATE TABLE messages (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,  -- 'user', 'assistant', 'tool_use', 'tool_result'
    content     TEXT NOT NULL,
    token_count INTEGER,
    created_at  TEXT NOT NULL
);

CREATE TABLE tool_calls (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    message_id  TEXT REFERENCES messages(id),
    tool_name   TEXT NOT NULL,
    input_json  TEXT,
    output_text TEXT,           -- NULL if externalized to disk
    output_ref  TEXT,           -- path to artifact file if externalized
    status      TEXT NOT NULL,  -- 'started', 'completed', 'failed', 'cancelled'
    exit_code   INTEGER,
    duration_ms INTEGER,
    created_at  TEXT NOT NULL
);

CREATE TABLE permissions (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    tool_name   TEXT NOT NULL,
    command     TEXT,
    decision    TEXT NOT NULL,  -- 'allow_once', 'allow_session', 'deny'
    created_at  TEXT NOT NULL
);

CREATE TABLE compactions (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    summary     TEXT NOT NULL,
    old_tokens  INTEGER,
    new_tokens  INTEGER,
    created_at  TEXT NOT NULL
);
```

### 15.3 Artifact externalization

Tool outputs exceeding 64 KB are written to disk and referenced from SQLite:

```text
~/.loki/artifacts/<session-id>/<hash>.txt
```

This keeps SQLite fast and the heap lean.

---

## 16. MCP (Model Context Protocol) Manager

### 16.1 Shared MCP lifecycle

A single MCP manager inside `agentd` owns all MCP server connections:

```text
Terminal 1 (session A) ─┐
Terminal 2 (session B) ─┼──→ MCP Manager ──→ GitHub MCP server (single instance)
Neovim     (session A) ─┘                ──→ Jira MCP server   (single instance)
```

### 16.2 Configuration

```json
// ~/.config/loki/mcp.json
{
  "servers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "${GITHUB_TOKEN}" },
      "transport": "stdio"
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/project"],
      "transport": "stdio"
    }
  }
}
```

### 16.3 Resilience

- Auto-restart on MCP server crash (exponential backoff).
- Tool discovery: query each MCP server for its tool manifest, register tools into the agent's catalog.
- If an MCP server is unavailable, its tools are marked as unavailable rather than crashing the session.

---

## 17. Lua Plugin Engine

### 17.1 Architecture

Lua is the only plugin runtime. It runs inside `agentd` via an embedded pure-Go Lua VM (`gopher-lua`).

```text
                 agentd
                   │
          Plugin Manager
                   │
             ┌─────┴─────┐
             │            │
          Lua VM     (future runtimes)
             │
       Lua Plugins
```

### 17.2 Capability API

Plugins interact exclusively through capability bindings. No access to Go internals.

```lua
-- Example: notification on test completion
loki.events.on("tool.completed", function(event)
    if event.tool_name == "BashTool" and string.find(event.input, "test") then
        loki.ui.notify("Tests finished: exit code " .. event.exit_code)
    end
end)

-- Example: register a custom tool
loki.tools.register({
    name = "deploy_staging",
    description = "Deploy current branch to staging environment",
    parameters = {
        { name = "branch", type = "string", required = false }
    }
}, function(input)
    local branch = input.branch or "main"
    local result = loki.bash.execute("deploy.sh --branch=" .. branch)
    return result
end)
```

### 17.3 Plugin discovery

```text
~/.config/loki/plugins/*.lua     (global plugins)
<workspace>/.loki/plugins/*.lua  (project-specific plugins)
```

### 17.4 Safety

- Execution step limits to prevent infinite loops (`SetExecutionLimit`).
- Memory cap per Lua VM instance.
- Plugins declare required capabilities in a manifest header:

```lua
-- @name deploy-helper
-- @version 1.0.0
-- @capabilities bash.execute, ui.notify
```

Only declared capabilities are bound into the Lua environment.

---

## 18. Process Supervision

### 18.1 Spawn-and-die model

Tools spawn subprocesses that are expected to terminate quickly:

```text
GrepTool:  spawn rg → runs 5ms → exits → memory returned to OS
BashTool:  spawn sh → runs 30s → exits → memory returned to OS
```

No tool process is kept resident. This is the primary mechanism for keeping daemon RAM low.

### 18.2 Process group isolation

Every spawned subprocess is placed in its own process group:

```go
cmd := exec.CommandContext(ctx, "bash", "-c", command)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // Unix
// Windows: CREATE_NEW_PROCESS_GROUP via Job Objects
```

On cancellation or timeout, kill the entire process group to prevent orphaned children.

### 18.3 Timeout enforcement

Every tool execution has a configurable timeout (default: 120 seconds for BashTool, 10 seconds for read-only tools). On timeout:

1. Kill the process group.
2. Return a `tool_result` with `status: "timeout"`.
3. The LLM sees the timeout and can adjust (e.g., run with more specific flags, break the task into pieces).

---

## 19. Go Package Structure

```text
loki/
├── cmd/
│   ├── agentd/                 # Daemon entrypoint
│   │   └── main.go
│   └── loki/                   # CLI thin client entrypoint
│       └── main.go
│
├── internal/
│   ├── daemon/                 # Lifecycle, PID, signal handling, client listener
│   ├── agent/                  # ReAct loop, turn orchestration
│   ├── session/                # Session state machine, goroutine management
│   ├── context/                # Token budget, compaction, AGENTS.md loading
│   ├── llm/                    # Provider interface + Anthropic/OpenAI adapters
│   ├── tools/                  # Tool registry, dispatch, built-in tools
│   │   ├── read.go
│   │   ├── glob.go
│   │   ├── grep.go
│   │   ├── list.go
│   │   ├── edit.go
│   │   ├── write.go
│   │   └── bash.go
│   ├── truncation/             # Output truncation gate
│   ├── permission/             # Danger classification, approval handshake
│   ├── mcp/                    # MCP client manager, tool discovery
│   ├── process/                # Subprocess spawn, process group management
│   ├── events/                 # In-memory pub/sub event broker
│   ├── ipc/                    # Transport abstraction (unix socket / named pipe)
│   ├── protocol/               # Wire framing, envelope encoding, versioning
│   ├── persistence/            # SQLite operations, schema migrations
│   └── config/                 # Configuration loading, AGENTS.md discovery
│
├── plugins/
│   ├── api/                    # Capability API contracts
│   └── lua/                    # gopher-lua VM, capability bindings
│
└── pkg/
    └── client/                 # Public client SDK for IDE extensions
```

---

## 20. Memory Budget Breakdown

Target: **< 20 MB idle, < 50 MB active (1 session)**

| Component          | Idle (bytes) | Active (bytes) | Notes                                       |
| :----------------- | :----------- | :------------- | :------------------------------------------ |
| Go runtime         | ~5 MB        | ~5 MB          | Goroutine stacks, GC metadata               |
| Daemon core        | ~2 MB        | ~2 MB          | Config, PID, IPC listener                   |
| Session (messages) | 0            | ~10–30 MB      | Conversation JSON; evicted when idle        |
| Event broker       | ~500 KB      | ~1 MB          | Channel buffers, subscriber registry        |
| SQLite connection  | ~2 MB        | ~3 MB          | WAL, page cache (constrained)               |
| Lua VM             | 0            | ~2 MB          | Only loaded if plugins exist                |
| MCP manager        | ~1 MB        | ~2 MB          | Connection state, tool catalog              |
| Tool execution     | 0            | 0              | Transient subprocesses — not in daemon heap |
| **Total**          | **~11 MB**   | **~45 MB**     | Well under targets                          |

Key insight: **tools contribute 0 bytes to daemon heap** because they're transient subprocesses. ripgrep runs in its own process, streams output, and exits. Its memory is never part of `agentd`'s RSS.

---

## 21. Failure Handling

| Failure                 | Behavior                                                                     |
| :---------------------- | :--------------------------------------------------------------------------- |
| Client crashes          | Daemon continues. Session persists. Client re-attaches later.                |
| All clients disconnect  | Session continues executing (detached mode).                                 |
| Daemon crashes          | Recover sessions from SQLite. Resume in-progress tasks from last checkpoint. |
| LLM API fails           | Retry with jittered exponential backoff. After max retries, pause session.   |
| LLM rate limit          | Queue request. Emit `session.rate_limited` event. Retry after cooldown.      |
| Tool subprocess hangs   | Timeout → kill process group → return error to LLM → LLM adjusts.            |
| MCP server crashes      | Auto-restart with backoff. Mark tools unavailable during downtime.           |
| SQLite write contention | WAL mode + busy timeout (5s). Serialized writer queue for mutations.         |
| Context window full     | Auto-compaction triggers before overflow.                                    |
| Disk full               | Graceful error. Pause artifact externalization. Warn via event.              |

---

## 22. Security Model

### 22.1 Daemon access control

- IPC socket/pipe permissions restricted to current user only (`0600` / DACL).
- Optional bearer token authentication for remote gateways (future).

### 22.2 Tool sandboxing

- Read-only tools: no sandboxing needed (pure file reads).
- BashTool: sandboxed via process groups and timeouts. Optional `workspace-only` mode restricts `cwd` to project directory.
- EditTool/WriteTool: optional path allowlists restricting which directories can be modified.

### 22.3 Plugin sandboxing

- Lua VM has no access to Go memory or OS primitives.
- Only explicitly bound capabilities are available.
- Execution limits prevent resource exhaustion.

### 22.4 Sensitive file protection

Files matching patterns like `.env`, `*_rsa`, `.git/credentials`, `*.key`, `*.pem` are flagged when read. The LLM receives a warning:

```text
⚠️ This file may contain sensitive data. Do not include its contents in any output visible to the user or external systems.
```

---

## 23. Comparison: Loki vs. Claude Code vs. Heavy IDE Agents

| Dimension           | Loki (this design) | Claude Code   | Cursor / Copilot |
| :------------------ | :----------------- | :------------ | :--------------- |
| Idle RAM            | **< 20 MB**        | ~50–100 MB    | 1.5–3.5 GB       |
| Active RAM          | **< 50 MB**        | ~100–200 MB   | 2.5–5.0 GB       |
| Startup             | **< 500 ms**       | < 1 s         | 10–30 s          |
| Code indexing       | **None**           | None          | Vector DB + LSP  |
| Multi-client        | **Yes**            | No            | No               |
| Detached execution  | **Yes**            | No            | No               |
| Session persistence | **SQLite**         | JSON files    | Varies           |
| Resource sharing    | **Single daemon**  | Per-process   | Per-process      |
| IDE integration     | **Native IPC**     | Terminal only | Built-in         |
| Plugin system       | **Lua**            | Limited       | Extension API    |

Loki combines Claude Code's efficiency with capabilities that Claude Code fundamentally cannot offer as a single-process CLI tool.

---

## 24. Key Architectural Decisions

| Decision           | Choice                             | Rationale                                            |
| :----------------- | :--------------------------------- | :--------------------------------------------------- |
| Agent backend      | Go                                 | Single binary, goroutines, low RSS, cross-platform   |
| Execution model    | Persistent daemon                  | Multi-client, detached execution, shared resources   |
| Code discovery     | Lazy fetch (ripgrep + glob + read) | Zero RAM overhead, always fresh, instant startup     |
| Caching strategy   | Context window is the cache        | No vector DBs, no indexes, no staleness              |
| File editing       | Exact string replacement           | LLMs are unreliable at diffs; string match is robust |
| Context management | Auto-compaction with LLM summary   | Infinite sessions without context overflow           |
| IPC                | Unix socket / Windows named pipe   | Zero network overhead, OS-level access control       |
| Wire format        | Length-prefixed JSON               | Simple, debuggable, sufficient for local IPC         |
| Persistence        | SQLite (WAL, CGO-free)             | Zero-config, transactional, single-file, portable    |
| Plugin runtime     | Lua only                           | Mature, tiny footprint, pure-Go VM available         |
| Tool execution     | Spawn-and-die subprocesses         | Zero resident memory for tools                       |
| Output handling    | Aggressive truncation at ingestion | Prevents context window exhaustion                   |
| Permission model   | Tool danger classification         | Simple, practical, configurable                      |
| Project context    | AGENTS.md / Agents.md file         | Global standard, zero-cost project fluency           |

---

## 25. What Loki Deliberately Does NOT Have

These are intentional omissions to keep the system lean:

- ❌ **No vector embedding database** — context window is the working memory.
- ❌ **No AST parser / symbol graph** — grep + glob is sufficient and always fresh.
- ❌ **No Language Server Protocol integration** — LSP daemons consume hundreds of MB.
- ❌ **No persistent filesystem watchers** — files are read on demand, not watched.
- ❌ **No background indexing** — zero startup cost, zero staleness risk.
- ❌ **No Electron/Chromium** — terminal-native, zero UI runtime overhead.
- ❌ **No WASM** — Lua is sufficient for the plugin use cases that matter.
- ❌ **No local LLM hosting** — Loki is an API-first runtime, not an inference engine.

---

## 26. Development Phases

### Phase 0: Foundation (v0.1.0)

- Go workspace + package scaffolding
- Cross-platform IPC transport (unix socket / named pipe)
- Length-prefixed JSON wire protocol + handshake

### Phase 1: MVP Daemon + CLI (v0.2.0 – v0.3.0)

- `agentd` background daemon lifecycle + auto-spawn
- ReAct agent loop with single LLM provider (Anthropic)
- Core tools: `ReadTool`, `GlobTool`, `GrepTool`, `ListTool`, `EditTool`, `WriteTool`, `BashTool`
- Output truncation pipeline
- SQLite persistence (sessions, messages, tool calls)
- AGENTS.md discovery and loading
- Minimal CLI thin client with streaming output
- Context manager with auto-compaction

### Phase 2: Multi-Client, MCP, Lua (v0.4.0 – v0.6.0)

- Multi-client session attachment + live rehydration
- Detached execution + re-attachment
- Permission system with danger classification
- Shared MCP manager
- Lua plugin engine with capability API
- Session management CLI commands

### Phase 3: IDE Integrations + Rich TUI (v0.7.0 – v0.8.0)

- Bubble Tea / Lipgloss TUI (split-view, diffs, command palette)
- Neovim plugin (`loki.nvim`) via IPC
- VS Code extension (`loki-vscode`) via IPC

### Phase 4: Multi-Agent + Production (v0.9.0 – v1.0.0)

- Multi-agent orchestration (planner, coder, tester, reviewer)
- Local web dashboard
- Performance profiling and optimization
- OS service integration (systemd, launchd, Windows service)
- Cross-platform release packaging

---

## 27. Core Thesis

```text
┌──────────────────────────────────────────────────────────────────────────┐
│                                                                          │
│   Claude Code's efficiency (lazy fetch, zero index, minimal RAM)         │
│                              +                                           │
│   Daemon persistence (multi-client, detached execution, shared state)    │
│                              =                                           │
│                           Loki                                           │
│                                                                          │
│   The leanest AI coding agent that doesn't sacrifice power.              │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

Build the agent once. Render it everywhere. Cache nothing.
