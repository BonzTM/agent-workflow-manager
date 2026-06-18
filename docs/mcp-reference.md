# MCP Reference

This document provides a detailed reference for the Model Context Protocol (MCP) interface of AWM.

## Overview

The `awm-mcp` binary is a JSON-RPC 2.0 stdio server that implements the Model Context Protocol. It allows LLMs and runtimes to discover and call AWM tools for context management, task tracking, and workflow governance.

For a detailed guide on how to wire AWM into your specific runtime, see the [Integration Guide](integration.md).

## Protocol

AWM uses the standard MCP JSON-RPC 2.0 protocol over standard input and output (stdio). Communication is line-delimited JSON.

## Methods

### initialize
Standard MCP handshake to establish capabilities and versions.

### tools/list
Returns the list of 12 available tools along with their descriptions and input schemas.

### tools/call
Invokes a specific tool with the provided arguments.

## Available Tools

Twelve tools are exposed through the MCP interface:

- **Core agent-facing** (4): `context`, `work`, `verify`, `done`
- **Supporting agent-facing** (3): `fetch`, `review`, `history`
- **Advanced backend-only** (1): `export`
- **Maintenance** (4): `sync`, `health`, `status`, `init`

### Tool Groups

| Group | Tools | Description |
|---|---|---|
| **Context** | `context`, `fetch`, `history` | Establish task context, hydrate file pointers, and recall history. |
| **Execution** | `work`, `sync` | Manage task progress, subtasks, and sync local config. |
| **Governance** | `verify`, `review`, `done` | Run automated tests, satisfy workflow gates, and close tasks. |
| **Admin** | `health`, `status`, `init`, `export` | Manage the AWM environment and export task data. |

## Verification vs Review

`verify` and `review` are intentionally different:

- `verify` runs deterministic repo-defined executable checks from `.awm/awm-tests.yaml` and updates `verify:tests`.
- `review` satisfies one named workflow gate from `.awm/awm-workflows.yaml`. In run mode, it executes that gate's `run` block and records review attempts for one review task such as `review:cross-llm`.

| Question | Tool |
|---|---|
| "Which repo-defined checks apply to this task and current diff?" | `verify` |
| "Has this one named workflow signoff gate been satisfied?" | `review` |

## Typical Closeout Sequence

The typical governed closeout sequence is:
1. `work` (final check-in)
2. `verify` (automated checks)
3. `review` (manual or cross-llm signoff, if required)
4. `done` (terminal closure)

## Error Handling

### JSON-RPC Errors
AWM uses standard JSON-RPC 2.0 error codes:
- `-32700`: Parse error
- `-32600`: Invalid Request
- `-32601`: Method not found
- `-32602`: Invalid params
- `-32603`: Internal error

### AWM Application Errors
When a tool call fails but the protocol is correct, AWM returns a result with `isError: true` and a JSON string in the content field containing:
- `status`: "error"
- `message`: A descriptive error message
- `code`: An AWM-specific error code (e.g., `not_found`, `validation_failed`, `precondition_failed`)
