# Dexstor

Local LLM controller and autonomous coding agent written in Go.

Dexstor provides a controlled execution environment for local LLM models with persistent workflow state, tool validation and recovery mechanisms.

## Architecture

```text
cmd/
 ├── bratan/
 │    └── main.go
 └── bratan-web/
      ├── main.go
      └── index.html

internal/
 ├── agent/
 │    ├── agent.go
 │    ├── state.go
 │    ├── task_state.go
 │    ├── reconcile.go
 │    └── FSM / ledger / recovery tests
 │
 ├── tools/
 │    ├── tools.go
 │    ├── project_validator.go
 │    ├── snapshot_diff.go
 │    ├── html_validator.go
 │    └── file safety / exec / write validation tests
 │
 ├── dependencygraph/
 ├── ollama/
 ├── parser/
 └── projectindex/
```

## Components

### Agent

`internal/agent`

Core autonomous controller:

- workflow state machine (FSM)
- persistent task state
- action ledger
- crash recovery
- repair and reconciliation flow

### Tools

`internal/tools`

Controlled execution layer:

- filesystem operations
- protected writes
- execution validation
- project verification
- snapshot and diff checks

### Project analysis

`internal/projectindex`

Indexes project structure and source information.

`internal/dependencygraph`

Tracks relationships between project components.

### LLM layer

`internal/ollama`

Communication with local Ollama models.

`internal/parser`

Parsing and handling model tool calls.

## Design goals

- Local-first operation
- Verifiable agent actions
- Controlled tool execution
- Persistent state recovery
- Minimal external dependencies

Dexstor is designed as a local autonomous controller where the LLM proposes actions and the Go controller validates and executes them.
