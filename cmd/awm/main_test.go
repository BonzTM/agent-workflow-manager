package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bonztm/agent-workflow-manager/internal/adapters/cli"
)

func TestRunCLI_HelpReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.RunCLI([]string{"--help"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "context") {
		t.Errorf("expected help output to mention 'context', got: %s", out)
	}
}

func TestRunCLI_VersionReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.RunCLI([]string{"--version"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "awm") {
		t.Errorf("expected version output to contain 'awm', got: %s", out)
	}
}

func TestRunCLI_UnknownSubcommandReturnsNonZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.RunCLI([]string{"nonexistent-cmd"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code for unknown subcommand")
	}
}
