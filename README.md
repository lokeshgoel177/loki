# Loki

> **Persistent Local AI Agent Runtime**
> *Build the agent once. Render it everywhere.*

Loki is a lightweight, persistent local AI coding-agent platform in which the agent backend runs as a single per-user daemon (`agentd`), while terminals, editors, and future UIs act as thin clients.

## Key Design Principles

1. **The AI agent is a local service, not a terminal process.**
2. **Thin clients:** Terminals, IDEs, and UIs attach/detach to persistent sessions without owning or interrupting the agent's execution lifecycle.
3. **Shared resources:** A single daemon manages LLM connections, MCP servers, filesystem watchers, subprocesses, caches, and context.
4. **Extensibility & Sandboxing:** Capability-based plugin API, starting with embedded Lua and designed for WebAssembly (WASM).

## Documentation

For full architectural details, see the design document:
- [Loki Architecture & Design Document](docs/DESIGN.md)

## Roadmap

- **Phase 1 (MVP):** Single Go `agentd` daemon, local IPC (Unix sockets / Windows named pipes), event broker, SQLite persistence, session manager, basic shell & filesystem tools, minimal CLI thin client.
- **Phase 2:** MCP support, multiple simultaneous clients, detached sessions & re-attachment, Lua plugin system, permission system, resource sharing.
- **Phase 3:** WASM runtime, Neovim/VS Code extensions, web interface, multi-agent orchestration.
