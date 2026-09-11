# Persistent Local AI Agent Runtime — Loki Design Document

## 1. Executive Summary

Build a lightweight, persistent local AI coding-agent platform in which the **agent backend runs as a single per-user daemon**, while terminals, editors, and future UIs act as thin clients.

The central design principle is:

> **The AI agent is a local service, not a terminal process.**

A single Go daemon owns agent execution, sessions, context, tool connections, filesystem state, subprocesses, caches, and event distribution. Multiple clients connect to that daemon through IPC and subscribe to the sessions they need.

The first plugin/runtime layer will use **Lua** for simplicity, maturity, and rapid iteration. The system will deliberately expose Lua through a stable capability-oriented plugin API so that **WebAssembly can be introduced later without redesigning the core architecture**.

The long-term product is therefore not merely another terminal coding assistant. It is a **local AI agent runtime with multiple interchangeable clients**.

---

## 2. Problem

Most AI coding assistants are architected around the UI/process:

```text
Terminal
   ↓
Agent runtime
   ↓
LLM
   ↓
Tools / MCP / filesystem
```

Opening multiple instances can result in duplicated:

* runtime processes
* language runtimes
* MCP connections
* filesystem watchers
* caches
* context/state
* subprocess managers
* indexes
* connection pools

This is particularly inefficient for a developer running several terminals, editor integrations, or parallel agent sessions.

Additionally, the UI often becomes tightly coupled to the agent's lifecycle.

The proposed architecture reverses this relationship.

---

## 3. Goals

### Primary goals

#### 3.1 Persistent agent daemon

Start one `agentd` process for the user and keep it alive independently of any individual UI.

#### 3.2 Multiple clients

Allow many clients to connect simultaneously:

```text
agentd
 ├── Terminal 1
 ├── Terminal 2
 ├── Terminal 3
 ├── Neovim
 ├── VS Code
 └── Web UI
```

#### 3.3 Shared resources

Resources should be shared wherever possible:

* LLM connection pools
* MCP connections
* filesystem watchers
* repository indexes
* caches
* tool registries
* subprocess management
* authentication/configuration state

#### 3.4 Persistent sessions

A session must survive client termination.

```text
Terminal
   ↓
disconnect

agentd
   ↓
continues task

Terminal
   ↓
attach
```

#### 3.5 Thin clients

Clients should primarily own:

* rendering state
* viewport state
* input
* selection
* local UI state

They should not own the complete agent runtime.

#### 3.6 Extensibility

Lua plugins should be supported initially.

The architecture must allow WASM or another runtime to be added later without changing the agent core API.

#### 3.7 Low resource overhead

Target a substantially smaller footprint than browser/Electron-style AI interfaces and minimize duplicate work when multiple clients are open.

---

## 4. Non-Goals

The initial version will not attempt to:

* replace every IDE integration
* implement a distributed cloud agent control plane
* support arbitrary native plugins
* provide a fully remote multi-user server
* make Lua the permanent plugin architecture
* optimize every individual LLM provider
* build a browser UI before the daemon/client protocol stabilizes

---

## 5. High-Level Architecture

```text
                         ┌──────────────────────────┐
                         │         agentd           │
                         │                          │
                         │       Go Runtime         │
                         │                          │
                         │  Agent Engine            │
                         │  Session Manager         │
                         │  Context Manager         │
                         │  Tool Manager            │
                         │  MCP Manager             │
                         │  Process Manager         │
                         │  Filesystem Watcher      │
                         │  Cache                   │
                         │  Event Broker            │
                         │  Persistence             │
                         │  Plugin Manager          │
                         └────────────┬─────────────┘
                                      │
                                 Local IPC
                                      │
             ┌────────────────────────┼────────────────────────┐
             │                        │                        │
             ▼                        ▼                        ▼
         Terminal                  Neovim                  VS Code
          client                   client                   client
             │                        │                        │
             └────────────────────────┴────────────────────────┘
                                      │
                              Future clients
                                      │
                              Web / Mobile / GUI
```

---

## 6. Core Architectural Model

The system has three primary concepts.

### 6.1 Daemon

`agentd` is the persistent local service.

Responsibilities:

* lifecycle management
* session execution
* LLM communication
* tool execution
* MCP management
* filesystem state
* persistence
* event streaming
* client connections
* plugin execution

The daemon remains independent of UI lifecycle.

---

### 6.2 Session

A session represents an AI task/conversation.

Example:

```text
Session A
  repository: payments
  branch: feature/refund
  context: ...
  agent state: ...
```

