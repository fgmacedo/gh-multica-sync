package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/config"
	"github.com/fgmacedo/gh-multica-sync/internal/state"
)

func runEnable(ctx context.Context, e *Env, args []string) error {
	owner, repo, err := e.resolveRepo(ctx, args)
	if err != nil {
		return err
	}
	full := owner + "/" + repo

	f, err := config.Load()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(f.Repos, func(r string) bool { return strings.EqualFold(r, full) }) {
		fmt.Fprintf(e.Out, "%s was already enabled.\n", full)
		return nil
	}
	f.Repos = append(f.Repos, full)
	if err := config.Save(f); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s enabled.\n", full)
	if f.InstallationID == 0 {
		fmt.Fprintln(e.Out, "The installation is not bound yet: gh multica-sync bootstrap")
	}
	return nil
}

func runDisable(ctx context.Context, e *Env, args []string) error {
	owner, repo, err := e.resolveRepo(ctx, args)
	if err != nil {
		return err
	}
	full := owner + "/" + repo

	f, err := config.Load()
	if err != nil {
		return err
	}
	before := len(f.Repos)
	f.Repos = slices.DeleteFunc(f.Repos, func(r string) bool { return strings.EqualFold(r, full) })
	if len(f.Repos) == before {
		fmt.Fprintf(e.Out, "%s was not enabled.\n", full)
		return nil
	}
	if err := config.Save(f); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s disabled.\n", full)
	return nil
}

func runStatus(ctx context.Context, e *Env) error {
	s := e.Settle()

	fmt.Fprintf(e.Out, "server:        %s\n", orDash(s.ServerURL))
	fmt.Fprintf(e.Out, "workspace:     %s\n", orDash(s.WorkspaceID))
	fmt.Fprintf(e.Out, "installation:  %s\n", orDashInt(s.InstallationID))
	fmt.Fprintf(e.Out, "config:        %s\n", s.ConfigPath)

	if len(s.Repos) == 0 {
		fmt.Fprintln(e.Out, "\nno repositories enabled")
	} else {
		fmt.Fprintln(e.Out, "\nenabled repositories:")
		for _, r := range s.Repos {
			fmt.Fprintf(e.Out, "  %s\n", r)
		}
	}

	// Inside a checkout, whether THIS repository is enabled is the thing the
	// user came to find out.
	if owner, repo, err := e.Forge.CurrentRepo(ctx); err == nil {
		full := owner + "/" + repo
		mark := "not enabled"
		if s.RepoEnabled(full) {
			mark = "enabled"
		}
		fmt.Fprintf(e.Out, "\ncurrent repository: %s (%s)\n", full, mark)
	}

	st, err := state.Load()
	if err == nil && len(st.Entries) > 0 {
		fmt.Fprintf(e.Out, "\n%d mirrored pull request(s):\n", len(st.Entries))
		keys := make([]string, 0, len(st.Entries))
		for k := range st.Entries {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			en := st.Entries[k]
			fmt.Fprintf(e.Out, "  %-28s %-8s %s\n", k, strings.ToLower(en.Snapshot.State), en.SyncedAt.Format("2006-01-02 15:04"))
		}
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orDashInt(n int64) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprint(n)
}
