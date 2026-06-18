package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/adapters/mcp"
)

func TestRunMCP_HelpReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := mcp.RunMCP([]string{"--help"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "mcp") && !strings.Contains(out, "MCP") && !strings.Contains(out, "awm-mcp") {
		t.Errorf("expected help output to mention mcp, got: %s", out)
	}
}

func TestRunMCP_VersionReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := mcp.RunMCP([]string{"--version"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
}