Multiple clients can attach to the same session.

```text
               Session A
                  │
        ┌─────────┼─────────┐
        │         │         │
      TUI       Neovim    VS Code
```

Sessions are persistent entities rather than processes.

---

### 6.3 Client

A client is a renderer/controller for a session.

It should be intentionally thin.

Responsibilities:

```text
connect()
authenticate()
subscribe()
send_input()
render()
```

The client should not need to know how the agent internally plans, invokes tools, maintains context, or communicates with the model.

---

## 7. Event-Driven Architecture

Clients should not receive an indiscriminate stream of all daemon activity.

Instead, the daemon exposes a multiplexed event broker.

Example:

```text
session.message.delta
session.message.complete
tool.started
tool.output
tool.completed
filesystem.changed
permission.requested
agent.state.changed
session.finished
session.failed
```

A client subscribes selectively.

Example:

```text
subscribe(session_id="abc")
```

or:

```text
subscribe(
    session_id="abc",
    events=[
        "message.*",
        "tool.*",
        "permission.*"
    ]
)
```

This reduces unnecessary data transfer and rendering work.

---

## 8. IPC

The first implementation should favor local IPC.

### Linux/macOS

Unix domain sockets.

### Windows

Named pipes.

A transport abstraction should hide the platform-specific implementation.

Recommended protocol characteristics:

* bidirectional
* streaming
* framed messages
* versioned
* request/response support
* asynchronous events
* reconnectable

A message protocol could initially use JSON for development simplicity.

A later version can move to protobuf or another compact binary protocol without changing the conceptual API.

---

## 9. Client Lifecycle

Example:

```text
$ agent
```

### First invocation

```text
client
  │
  ├── connect to agentd
  │
  └── daemon unavailable
          │
          ▼
      start agentd
          │
          ▼
       connect
```

### Later invocations

```text
client 1 ──┐
client 2 ──┼──→ existing agentd
client 3 ──┘
```

No additional agent backend is started.

---

## 10. Detached Execution

The daemon is responsible for work even when no client is attached.

Example:

```text
User:
"Refactor this module and run the tests."
```

Then:

```text
TUI
 │
 ├── request
 ▼
agentd
 │
 ├── planning
 ├── editing
 ├── tests
 └── results
```

User closes the terminal.

The daemon continues.

Later:

```text
agent attach
```

The client reconstructs the current state from persisted session data and live events.

---

## 11. Resource Sharing

This is a major differentiator.

Instead of:

```text
T1 → MCP #1
T2 → MCP #2
T3 → MCP #3
```

the daemon provides:

```text
T1 ─┐
T2 ─┼──→ MCP Manager → MCP server
T3 ─┘
```

Likewise:

```text
T1 ─┐
T2 ─┼──→ filesystem watcher
T3 ─┘
```

and:

```text
T1 ─┐
T2 ─┼──→ LLM connection pool
T3 ─┘
```

This reduces duplicated process and memory consumption.

---

## 12. State Management

The daemon should distinguish between:

### Durable state

Stored persistently:

* session metadata
* conversation records
* tool execution history
* configuration
* permissions
* task state
* checkpoints

### Ephemeral state

Kept in memory:

* active streams
* currently running tools
* client subscriptions
* temporary buffers
* connection state

The system should avoid keeping unnecessarily large rendered UI structures in memory.

The terminal should store only what it needs for rendering.

---

## 13. Context Management

The complete model context should belong to the daemon.

Clients should never be required to maintain the canonical context.

```text
client
   │
   │ user input
   ▼
agentd
   │
   ├── context manager
   ├── summarization
   ├── retrieval
   ├── tool history
   └── model request
```

This makes client reconnection straightforward.

It also prevents multiple clients from maintaining divergent versions of the conversation.

---

## 14. Plugin Architecture

The plugin system must be separated from the internal Go implementation.

The key rule is:

> **Lua is an implementation of the plugin API, not the plugin API itself.**

Conceptually:

```text
             Stable Plugin API
                    │
           ┌────────┴────────┐
           │                 │
        Lua runtime      WASM runtime
           │                 │
        plugins           plugins
```

---

## 15. Capability-Based Plugin API

Plugins should receive explicit capabilities rather than direct access to Go internals.

Example capabilities:

```text
session.send()
session.get()
session.subscribe()

filesystem.read()
filesystem.write()

tool.execute()

ui.notify()
ui.register_command()

config.get()

agent.log()
```

A plugin should not receive pointers to internal objects such as:

```go
*Agent
*Session
*Cache
*MCPManager
```

