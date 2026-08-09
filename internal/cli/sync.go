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
// Thirty comfortably covers the gap between two polls at any human pace, and
// keeps the call cheap.
const listLimit = 30

// burstWindow is how long a recent push keeps a repository under closer watch,
// when the optional hook is installed.
const burstWindow = 3 * time.Minute

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
	sender, err := e.newSender(s)
	if err != nil {
		return err
	}
	st, err := state.Load()
	if err != nil {
		return err
	}

	var snaps []payload.Snapshot
	if len(rest) > 0 {
		n, perr := strconv.ParseInt(rest[0], 10, 32)
		if perr != nil {
			return fmt.Errorf("invalid pull request number: %q", rest[0])
		}
		snap, ferr := e.Forge.PullRequest(ctx, owner, repo, int32(n))
		if ferr != nil {
			return ferr
		}
		snaps = []payload.Snapshot{snap}
	} else {
		if snaps, err = e.Forge.ListPullRequests(ctx, owner, repo, listLimit); err != nil {
			return err
		}
	}

	sent, err := e.emit(ctx, s, sender, st, snaps)
	if err != nil {
		return err
	}
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "%s/%s: %d event(s) sent, %d pull request(s) inspected.\n", owner, repo, sent, len(snaps))
	return nil
}

func runPoll(ctx context.Context, e *Env) error {
	s := e.Settle()
	// With no repository enabled there is nothing to do, and this is how the
	// timer costs practically nothing while the tool sits idle.
	if len(s.Repos) == 0 {
		return nil
	}
	sender, err := e.newSender(s)
	if err != nil {
		return err
	}
	st, err := state.Load()
	if err != nil {
		return err
	}

	total := 0
	var firstErr error
	var retry []string

	sweep := func(full string) int {
		owner, repo, perr := github.ParseRepo(full)
		if perr != nil {
			fmt.Fprintf(e.Err, "warning: %v\n", perr)
			return 0
		}
		snaps, lerr := e.Forge.ListPullRequests(ctx, owner, repo, listLimit)
		if lerr != nil {
			// One unreachable repository must not stop the others: the timer
			// runs unattended, and failing everything because of one would
			// mean losing the transitions of the rest.
			fmt.Fprintf(e.Err, "warning: %s: %v\n", full, lerr)
			if firstErr == nil {
				firstErr = lerr
			}
			return 0
		}
		n, serr := e.emit(ctx, s, sender, st, snaps)
		if serr != nil {
			fmt.Fprintf(e.Err, "warning: %s: %v\n", full, serr)
			if firstErr == nil {
				firstErr = serr
			}
		}
		return n
	}

	for _, full := range s.Repos {
		n := sweep(full)
		total += n
		// There was a recent push here and we have seen nothing new yet: the
		// pull request may be opening right now. Worth a second look before the
		// next cycle, which is five minutes away.
		if n == 0 && st.RecentPush(full, burstWindow) {
			retry = append(retry, full)
		}
	}

	for _, full := range retry {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(45 * time.Second):
		}
		total += sweep(full)
		delete(st.Pushes, full)
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
func (e *Env) emit(ctx context.Context, s config.Settings, sender *webhook.Client, st *state.Store, snaps []payload.Snapshot) (int, error) {
	sent := 0
	for _, snap := range snaps {
		action, changed := payload.DeriveAction(st.Previous(snap.Owner, snap.Repo, snap.Number), snap)
		if !changed {
			continue
		}
		snap.AuthorAvatar = e.avatar(ctx, st, snap.AuthorLogin)

		ev := payload.Build(snap, action, s.InstallationID)
		if err := sender.Send("pull_request", ev); err != nil {
			return sent, err
		}
		st.Record(snap, action)
		sent++
		fmt.Fprintf(e.Out, "%s/%s#%d %s\n", snap.Owner, snap.Repo, snap.Number, action)
	}
	return sent, nil
}

// avatar resolves the author's avatar URL, with a cache. `gh pr list` does not
// return this field and it is only cosmetic on the card, so a failure becomes
// absence.
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
// Checking the installation is the crux of this tool: the endpoint returns 200
// even when it drops the event for an unrecognized installation, so confirming
// the binding BEFORE is what turns a silent failure into an error message.
func (e *Env) newSender(s config.Settings) (*webhook.Client, error) {
	if s.WebhookSecret == "" {
		return nil, fmt.Errorf("no GITHUB_WEBHOOK_SECRET: run 'gh multica-sync doctor'")
	}
	if s.InstallationID == 0 {
		return nil, fmt.Errorf("no installation bound: run 'gh multica-sync bootstrap'")
	}
	if s.Token != "" && s.WorkspaceID != "" {
		resp, err := bootstrap.NewMultica(s.ServerURL, s.Token).Installations(s.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if !resp.Bound(s.InstallationID) {
			return nil, fmt.Errorf(
				"installation %d is no longer bound to the workspace: events would be dropped silently.\n"+
					"  Run 'gh multica-sync bootstrap' to bind it again", s.InstallationID)
		}
	}
	return webhook.New(s.WebhookEndpoint(), s.WebhookSecret), nil
}
