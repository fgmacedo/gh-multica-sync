// Package forge abstracts where pull requests come from.
//
// Today only GitHub exists, and the installation this tool fabricates is
// specific to it. For GitLab, Gitea and Forgejo, Multica already ships a
// native, token-based integration, so future support there would forward
// rather than fabricate: same interface here, a different deliverer on the
// other side. The seam exists so that difference fits without a rewrite.
package forge

import (
	"context"

	"github.com/fgmacedo/gh-multica-sync/internal/payload"
)

// Forge discovers pull requests for a repository.
type Forge interface {
	// Name identifies the forge in user-facing messages.
	Name() string
	// ListPullRequests returns the most recent pull requests, open or not.
	ListPullRequests(ctx context.Context, owner, repo string, limit int) ([]payload.Snapshot, error)
	// PullRequest returns a single pull request.
	PullRequest(ctx context.Context, owner, repo string, number int32) (payload.Snapshot, error)
	// CurrentRepo resolves the repository of the current working directory.
	CurrentRepo(ctx context.Context) (owner string, repo string, err error)
}