This creates a clean boundary and protects the future WASM migration.

---

## 16. Lua — Phase 1

Lua is the initial embedded scripting environment.

Potential use cases:

* hooks
* commands
* key bindings
* notifications
* automation
* agent customization
* event handling
* workflow logic

Example:

```lua
agent.events.on("tool.completed", function(event)
    if event.tool == "pytest" then
        agent.ui.notify("Tests completed")
    end
end)
```

Lua execution remains inside the daemon.

Lua should only interact through the public capability API.

---

## 17. WASM — Phase 2

WASM can be introduced later as a second plugin runtime.

```text
                  Plugin Manager
                        │
              ┌─────────┴─────────┐
              │                   │
           Lua VM             WASM runtime
              │                   │
          Lua plugins          WASM plugins
```

Advantages:

* language independence
* sandboxing
* stronger isolation
* third-party ecosystem potential
* controlled capabilities
* predictable deployment

The existing plugin API remains the conceptual contract.

Only another runtime adapter is added.

---

## 18. Why Go

Go is a strong fit for the daemon because its workload is highly concurrent.

The daemon will simultaneously handle:

* streaming model responses
* many clients
* MCP connections
* subprocesses
* filesystem watchers
* plugin execution
* event distribution
* persistence
* timers/background tasks

Goroutines and channels make this model natural.

Go also provides:

* single-binary deployment
* easy cross-platform distribution
* low operational complexity
* strong networking primitives
* mature CLI ecosystem
* straightforward concurrency

---

## 19. Suggested Go Package Structure

```text
agent/
├── cmd/
│   └── agentd/
│
├── internal/
│   ├── daemon/
│   ├── agent/
│   ├── session/
│   ├── context/
│   ├── llm/
│   ├── tools/
│   ├── mcp/
│   ├── process/
│   ├── filesystem/
│   ├── events/
│   ├── persistence/
│   ├── cache/
│   ├── ipc/
│   └── security/
│
├── plugins/
│   ├── api/
│   └── lua/
│
└── client/
    └── protocol/
```

The exact package boundaries can evolve; the important principle is keeping the daemon core separate from runtime adapters.

---

## 20. Persistence

SQLite is a strong initial choice.

Possible schema concepts:

```text
sessions
messages
tool_calls
checkpoints
clients
permissions
plugins
configuration
```

SQLite provides:

* local persistence
* transactional updates
* no separate database process
* simple backups
* mature Go support

Large blobs should not necessarily live directly in the main session tables. Artifacts can be stored separately and referenced from SQLite.

---

## 21. Security Model

A local daemon becomes a powerful process, therefore security must be part of the architecture.

The daemon may have access to:

```text
filesystem
shell
git
network
credentials
MCP servers
```

Recommended model:

```text
Client
   ↓
authentication
   ↓
session permissions
   ↓
capability checks
   ↓
tool execution
```

Plugins should never implicitly inherit every capability.

For example:

```text
plugin A:
filesystem.read

plugin B:
filesystem.read
filesystem.write

plugin C:
filesystem.read
shell.execute
```

Later WASM can make this boundary substantially stronger through sandboxing.

---

## 22. Memory Optimization Strategy

The daemon architecture alone is not sufficient.

The implementation should explicitly optimize:

### Shared resources

One process/resource manager instead of one per client.

### Thin clients

Do not duplicate the entire agent state in every UI.

### Event streaming

Do not continuously copy large state snapshots.

### Viewport rendering

Render only the visible terminal content where possible.

### Context compaction

Do not indefinitely retain every token in active memory.

### Externalized artifacts

Keep large tool outputs/artifacts outside the hot in-memory state.

### Connection pooling

Reuse model/network resources.

---

## 23. Example Runtime

Three terminals are open:

```text
               agentd
                 │
       ┌─────────┼──────────┐
       │         │          │
       T1        T2         T3
       │         │          │
     S1/S2      S1          S3
```

Resources:

```text
1 daemon
1 MCP manager
1 filesystem watcher
1 process manager
1 cache
1 persistence layer
```

instead of:

```text
3 full agent processes
3 MCP managers
3 watchers
3 caches
...
```

The actual memory savings will depend heavily on the agent implementation and client runtime, so these should be benchmarked rather than assumed.

---

## 24. Client Protocol Example

Client → daemon:

```json
{
  "type": "session.attach",
  "session_id": "abc123"
}
```

Daemon → client:

```json
{
  "type": "session.state",
  "session_id": "abc123",
  "state": "running"
}
```

Streaming response:

