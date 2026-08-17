package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fgmacedo/gh-multica-sync/internal/bootstrap"
	"github.com/fgmacedo/gh-multica-sync/internal/config"
	"github.com/fgmacedo/gh-multica-sync/internal/ghcli"
)

type check struct {
	name string
	ok   bool
	info string
	fix  string
}

// runDoctor inspects every prerequisite and, for whatever is missing, prints
// the command that fixes it. It changes nothing.
func runDoctor(ctx context.Context, e *Env) error {
	s := e.Settle()
	checks := []check{}

	if err := ghcli.Authenticated(ctx, e.GH); err != nil {
		checks = append(checks, check{"gh authenticated", false, err.Error(), "gh auth login"})
	} else {
		checks = append(checks, check{"gh authenticated", true, "", ""})
	}

	// The Multica CLI config, where everything we do not ask for comes from.
	if s.CLIErr != nil {
		checks = append(checks, check{"multica config", false, s.CLIErr.Error(), "multica setup self-host"})
	} else {
		checks = append(checks, check{"multica config", true, s.ServerURL, ""})
	}

	if s.ServerURL == "" {
		checks = append(checks, check{"server responding", false, "no server_url", "multica setup self-host"})
	} else if err := ping(s.ServerURL); err != nil {
		checks = append(checks, check{"server responding", false, err.Error(),
			"cd " + s.ServerDir + " && docker compose -f docker-compose.selfhost.yml up -d"})
	} else {
		checks = append(checks, check{"server responding", true, s.ServerURL, ""})
	}

	// The webhook secret has to be read, not just found: it is what we sign with.
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

	// Per workspace, asked of the running server rather than of the config on
	// disk: see Multica.Installations.
	list, listErr := e.workspaces(ctx)
	if listErr != nil && s.ServerURL != "" && s.Token != "" {
		// Without the list there is no prefix, and reporting that as "no issue
		// prefix" would send you to Multica to set one that is already there.
		checks = append(checks, check{"workspace list", false, listErr.Error(), ""})
	}
	if len(s.Workspaces) == 0 {
		checks = append(checks, check{"workspaces configured", false, "none",
			"gh multica-sync bootstrap"})
	}
	for _, w := range s.Workspaces {
		name := label(list, w.WorkspaceID)
		if s.ServerURL == "" || s.Token == "" {
			continue
		}
		resp, err := bootstrap.NewMultica(s.ServerURL, s.Token).Installations(w.WorkspaceID)
		switch {
		case err != nil:
			checks = append(checks, check{"workspace " + name, false, err.Error(), ""})
			continue
		case !resp.Configured:
			checks = append(checks, check{"workspace " + name, false,
				"the running server does not see GITHUB_APP_SLUG and GITHUB_WEBHOOK_SECRET yet",
				"gh multica-sync bootstrap --write-env, then restart the backend"})
			continue
		case w.InstallationID == 0:
			checks = append(checks, check{"workspace " + name, false, "no installation bound",
				"gh multica-sync bootstrap --workspace " + w.WorkspaceID})
			continue
		case !resp.Bound(w.InstallationID):
			checks = append(checks, check{"workspace " + name, false,
				fmt.Sprintf("installation %d does not appear in this workspace", w.InstallationID),
				"gh multica-sync bootstrap --workspace " + w.WorkspaceID})
			continue
		}
		prefix := serverPrefix(list, w.WorkspaceID)
		if prefix == "" && listErr == nil {
			checks = append(checks, check{"workspace " + name, false, errNoPrefix.Error(),
				"set an issue prefix for this workspace in Multica"})
			continue
		}
		info := fmt.Sprintf("installation %d, %s-<n>, %s", w.InstallationID, prefix, reposLabel(w.Repos))
		checks = append(checks, check{"workspace " + name, true, info, ""})
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
		if s.EnabledRepos() == 0 {
			fmt.Fprintln(e.Out, "Nothing enabled yet: run 'gh multica-sync enable' inside a checkout.")
			return nil
		}
		fmt.Fprintln(e.Out, "All set. Polling can run.")
		return nil
	}
	return fmt.Errorf("%d check(s) pending", failed)
}

func reposLabel(repos []config.Repo) string {
	if len(repos) == 0 {
		return "no repositories enabled"
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		names = append(names, fmt.Sprintf("%s (%s)", r.Name, r.EffectiveScope()))
	}
	return strings.Join(names, ", ")
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
