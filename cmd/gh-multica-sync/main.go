// Command gh-multica-sync mirrors GitHub pull requests into a local Multica
// instance.
//
// Install it as a gh extension (`gh extension install fgmacedo/gh-multica-sync`)
// and run it as `gh multica-sync <command>`.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fgmacedo/gh-multica-sync/internal/cli"
)

// version is injected at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	cli.Version = version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Run(ctx, cli.NewEnv(), os.Args[1:]))
}
