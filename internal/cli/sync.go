package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/fgmacedo/gh-multica-sync/internal/bootstrap"
	"github.com/fgmacedo/gh-multica-sync/internal/config"
	"github.com/fgmacedo/gh-multica-sync/internal/forge/github"
	"github.com/fgmacedo/gh-multica-sync/internal/payload"
	"github.com/fgmacedo/gh-multica-sync/internal/state"
	"github.com/fgmacedo/gh-multica-sync/internal/webhook"
)

// listLimit is how many pull requests we look at per repository each cycle.
// Thirty covers the gap between two polls at any human pace.
const listLimit = 30

// burstWindow is how long a recent push keeps a repository under closer watch,
// when the optional hook is installed.
const burstWindow = 3 * time.Minute

// target is one repository paired with the workspace it feeds, since the same
// repository may be enabled in more than one board.
type target struct {
	owner, repo string
	full        string // owner/repo as the config spells it
	ws          config.Workspace
	cfg         config.Repo
	prefix      string
	sender      *webhook.Client
}

func authorFor(r config.Repo) string {
	if r.EffectiveScope() == config.ScopeAll {
		return ""
	}
	return "@me"
}

func runSync(ctx context.Context, e *Env, args []string) error {
	owner, repo, err := e.resolveRepo(ctx, args)
	if err != nil {
		return err
	}
	rest := args
	if len(rest) > 0 {
		if _, _, perr := github.ParseRepo(rest[0]); perr == nil {
			rest = rest[1:]
		}
	}

	s := e.Settle()
	full := owner + "/" + repo
	workspaces := s.RepoWorkspaces(full)
	if len(workspaces) == 0 {
		return fmt.Errorf("%s is not enabled in any workspace: run 'gh multica-sync enable'", full)
	}
	list, err := e.workspaces(ctx)
	if err != nil {
		return fmt.Errorf("resolving issue prefixes: %w", err)
	}

	st, err := state.Load(s.CurrentWorkspaceID)
	if err != nil {
		return err
	}

	var number int32
	if len(rest) > 0 {
		n, perr := strconv.ParseInt(rest[0], 10, 32)
		if perr != nil {
			return fmt.Errorf("invalid pull request number: %q", rest[0])
		}
		number = int32(n)
	}

	syncOne := func(ws config.Workspace) (int, error) {
		prefix := serverPrefix(list, ws.WorkspaceID)
		sender, rerr := e.ready(s, ws, prefix)
		if rerr != nil {
			return 0, rerr
		}
		rcfg, _ := ws.Repo(full)
		var snaps []payload.Snapshot
		if number > 0 {
			// A pull request named explicitly bypasses the scope filter.
			snap, ferr := e.Forge.PullRequest(ctx, owner, repo, number)
			if ferr != nil {
				return 0, ferr
			}
			snaps = []payload.Snapshot{snap}
		} else {
			var lerr error
			if snaps, lerr = e.Forge.ListPullRequests(ctx, owner, repo, listLimit, authorFor(rcfg)); lerr != nil {
				return 0, lerr
			}
		}
		return e.emit(ctx, ws, full, prefix, sender, st, snaps)
	}

	// One failing workspace does not cancel the others, the same way the poll
	// treats one failing repository: a repository enabled in two boards would
	// otherwise lose the healthy board to the broken one.
	sent := 0
	var firstErr error
	for _, ws := range workspaces {
		n, werr := syncOne(ws)
		sent += n
		if werr != nil {
			fmt.Fprintf(e.Err, "warning: workspace %s: %v\n", short(ws.WorkspaceID), werr)
			if firstErr == nil {
				firstErr = werr
			}
		}
	}

	if err := st.Save(); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s: %d event(s) sent across %d workspace(s).\n", full, sent, len(workspaces))
	return firstErr
}

