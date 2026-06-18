# AWM Integration Guide

This guide describes how to integrate the Agent Workflow Manager (AWM) into any Model Context Protocol (MCP) compatible runtime using the JSON-RPC 2.0 stdio protocol.

## Overview

AWM exposes its tools via the `awm-mcp` binary. This binary implements the MCP standard over standard input and output (stdio). It allows LLM runtimes to discover and call AWM tools for context management, task tracking, and workflow governance.

## Protocol

AWM uses JSON-RPC 2.0 over stdio. Communications are line-delimited JSON objects.
- Standard input (stdin) for requests and notifications to AWM.
- Standard output (stdout) for responses and notifications from AWM.

## Handshake

Every session begins with a standard MCP handshake.

### 1. Initialize Request (Client → AWM)
The client sends its capabilities and version.

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-03-26",
    "capabilities": {},
    "clientInfo": {
      "name": "my-runtime",
      "version": "1.0"
    }
  }
}
```

### 2. Initialize Response (AWM → Client)
AWM responds with its capabilities, including the tool server.

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-03-26",
    "capabilities": {
      "tools": {}
    },
    "serverInfo": {
      "name": "awm-mcp",
      "version": "0.1.0"
    }
  }
}
```

### 3. Initialized Notification (Client → AWM)
The client confirms it has processed the initialization.

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/initialized"
}
```

## Listing Tools

To discover available tools, use the `tools/list` method.

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list"
}
```

AWM returns a list of 12 tools, including `context`, `work`, `verify`, and `done`, along with their descriptions and input schemas.

## Calling Tools

Invoke tools using the `tools/call` method with the tool name and arguments.

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "context",
    "arguments": {
      "project_id": "myapp",
      "task_text": "fix login bug",
      "phase": "execute"
    }
  }
}
```

### Tool Response
Responses are wrapped in a content array.

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"version\":\"awm.v1\",\"status\":\"success\",...}"
      }
    ]
  }
}
```

## Error Handling

### Protocol Errors
Standard JSON-RPC error codes are used for transport or protocol failures (e.g., -32601 for Method not found).

### Application Errors
If a tool execution fails, AWM returns a response with `isError: true` and the error details in the content.

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "isError": true,
    "content": [
      {
        "type": "text",
        "text": "{\"version\":\"awm.v1\",\"status\":\"error\",\"message\":\"project not found\"}"
      }
    ]
  }
}
```

## Runtime Configuration

### Claude Desktop
Add AWM to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "awm": {
      "command": "awm-mcp",
      "args": [],
      "env": {
        "AWM_PROJECT_ID": "my-project"
      }
    }
  }
}
```

### OpenCode
Configure AWM as an MCP server in your OpenCode settings using the `awm-mcp` binary path.

### Generic Integration
Launch `awm-mcp` as a subprocess and communicate via stdin/stdout.

## Environment Variables

| Variable | Description |
|---|---|
| `AWM_PROJECT_ID` | Default project ID for AWM operations |
| `AWM_PG_DSN` | PostgreSQL connection string (if using Postgres storage) |
| `AWM_SQLITE_PATH` | Path to SQLite database (default: `~/.awm/awm.db`) |
| `AWM_LOG_LEVEL` | Logging verbosity (debug, info, warn, error) |
| `AWM_LOG_SINK` | Where to send logs (stderr, file path) |

## Typical Workflow

1. **`context`**: Establish the task and phase.
2. **`work`**: Check in progress and get guidance.
3. **`verify`**: Validate changes against requirements.
4. **`review`**: Prepare for final submission.
5. **`done`**: Close out the task.
