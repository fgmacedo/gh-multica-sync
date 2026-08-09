// Package github implements Forge on top of the gh CLI.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/ghcli"
	"github.com/fgmacedo/gh-multica-sync/internal/payload"
)

// prFields are the fields we ask gh for. Each one exists because
// mirrorPullRequestForWorkspace, on Multica's side, reads its counterpart in
// the payload: asking for less would leave a column empty on the card, asking
// for more would be weight with no destination.
const prFields = "number,title,body,state,isDraft,mergedAt,closedAt,createdAt,updatedAt,url," +
	"headRefName,headRefOid,author,additions,deletions,changedFiles,mergeStateStatus"

type ghPR struct {
	Number       int32  `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	State        string `json:"state"`
	IsDraft      bool   `json:"isDraft"`
	MergedAt     string `json:"mergedAt"`
	ClosedAt     string `json:"closedAt"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	URL          string `json:"url"`
	HeadRefName  string `json:"headRefName"`
	HeadRefOid   string `json:"headRefOid"`
	Additions    int32  `json:"additions"`
	Deletions    int32  `json:"deletions"`
	ChangedFiles int32  `json:"changedFiles"`
	MergeState   string `json:"mergeStateStatus"`
	Author       struct {
		Login string `json:"login"`
	} `json:"author"`
}

type Client struct{ gh ghcli.Runner }

func New(r ghcli.Runner) *Client { return &Client{gh: r} }

func (c *Client) Name() string { return "github" }

func (c *Client) ListPullRequests(ctx context.Context, owner, repo string, limit int) ([]payload.Snapshot, error) {
	out, err := c.gh.Run(ctx, "pr", "list",
		"--repo", owner+"/"+repo,
		"--state", "all",
		"--limit", fmt.Sprint(limit),
		"--json", prFields,
	)
	if err != nil {
		return nil, err
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}
	snaps := make([]payload.Snapshot, 0, len(prs))
	for _, pr := range prs {
		snaps = append(snaps, toSnapshot(owner, repo, pr))
	}
	return snaps, nil
}

func (c *Client) PullRequest(ctx context.Context, owner, repo string, number int32) (payload.Snapshot, error) {
	out, err := c.gh.Run(ctx, "pr", "view", fmt.Sprint(number),
		"--repo", owner+"/"+repo, "--json", prFields)
	if err != nil {
		return payload.Snapshot{}, err
	}
	var pr ghPR
	if err := json.Unmarshal(out, &pr); err != nil {
		return payload.Snapshot{}, fmt.Errorf("parsing gh output: %w", err)
	}
	return toSnapshot(owner, repo, pr), nil
}

// CurrentRepo resolves the repository from the current directory, which is
// what allows `gh multica-sync enable` with no argument inside a checkout.
func (c *Client) CurrentRepo(ctx context.Context) (string, string, error) {
	out, err := c.gh.Run(ctx, "repo", "view", "--json", "owner,name")
	if err != nil {
		return "", "", err
	}
	var v struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", "", fmt.Errorf("parsing gh output: %w", err)
	}
	if v.Owner.Login == "" || v.Name == "" {
		return "", "", fmt.Errorf("could not resolve the repository for this directory")
	}
	return v.Owner.Login, v.Name, nil
}

// AvatarURL fetches an author's avatar. `gh pr list` does not return this
// field, and it is purely cosmetic on the card, so callers treat a failure as
// absence rather than as an error.
func (c *Client) AvatarURL(ctx context.Context, login string) (string, error) {
	out, err := c.gh.Run(ctx, "api", "users/"+login, "--jq", ".avatar_url")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func toSnapshot(owner, repo string, pr ghPR) payload.Snapshot {
	return payload.Snapshot{
		Owner:          owner,
		Repo:           repo,
		Number:         pr.Number,
		Title:          pr.Title,
		Body:           pr.Body,
		State:          pr.State,
		Draft:          pr.IsDraft,
		HTMLURL:        pr.URL,
		HeadRef:        pr.HeadRefName,
		HeadSHA:        pr.HeadRefOid,
		AuthorLogin:    pr.Author.Login,
		MergedAt:       pr.MergedAt,
		ClosedAt:       pr.ClosedAt,
		CreatedAt:      pr.CreatedAt,
		UpdatedAt:      pr.UpdatedAt,
		MergeStateHint: pr.MergeState,
		Additions:      pr.Additions,
		Deletions:      pr.Deletions,
		ChangedFiles:   pr.ChangedFiles,
	}
}

// ParseRepo accepts "owner/repo" and returns its parts.
func ParseRepo(s string) (string, string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", fmt.Errorf("invalid repository %q, use the owner/repo form", s)
	}
	return owner, repo, nil
}