func runPoll(ctx context.Context, e *Env) error {
	s := e.Settle()
	if s.EnabledRepos() == 0 {
		return nil
	}
	list, err := e.workspaces(ctx)
	if err != nil {
		return fmt.Errorf("resolving issue prefixes: %w", err)
	}
	st, err := state.Load(s.CurrentWorkspaceID)
	if err != nil {
		return err
	}

	total := 0
	var firstErr error
	var retry []target

	// Prefix and sender are decided once per workspace, and both are why a
	// workspace can be swept at all: building the sender confirms the
	// installation binding, which is a question worth asking once a cycle
	// rather than once per enabled repository.
	var targets []target
	for _, ws := range s.Workspaces {
		prefix := serverPrefix(list, ws.WorkspaceID)
		sender, werr := e.ready(s, ws, prefix)
		if werr != nil {
			fmt.Fprintf(e.Err, "warning: %v\n", werr)
			if firstErr == nil {
				firstErr = werr
			}
			continue
		}
		for _, r := range ws.Repos {
			owner, repo, perr := github.ParseRepo(r.Name)
			if perr != nil {
				fmt.Fprintf(e.Err, "warning: %v\n", perr)
				continue
			}
			targets = append(targets, target{
				owner: owner, repo: repo, full: r.Name,
				ws: ws, cfg: r, prefix: prefix, sender: sender,
			})
		}
	}

	sweep := func(t target) int {
		snaps, lerr := e.Forge.ListPullRequests(ctx, t.owner, t.repo, listLimit, authorFor(t.cfg))
		if lerr != nil {
			// The timer runs unattended, so one unreachable repository must not
			// cost the others their transitions.
			fmt.Fprintf(e.Err, "warning: %s: %v\n", t.full, lerr)
			if firstErr == nil {
				firstErr = lerr
			}
			return 0
		}
		n, eerr := e.emit(ctx, t.ws, t.full, t.prefix, t.sender, st, snaps)
		if eerr != nil {
			fmt.Fprintf(e.Err, "warning: %s: %v\n", t.full, eerr)
			if firstErr == nil {
				firstErr = eerr
			}
		}
		return n
	}

	for _, t := range targets {
		n := sweep(t)
		total += n
		// A recent push with nothing new yet: the pull request may be opening
		// right now, and the next cycle is five minutes away.
		if n == 0 && st.RecentPush(t.full, burstWindow) {
			retry = append(retry, t)
		}
	}

	for _, t := range retry {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(45 * time.Second):
		}
		total += sweep(t)
		delete(st.Pushes, t.full)
	}

	if err := st.Save(); err != nil {
		return err
	}
	if total > 0 {
		fmt.Fprintf(e.Out, "%d event(s) sent.\n", total)
	}
	return firstErr
}

// emit compares each snapshot against what we already knew and sends only what
// changed.
func (e *Env) emit(ctx context.Context, ws config.Workspace, repo, prefix string, sender *webhook.Client, st *state.Store, snaps []payload.Snapshot) (int, error) {
	sent, examined, skipped := 0, 0, 0
	// Counted as we go, not from len(snaps): a send that fails midway leaves the
	// rest of the list unexamined.
	defer func() { st.RecordSweep(ws.WorkspaceID, repo, examined, skipped) }()
	for _, snap := range snaps {
		examined++
		if !payload.MentionsIssue(snap, prefix) {
			skipped++
			continue
		}
		action, changed := payload.DeriveAction(st.Previous(ws.WorkspaceID, snap), snap)
		if !changed {
			continue
		}
		snap.AuthorAvatar = e.avatar(ctx, st, snap.AuthorLogin)

		ev := payload.Build(snap, action, ws.InstallationID)
		if err := sender.Send("pull_request", ev); err != nil {
			return sent, err
		}
		st.Record(ws.WorkspaceID, snap, action)
		sent++
		fmt.Fprintf(e.Out, "%s/%s#%d %s\n", snap.Owner, snap.Repo, snap.Number, action)
	}
	return sent, nil
}

// avatar resolves the author's avatar URL, cached so Forge.AvatarURL is called
// once per author. It is cosmetic on the card, so a failure becomes absence.
func (e *Env) avatar(ctx context.Context, st *state.Store, login string) string {
	if login == "" {
		return ""
	}
	if url, ok := st.Avatars[login]; ok {
		return url
	}
	url, err := e.Forge.AvatarURL(ctx, login)
	if err != nil {
		return ""
	}
	st.Avatars[login] = url
	return url
}

// newSender validates the prerequisites before anything is sent.
//
// The endpoint returns 200 even when it drops the event for an unrecognized
// installation, so confirming the binding beforehand is what turns a silent
// failure into an error message.
func (e *Env) newSender(s config.Settings, ws config.Workspace) (*webhook.Client, error) {
	if s.WebhookSecret == "" {
		return nil, fmt.Errorf("no GITHUB_WEBHOOK_SECRET: run 'gh multica-sync doctor'")
	}
	if ws.InstallationID == 0 {
		return nil, fmt.Errorf("workspace %s has no installation bound: run 'gh multica-sync bootstrap --workspace %s'",
			short(ws.WorkspaceID), ws.WorkspaceID)
	}
	if s.Token != "" {
		resp, err := bootstrap.NewMultica(s.ServerURL, s.Token).Installations(ws.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if !resp.Bound(ws.InstallationID) {
			return nil, fmt.Errorf(
				"installation %d is no longer bound to workspace %s: events would be dropped silently.\n"+
					"  Run 'gh multica-sync bootstrap --workspace %s' to bind it again",
				ws.InstallationID, short(ws.WorkspaceID), ws.WorkspaceID)
		}
	}
	return webhook.New(s.WebhookEndpoint(), s.WebhookSecret), nil
}
