# Loki

> **Persistent Local AI Agent Runtime**
> _Build the agent once. Render it everywhere. Cache nothing._

Loki is an ultra-lightweight, persistent local AI coding-agent platform. The agent backend runs as a single per-user Go daemon (`agentd`), while terminals, editors, and future UIs act as thin ephemeral clients.

By combining **Claude Code's lazy-fetch, zero-index, minimal-RAM efficiency** with a **persistent daemon architecture**, Loki provides multi-client multiplexing and detached background execution without heavy language servers or vector databases.

## Key Design Principles

1. **The AI agent is a local service, not a terminal process.** The daemon owns execution; clients attach, detach, and reconnect without interrupting running tasks.
2. **Zero pre-indexing & lazy retrieval:** No vector embeddings, no AST parsers, and no persistent filesystem watchers. Code is fetched on demand via `ripgrep`, glob, and line-sliced file reads.
3. **Context-as-cache with prompt caching:** Active working memory resides in the LLM context window with Anthropic ephemeral prompt cache breakpoints, slashing costs by 90% and latency by 85%+.
4. **Spawn-and-die tools with ingestion truncation:** Search tools run as transient subprocesses leaving 0 bytes on the daemon heap. Bloated outputs are capped (500 lines / 40KB) before entering context.
5. **Exact string replacement editing:** Robust code modifications using exact match validation (`old_string` -> `new_string`) rather than fragile unified diffs.
6. **Shared resources:** A single daemon manages LLM connections, shared MCP servers, process supervision, and SQLite persistence across all connected clients.
7. **Capability-gated Lua extensibility:** Embedded pure-Go Lua runtime (`gopher-lua`) with strict capability sandboxing (no WASM overhead).
8. **Radically low resource footprint:** Target `< 20MB` daemon idle RSS, `< 50MB` active RSS, and 0% idle CPU.

## Documentation

- [Loki Master Architecture & System Design Document](docs/DESIGN.md)
- [Loki Engineering Roadmap](docs/roadmap.md)

## Roadmap Summary

For the comprehensive, phased work breakdown and milestone schedule, refer to the [Loki Engineering Roadmap](docs/roadmap.md).

- **Phase 0 (v0.1.0):** Foundation, project scaffolding, and cross-platform IPC transport (Unix domain sockets & Windows named pipes).
- **Phase 1 (MVP, v0.2.0 – v0.3.0):** Single Go `agentd` daemon, local IPC, event broker, ReAct loop, lazy retrieval & exact string edit tools, truncation gate, `AGENTS.md` orientation, auto-compaction, SQLite persistence, and minimal CLI thin client.
- **Phase 2 (v0.4.0 – v0.6.0):** Multi-client attachment & live rehydration, detached execution, shared MCP manager, tool danger classification & permission handshake, and capability-based Lua plugin engine.
- **Phase 3 (v0.7.0 – v0.8.0):** Rich interactive Bubble Tea terminal UI (TUI) and first-party Neovim (`loki.nvim`) & VS Code (`loki-vscode`) plugins.
- **Phase 4 (v0.9.0 – v1.0.0):** Multi-agent orchestration (planner, coder, tester, reviewer), workspace conflict prevention, embedded local web dashboard & authenticated remote gateway.
- **Phase 5 (Post-v1.0):** Performance profiling (<20MB idle RSS verification), OS service integration (systemd, launchd, Windows Service), and cross-platform packaging.
