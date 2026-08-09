// Package cli implements the subcommands.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/fgmacedo/gh-multica-sync/internal/config"
	"github.com/fgmacedo/gh-multica-sync/internal/forge/github"
	"github.com/fgmacedo/gh-multica-sync/internal/ghcli"
)

// Version is set at build time.
var Version = "dev"

// Env groups dependencies so commands can be tested without touching the real
// environment.
type Env struct {
	Out    io.Writer
	Err    io.Writer
	GH     ghcli.Runner
	Forge  *github.Client
	Settle func() config.Settings
}

func NewEnv() *Env {
	gh := ghcli.New()
	return &Env{
		Out:    os.Stdout,
		Err:    os.Stderr,
		GH:     gh,
		Forge:  github.New(gh),
		Settle: config.Resolve,
	}
}

const usage = `gh-multica-sync %s

Mirrors GitHub pull requests into a local Multica instance, with no GitHub App
and no public tunnel. Enabled per repository.

USAGE
  gh multica-sync <command> [arguments]

COMMANDS
  doctor                   Diagnose the environment without changing anything
  bootstrap [--write-env]  Bind a local installation to your workspace
  enable [owner/repo]      Enable a repository (with no argument, the current one)
  disable [owner/repo]     Disable a repository
  status                   Show settings and enabled repositories
  sync [owner/repo] [n]    Sync now (with no argument, the current repository)
  poll                     Sweep enabled repositories and emit what changed
  install-timer            Install the LaunchAgent that polls every 5 minutes
  install-hook             Install a pre-push hook that speeds up polling here
  version                  Print the version

Start with 'doctor': it lists what is missing and the command that fixes each.
`

// Run dispatches the subcommand and returns the exit code.
func Run(ctx context.Context, e *Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(e.Out, usage, Version)
		return 0
	}

	var err error
	switch args[0] {
	case "doctor":
		err = runDoctor(ctx, e)
	case "bootstrap":
		err = runBootstrap(ctx, e, args[1:])
	case "enable":
		err = runEnable(ctx, e, args[1:])
	case "disable":
		err = runDisable(ctx, e, args[1:])
	case "status":
		err = runStatus(ctx, e)
	case "sync":
		err = runSync(ctx, e, args[1:])
	case "poll":
		err = runPoll(ctx, e)
	case "install-timer":
		err = runInstallTimer(ctx, e)
	case "install-hook":
		err = runInstallHook(ctx, e)
	case "mark-push":
		err = runMarkPush(ctx, e, args[1:])
	case "version", "--version", "-v":
		fmt.Fprintln(e.Out, Version)
	case "help", "--help", "-h":
		fmt.Fprintf(e.Out, usage, Version)
	default:
		fmt.Fprintf(e.Err, "unknown command: %s\n\n", args[0])
		fmt.Fprintf(e.Err, usage, Version)
		return 2
	}

	if err != nil {
		fmt.Fprintf(e.Err, "error: %v\n", err)
		return 1
	}
	return 0
}

// resolveRepo accepts "owner/repo" or, with no argument, resolves the current
// directory. An explicit repository wins so the tool can run outside a
// checkout, which is what the timer does.
func (e *Env) resolveRepo(ctx context.Context, args []string) (string, string, error) {
	if len(args) > 0 && args[0] != "" {
		return github.ParseRepo(args[0])
	}
	owner, repo, err := e.Forge.CurrentRepo(ctx)
	if err != nil {
		return "", "", fmt.Errorf("no repository argument and could not resolve the current one: %w", err)
	}
	return owner, repo, nil
}
