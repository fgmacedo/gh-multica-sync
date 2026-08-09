package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fgmacedo/gh-multica-sync/internal/bootstrap"
	"github.com/fgmacedo/gh-multica-sync/internal/ghcli"
)

type check struct {
	name string
	ok   bool
	info string
	fix  string
}

// runDoctor inspects every prerequisite and, for whatever is missing, prints
// the command that fixes it. It changes nothing: safe to run at any time, and
// the first thing to run when something stops working.
func runDoctor(ctx context.Context, e *Env) error {
	s := e.Settle()
	checks := []check{}

	// 1. gh authenticated. Without it there is no way to discover any pull
	// request at all.
	if err := ghcli.Authenticated(ctx, e.GH); err != nil {
		checks = append(checks, check{"gh authenticated", false, err.Error(), "gh auth login"})
	} else {
		checks = append(checks, check{"gh authenticated", true, "", ""})
	}

	// 2. The Multica CLI config, where everything we do not ask for comes from.
	if s.CLIErr != nil {
		checks = append(checks, check{"multica config", false, s.CLIErr.Error(), "multica setup self-host"})
	} else {
		checks = append(checks, check{"multica config", true,
			fmt.Sprintf("%s, workspace %s", s.ServerURL, short(s.WorkspaceID)), ""})
	}

	// 3. Server reachable.
	if s.ServerURL == "" {
		checks = append(checks, check{"server responding", false, "no server_url", "multica setup self-host"})
	} else if err := ping(s.ServerURL); err != nil {
		checks = append(checks, check{"server responding", false, err.Error(),
			"cd " + s.ServerDir + " && docker compose -f docker-compose.selfhost.yml up -d"})
	} else {
		checks = append(checks, check{"server responding", true, s.ServerURL, ""})
	}

	// 4. Webhook secret. We need the value, not just to know it exists: it is
	// what we sign with.
	switch {
	case s.EnvErr != nil:
		checks = append(checks, check{"webhook secret", false, s.EnvErr.Error(),
			"set MULTICA_SERVER_DIR or MULTICA_SYNC_WEBHOOK_SECRET"})
	case s.WebhookSecret == "":
		checks = append(checks, check{"webhook secret", false,
			"GITHUB_WEBHOOK_SECRET empty in " + s.EnvPath, "gh multica-sync bootstrap --write-env"})
	default:
		checks = append(checks, check{"webhook secret", true, "read from " + s.EnvPath, ""})
	}

	// 5. Does the running server consider the integration configured? This is
	// the check that counts, because it answers for the live process rather
	// than for a file on disk that may have been edited without a restart.
	var bound bool
	if s.ServerURL != "" && s.Token != "" && s.WorkspaceID != "" {
		m := bootstrap.NewMultica(s.ServerURL, s.Token)
		resp, err := m.Installations(s.WorkspaceID)
		switch {
		case err != nil:
			checks = append(checks, check{"integration on the server", false, err.Error(), ""})
		case !resp.Configured:
			checks = append(checks, check{"integration on the server", false,
				"the running server does not see GITHUB_APP_SLUG and GITHUB_WEBHOOK_SECRET yet",
				"gh multica-sync bootstrap --write-env, then restart the backend"})
		default:
			checks = append(checks, check{"integration on the server", true, "", ""})
			bound = resp.Bound(s.InstallationID)
		}
	}

	// 6. Installation bound. This is what prevents the silent drop.
	switch {
	case s.InstallationID == 0:
		checks = append(checks, check{"installation bound", false, "no local installation created", "gh multica-sync bootstrap"})
	case !bound:
		checks = append(checks, check{"installation bound", false,
			fmt.Sprintf("id %d does not appear in the workspace", s.InstallationID), "gh multica-sync bootstrap"})
	default:
		checks = append(checks, check{"installation bound", true, fmt.Sprint(s.InstallationID), ""})
	}

	// 7. Enabled repositories.
	if len(s.Repos) == 0 {
		checks = append(checks, check{"enabled repositories", false, "none",
			"gh multica-sync enable (inside a checkout)"})
	} else {
		checks = append(checks, check{"enabled repositories", true, strings.Join(s.Repos, ", "), ""})
	}

	failed := 0
	for _, c := range checks {
		mark := "ok     "
		if !c.ok {
			mark = "missing"
			failed++
		}
		line := fmt.Sprintf("  [%s] %s", mark, c.name)
		if c.info != "" {
			line += ": " + c.info
		}
		fmt.Fprintln(e.Out, line)
		if !c.ok && c.fix != "" {
			fmt.Fprintf(e.Out, "            fix with: %s\n", c.fix)
		}
	}

	fmt.Fprintln(e.Out)
	if failed == 0 {
		fmt.Fprintln(e.Out, "All set. Polling can run.")
		return nil
	}
	return fmt.Errorf("%d check(s) pending", failed)
}

func ping(baseURL string) error {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(strings.TrimSuffix(baseURL, "/") + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d on /healthz", resp.StatusCode)
	}
	return nil
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
