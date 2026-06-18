package main

import (
	"os"

	"github.com/bonztm/agent-workflow-manager/internal/adapters/mcp"
)

func main() {
	os.Exit(mcp.RunMCP(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
