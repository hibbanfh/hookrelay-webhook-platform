// Command hookrelay is the single binary behind the API, the worker and the
// one-shot migrate/seed jobs (ADR 0002). This file only dispatches subcommands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = "usage: hookrelay <api|worker|migrate up|migrate down|seed>"

func main() {
	os.Exit(realMain())
}

func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hookrelay:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand\n%s", usage)
	}
	switch args[0] {
	case "api":
		return runAPI(ctx)
	case "worker":
		return runWorker(ctx)
	case "migrate":
		return runMigrate(args[1:])
	case "seed":
		return runSeed(ctx)
	default:
		return fmt.Errorf("unknown subcommand %q\n%s", args[0], usage)
	}
}
