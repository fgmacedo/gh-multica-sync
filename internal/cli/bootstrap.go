package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/fgmacedo/gh-multica-sync/internal/bootstrap"
	"github.com/fgmacedo/gh-multica-sync/internal/config"
)

func runBootstrap(ctx context.Context, e *Env, args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	writeEnv := fs.Bool("write-env", false, "set GITHUB_APP_SLUG and GITHUB_WEBHOOK_SECRET in the server's .env")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s := e.Settle()
	if s.CLIErr != nil {
		return fmt.Errorf("could not find the Multica configuration: %w", s.CLIErr)
	}
	if s.Token == "" || s.WorkspaceID == "" {
		return fmt.Errorf("the Multica configuration has no token or workspace: run 'multica setup self-host' first")
	}

	// Step 1: the two variables on the server. Writing to someone's .env and
	// restarting their backend are different things: we do the first on
	// explicit request, and never the second, because taking someone's backend
	// down unannounced is the kind of surprise that burns trust in a tool.
	if s.WebhookSecret == "" {
		if !*writeEnv {
			return fmt.Errorf(
				"the server has no GITHUB_WEBHOOK_SECRET set in %s.\n"+
					"  Run with --write-env to have me set both variables, or set them by hand:\n"+
					"    GITHUB_APP_SLUG=%s\n"+
					"    GITHUB_WEBHOOK_SECRET=<random>",
				s.EnvPath, bootstrap.DefaultAppSlug)
		}
		secret, err := bootstrap.GenerateSecret()
		if err != nil {
			return err
		}
		vars := map[string]string{"GITHUB_WEBHOOK_SECRET": secret}
		if s.AppSlug == "" {
			vars["GITHUB_APP_SLUG"] = bootstrap.DefaultAppSlug
		}
		if err := bootstrap.WriteEnvVars(s.EnvPath, vars); err != nil {
			return err
		}
		fmt.Fprintf(e.Out, "Wrote the variables to %s.\n\n", s.EnvPath)
		fmt.Fprintf(e.Out, "Restart the backend so it picks them up, then run this command again:\n")
		fmt.Fprintf(e.Out, "  cd %s && docker compose -f docker-compose.selfhost.yml up -d backend\n", s.ServerDir)
		return nil
	}

	// Step 2: the installation. If one is already bound we do not create a
	// second: two installations for the same workspace would only double the
	// events.
	m := bootstrap.NewMultica(s.ServerURL, s.Token)
	resp, err := m.Installations(s.WorkspaceID)
	if err != nil {
		return err
	}
	if !resp.Configured {
		return fmt.Errorf("the running server does not see the .env variables yet: restart the backend and try again")
	}
	if s.InstallationID != 0 && resp.Bound(s.InstallationID) {
		fmt.Fprintf(e.Out, "Installation %d is already bound to this workspace. Nothing to do.\n", s.InstallationID)
		return nil
	}

	id := s.InstallationID
	if id == 0 {
		if id, err = bootstrap.GenerateInstallationID(); err != nil {
			return err
		}
	}
	if err := m.Bind(s.WorkspaceID, id); err != nil {
		return err
	}

	f, err := config.Load()
	if err != nil {
		return err
	}
	f.InstallationID = id
	if err := config.Save(f); err != nil {
		return err
	}

	// Trusting the 200 would be exactly the mistake this tool exists to avoid:
	// we confirm by reading it back.
	resp, err = m.Installations(s.WorkspaceID)
	if err != nil {
		return err
	}
	if !resp.Bound(id) {
		return fmt.Errorf("the server accepted the call but installation %d does not appear in the workspace", id)
	}

	fmt.Fprintf(e.Out, "Installation %d bound to workspace %s.\n", id, short(s.WorkspaceID))
	fmt.Fprintln(e.Out, "Now enable a repository: gh multica-sync enable")
	return nil
}
