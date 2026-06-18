package main

import (
	"os"

	"github.com/bonztm/agent-workflow-manager/internal/adapters/cli"
)

func main() {
	os.Exit(cli.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
