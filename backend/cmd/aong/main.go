package main

import (
	"fmt"
	"os"

	"github.com/aoagents/agent-orchestrator/backend/internal/aongcli"
)

func main() {
	if err := aongcli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(aongcli.ExitCode(err))
	}
}
