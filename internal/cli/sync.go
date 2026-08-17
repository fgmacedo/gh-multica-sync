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
	ws          config.Workspace
	cfg         config.Repo
	prefix      string
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
	prefixes, err := e.prefixes(ctx)
	if err != nil {
		return err
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

	sent := 0
	for _, ws := range workspaces {
		prefix := prefixes[ws.WorkspaceID]
		if prefix == "" {
			return fmt.Errorf("workspace %s has no issue prefix: no pull request can reference a card",
				short(ws.WorkspaceID))
		}
		sender, serr := e.newSender(s, ws)
		if serr != nil {
			return serr
		}
		rcfg, _ := ws.Repo(full)
		var snaps []payload.Snapshot
		if number > 0 {
			// A pull request named explicitly bypasses the scope filter.
			snap, ferr := e.Forge.PullRequest(ctx, owner, repo, number)
			if ferr != nil {
				return ferr
			}
			snaps = []payload.Snapshot{snap}
		} else if snaps, err = e.Forge.ListPullRequests(ctx, owner, repo, listLimit, authorFor(rcfg)); err != nil {
			return err
		}
		n, eerr := e.emit(ctx, ws, full, prefix, sender, st, snaps)
		sent += n
		if eerr != nil {
			return eerr
		}
	}

	if err := st.Save(); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s: %d event(s) sent across %d workspace(s).\n", full, sent, len(workspaces))
	return nil
}

func runPoll(ctx context.Context, e *Env) error {
	s := e.Settle()
	if s.EnabledRepos() == 0 {
		return nil
	}
	prefixes, err := e.prefixes(ctx)
	if err != nil {
		return err
	}
	st, err := state.Load(s.CurrentWorkspaceID)
	if err != nil {
		return err
	}

	total := 0
	var firstErr error
	var retry []target

	var targets []target
	for _, ws := range s.Workspaces {
		prefix := prefixes[ws.WorkspaceID]
		if prefix == "" {
			// Every pull request would be skipped anyway, and doing it quietly
			// is what made the last prefix problem take two days to surface.
			err := fmt.Errorf("workspace %s has no issue prefix: no pull request can reference a card",
				short(ws.WorkspaceID))
			fmt.Fprintf(e.Err, "warning: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range ws.Repos {
			owner, repo, perr := github.ParseRepo(r.Name)
			if perr != nil {
				fmt.Fprintf(e.Err, "warning: %v\n", perr)
				continue
			}
			targets = append(targets, target{owner: owner, repo: repo, ws: ws, cfg: r, prefix: prefix})
		}
	}

	// One sender per workspace, not per repository: building it confirms the
	// installation binding against the server, and repeating that call once per
	// enabled repository asks the same question several times per cycle.
	//
	// Only success is remembered. A failure is usually a server that is down for
	// a moment, and the burst retry forty-five seconds later exists precisely to
	// catch it back up.
	senders := map[string]*webhook.Client{}
	senderFor := func(ws config.Workspace) (*webhook.Client, error) {
		if c, ok := senders[ws.WorkspaceID]; ok {
			return c, nil
		}
		c, err := e.newSender(s, ws)
		if err != nil {
			return nil, err
		}
		senders[ws.WorkspaceID] = c
		return c, nil
	}

	sweep := func(t target) int {
		sender, serr := senderFor(t.ws)
		if serr != nil {
			fmt.Fprintf(e.Err, "warning: %s/%s: %v\n", t.owner, t.repo, serr)
			if firstErr == nil {
				firstErr = serr
			}
			return 0
		}
		snaps, lerr := e.Forge.ListPullRequests(ctx, t.owner, t.repo, listLimit, authorFor(t.cfg))
		if lerr != nil {
			// The timer runs unattended, so one unreachable repository must not
			// cost the others their transitions.
			fmt.Fprintf(e.Err, "warning: %s/%s: %v\n", t.owner, t.repo, lerr)
			if firstErr == nil {
				firstErr = lerr
			}
			return 0
		}
		n, eerr := e.emit(ctx, t.ws, t.owner+"/"+t.repo, t.prefix, sender, st, snaps)
		if eerr != nil {
			fmt.Fprintf(e.Err, "warning: %s/%s: %v\n", t.owner, t.repo, eerr)
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
		if n == 0 && st.RecentPush(t.owner+"/"+t.repo, burstWindow) {
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
		delete(st.Pushes, t.owner+"/"+t.repo)
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
	sent, skipped := 0, 0
	// Recorded even when a send fails midway, so status reflects the pass that
	// actually happened.
	defer func() { st.RecordSweep(ws.WorkspaceID, repo, len(snaps), skipped) }()
	for _, snap := range snaps {
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