```json
{
  "type": "message.delta",
  "session_id": "abc123",
  "message_id": "m42",
  "delta": "Running tests..."
}
```

Tool event:

```json
{
  "type": "tool.started",
  "session_id": "abc123",
  "tool": "shell",
  "command": "go test ./..."
}
```

The exact wire format can change later. The semantic events should remain stable.

---

## 25. Failure Handling

The daemon must be the lifecycle authority.

Examples:

### Client crashes

Daemon continues.

### Daemon crashes

Recover durable sessions from SQLite/checkpoints.

### LLM connection fails

Session enters recoverable state.

### MCP server dies

MCP manager reconnects or marks the capability unavailable.

### Tool process hangs

Process manager enforces timeout/cancellation.

### Client disconnects during streaming

Streaming state remains owned by daemon.

---

## 26. Concurrency Model

A session should have controlled concurrency.

For example:

```text
Session
  │
  ├── agent execution loop
  ├── tool execution
  ├── event publishing
  └── client subscriptions
```

Avoid arbitrary concurrent mutation of session state.

Prefer explicit ownership/serialization of state transitions.

---

## 27. Multi-Agent Future

The architecture should eventually permit:

```text
                 agentd
                   │
        ┌──────────┼───────────┐
        │          │           │
      Agent A    Agent B     Agent C
        │          │           │
      coding     tests       review
```

The daemon can coordinate:

* dependencies
* file ownership
* shared context
* subprocesses
* permissions
* event streams

This becomes a foundation for parallel coding agents rather than merely a chatbot.

---

## 28. Product Surface

Initial product:

```text
agent CLI
```

Later:

```text
agent CLI
agent attach
agent list
agent sessions
agent kill
agent logs
```

Then:

```text
Neovim plugin
VS Code extension
Web client
Desktop client
Remote client
```

All clients communicate with the same daemon.

---

## 29. Initial MVP

The MVP should deliberately remain small.

### Phase 1

```text
Go agentd
   │
   ├── one LLM provider
   ├── session manager
   ├── SQLite
   ├── Unix socket / named pipe
   ├── event broker
   └── basic shell/filesystem tools
```

Client:

```text
minimal terminal UI
```

No plugin marketplace.

No Web UI.

No WASM.

---

## 30. Phase 2

Add:

```text
MCP
multiple clients
session attach/detach
persistent background tasks
Lua runtime
plugin API
permission system
resource sharing
```

---

## 31. Phase 3

Add:

```text
WASM runtime
Neovim
VS Code
remote/local Web UI
plugin distribution
sandboxing
multi-agent orchestration
```

---

## 32. Key Architectural Decisions

| Decision               | Choice                           |
| ---------------------- | -------------------------------- |
| Agent backend          | Go                               |
| Execution model        | Persistent local daemon          |
| UI model               | Thin clients                     |
| IPC                    | Unix socket / Windows named pipe |
| State                  | Daemon-owned                     |
| Persistence            | SQLite                           |
| Communication          | Event-driven                     |
| Initial plugin runtime | Lua                              |
| Future plugin runtime  | WASM                             |
| Plugin abstraction     | Capability-based API             |
| MCP ownership          | Daemon                           |
| Tool ownership         | Daemon                           |
| Context ownership      | Daemon                           |
| UI ownership           | Client                           |
| Session ownership      | Daemon                           |

---

## 33. Core Design Principle

The architecture can be summarized as:

```text
                 UI is ephemeral
                       │
                       ▼
                 ┌───────────┐
                 │   Client  │
                 └─────┬─────┘
                       │
                       │ IPC
                       ▼
              ┌──────────────────┐
              │     agentd       │
              │                  │
              │ persistent state │
              │ agent execution  │
              │ tools            │
              │ MCP              │
              │ context          │
              │ resources        │
              │ plugins          │
              └──────────────────┘
```

The daemon is the product's **execution substrate**.

The terminal is merely one view of it.

---

## 34. Long-Term Vision

The eventual system should feel more like a local operating-system service than a traditional CLI program:

```text
                        agentd
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
      Terminal          IDE              Mobile
        │                 │                 │
        └─────────────────┼─────────────────┘
                          │
                     same sessions
                     same context
                     same agents
                     same tools
```

A developer can start an agent once, detach from it, reconnect from another interface, and allow the system to continue operating independently of any individual UI.

The design deliberately starts with a simple embedded Lua runtime while maintaining a stable plugin boundary that can later support WASM without requiring the Go agent core to be rewritten.

**Primary thesis:**

> Build the agent once. Render it everywhere.
