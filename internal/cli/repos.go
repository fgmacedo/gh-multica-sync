package cli

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/config"
	"github.com/fgmacedo/gh-multica-sync/internal/state"
)

// repoFlags parses the shared --workspace flag and returns the remaining
// positional arguments.
func repoFlags(e *Env, name string, args []string) (string, string, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.Err)
	ws := fs.String("workspace", "", "target workspace (id, slug or issue prefix)")
	scope := fs.String("scope", config.ScopeMine,
		"how much of the repository to sweep: mine (your pull requests) or all")
	if err := fs.Parse(args); err != nil {
		return "", "", nil, err
	}
	if *scope != config.ScopeMine && *scope != config.ScopeAll {
		return "", "", nil, fmt.Errorf("invalid --scope %q, use %q or %q", *scope, config.ScopeMine, config.ScopeAll)
	}
	return *ws, *scope, fs.Args(), nil
}

func runEnable(ctx context.Context, e *Env, args []string) error {
	wsFlag, scope, rest, err := repoFlags(e, "enable", args)
	if err != nil {
		return err
	}
	owner, repo, err := e.resolveRepo(ctx, rest)
	if err != nil {
		return err
	}
	full := owner + "/" + repo

	s := e.Settle()
	wsID, err := e.resolveWorkspace(ctx, s, wsFlag)
	if err != nil {
		return err
	}

	f, err := config.Load(s.CurrentWorkspaceID)
	if err != nil {
		return err
	}
	w := f.Find(wsID)
	// Caching the prefix here is what lets a sweep know what a card key looks
	// like without asking the server every cycle.
	if list, lerr := e.workspaces(ctx); lerr == nil {
		for _, info := range list {
			if info.ID == wsID {
				w.IssuePrefix = info.IssuePrefix
			}
		}
	}
	if existing, ok := w.Repo(full); ok && existing.Scope == scope {
		fmt.Fprintf(e.Out, "%s was already enabled in this workspace.\n", full)
		return config.Save(f)
	}
	w.Repos = slices.DeleteFunc(w.Repos, func(r config.Repo) bool { return strings.EqualFold(r.Name, full) })
	w.Repos = append(w.Repos, config.Repo{Name: full, Scope: scope})
	if err := config.Save(f); err != nil {
		return err
	}

	scopeNote := "your pull requests only"
	if scope == config.ScopeAll {
		scopeNote = "every recent pull request"
	}
	fmt.Fprintf(e.Out, "%s enabled in workspace %s (%s).\n", full, e.workspaceLabel(ctx, wsID), scopeNote)
	if w.IssuePrefix != "" {
		fmt.Fprintf(e.Out, "Only pull requests referencing %s-<n> are mirrored.\n", w.IssuePrefix)
	}
	if w.InstallationID == 0 {
		fmt.Fprintf(e.Out, "This workspace has no installation yet: gh multica-sync bootstrap --workspace %s\n", wsID)
	}
	return nil
}

func runDisable(ctx context.Context, e *Env, args []string) error {
	wsFlag, _, rest, err := repoFlags(e, "disable", args)
	if err != nil {
		return err
	}
	owner, repo, err := e.resolveRepo(ctx, rest)
	if err != nil {
		return err
	}
	full := owner + "/" + repo

	s := e.Settle()
	wsID, err := e.resolveWorkspace(ctx, s, wsFlag)
	if err != nil {
		return err
	}

	f, err := config.Load(s.CurrentWorkspaceID)
	if err != nil {
		return err
	}
	w := f.Find(wsID)
	before := len(w.Repos)
	w.Repos = slices.DeleteFunc(w.Repos, func(r config.Repo) bool { return strings.EqualFold(r.Name, full) })
	if len(w.Repos) == before {
		fmt.Fprintf(e.Out, "%s was not enabled in this workspace.\n", full)
		return nil
	}
	if err := config.Save(f); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s disabled in workspace %s.\n", full, e.workspaceLabel(ctx, wsID))
	return nil
}

func runStatus(ctx context.Context, e *Env) error {
	s := e.Settle()
	list, _ := e.workspaces(ctx)

	fmt.Fprintf(e.Out, "server:   %s\n", orDash(s.ServerURL))
	fmt.Fprintf(e.Out, "config:   %s\n", s.ConfigPath)

	if len(s.Workspaces) == 0 {
		fmt.Fprintln(e.Out, "\nno workspace configured yet: gh multica-sync bootstrap")
	}
	for _, w := range s.Workspaces {
		fmt.Fprintf(e.Out, "\nworkspace %s\n", label(list, w.WorkspaceID))
		fmt.Fprintf(e.Out, "  installation: %s\n", orDashInt(w.InstallationID))
		if len(w.Repos) == 0 {
			fmt.Fprintln(e.Out, "  repositories: none")
			continue
		}
		fmt.Fprintf(e.Out, "  issue prefix: %s\n", orDash(w.IssuePrefix))
		fmt.Fprintln(e.Out, "  repositories:")
		for _, r := range w.Repos {
			fmt.Fprintf(e.Out, "    %-42s scope: %s\n", r.Name, r.EffectiveScope())
		}
	}

	// Inside a checkout, whether THIS repository is enabled is the thing the
	// user came to find out.
	if owner, repo, err := e.Forge.CurrentRepo(ctx); err == nil {
		full := owner + "/" + repo
		in := s.RepoWorkspaces(full)
		if len(in) == 0 {
			fmt.Fprintf(e.Out, "\ncurrent repository: %s (not enabled)\n", full)
		} else {
			names := make([]string, 0, len(in))
			for _, w := range in {
				names = append(names, label(list, w.WorkspaceID))
			}
			fmt.Fprintf(e.Out, "\ncurrent repository: %s (enabled in %s)\n", full, strings.Join(names, ", "))
		}
	}

	st, err := state.Load(s.CurrentWorkspaceID)
	if err == nil && len(st.Entries) > 0 {
		fmt.Fprintf(e.Out, "\n%d mirrored pull request(s):\n", len(st.Entries))
		keys := make([]string, 0, len(st.Entries))
		for k := range st.Entries {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			en := st.Entries[k]
			// Keys carry the workspace UUID, which is noise on screen: show the
			// workspace by name and the pull request by its usual form.
			wsID, pr, _ := strings.Cut(k, "|")
			fmt.Fprintf(e.Out, "  %-14s %-30s %-8s %s\n",
				label(list, wsID), pr, strings.ToLower(en.Snapshot.State), en.SyncedAt.Format("2006-01-02 15:04"))
		}
	}
	return nil
}

// workspaceLabel is the forgiving variant used in messages: it never fails, it
// just falls back to the id.
func (e *Env) workspaceLabel(ctx context.Context, id string) string {
	list, err := e.workspaces(ctx)
	if err != nil {
		return short(id)
	}
	return label(list, id)
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
