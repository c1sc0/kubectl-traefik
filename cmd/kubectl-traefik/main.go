package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/c1sc0/kubectl-traefik/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd := cli.NewRootCommand(version, genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := cmd.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, cli.ErrFindings) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}
