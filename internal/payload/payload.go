// Package payload builds the `pull_request` event body that Multica expects to
// receive from a GitHub App.
//
// The fields here mirror `ghPullRequestPayload` and what
// `mirrorPullRequestForWorkspace` actually reads
// (server/internal/handler/github.go, read at tag v0.4.21). Fields Multica does
// not read are left out on purpose: a smaller payload is easier to diff against
// upstream when they change it.
package payload

import "strings"

// PullRequestEvent is the webhook body.
type PullRequestEvent struct {
	Action       string       `json:"action"`
	Number       int          `json:"number"`
	PullRequest  PullRequest  `json:"pull_request"`
	Repository   Repository   `json:"repository"`
	Installation Installation `json:"installation"`
	Changes      *Changes     `json:"changes,omitempty"`
}

type Installation struct {
	ID int64 `json:"id"`
}

type Repository struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    User   `json:"owner"`
}

type User struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// Changes only shows up on `edited`. Multica reads `changes.base.ref` to learn
// whether the PR switched base and, with that, whether its mergeable state went
// stale.
type Changes struct {
	Base *struct {
		Ref *struct {
			From string `json:"from"`
		} `json:"ref,omitempty"`
	} `json:"base,omitempty"`
}

type PullRequest struct {
	Number         int32  `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state"` // open | closed
	Draft          bool   `json:"draft"`
	Merged         bool   `json:"merged"`
	HTMLURL        string `json:"html_url"`
	Head           Ref    `json:"head"`
	User           User   `json:"user"`
	MergedAt       string `json:"merged_at,omitempty"`
	ClosedAt       string `json:"closed_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	MergeableState string `json:"mergeable_state,omitempty"`
	Additions      int32  `json:"additions"`
	Deletions      int32  `json:"deletions"`
	ChangedFiles   int32  `json:"changed_files"`
}

// Snapshot is a normalized view of a pull request as the forge reports it. It
// is what local state persists and what action derivation compares.
type Snapshot struct {
	Owner          string
	Repo           string
	Number         int32
	Title          string
	Body           string
	State          string // OPEN | CLOSED | MERGED, as the forge reports it
	Draft          bool
	HTMLURL        string
	HeadRef        string
	HeadSHA        string
	AuthorLogin    string
	AuthorAvatar   string
	MergedAt       string
	ClosedAt       string
	CreatedAt      string
	UpdatedAt      string
	MergeStateHint string // CLEAN | DIRTY | BLOCKED | ..., as the forge reports it
	Additions      int32
	Deletions      int32
	ChangedFiles   int32
}

// Merged reports whether the pull request was merged.
func (s Snapshot) Merged() bool { return strings.EqualFold(s.State, "MERGED") }

// Open reports whether the pull request is still open.
func (s Snapshot) Open() bool { return strings.EqualFold(s.State, "OPEN") }

// webhookState maps the forge state onto the webhook vocabulary, which only
// knows open and closed: a merged pull request is closed, with merged=true
// alongside it.
func (s Snapshot) webhookState() string {
	if s.Open() {
		return "open"
	}
	return "closed"
}

// mergeableState translates the forge's merge state hint into the webhook's
// mergeable_state vocabulary. An unknown value maps to the empty string, which
// makes Multica treat it as "not reported" instead of storing garbage.
func mergeableState(hint string) string {
	switch strings.ToUpper(hint) {
	case "CLEAN":
		return "clean"
	case "DIRTY":
		return "dirty"
	case "BLOCKED":
		return "blocked"
	case "BEHIND":
		return "behind"
	case "UNSTABLE":
		return "unstable"
	case "HAS_HOOKS":
		return "has_hooks"
	case "DRAFT":
		return "draft"
	default:
		return ""
	}
}

// Build assembles the event from the current snapshot and an already derived
// action.
func Build(s Snapshot, action string, installationID int64) PullRequestEvent {
	return PullRequestEvent{
		Action: action,
		Number: int(s.Number),
		PullRequest: PullRequest{
			Number:         s.Number,
			Title:          s.Title,
			Body:           s.Body,
			State:          s.webhookState(),
			Draft:          s.Draft,
			Merged:         s.Merged(),
			HTMLURL:        s.HTMLURL,
			Head:           Ref{Ref: s.HeadRef, SHA: s.HeadSHA},
			User:           User{Login: s.AuthorLogin, AvatarURL: s.AuthorAvatar},
			MergedAt:       s.MergedAt,
			ClosedAt:       s.ClosedAt,
			CreatedAt:      s.CreatedAt,
			UpdatedAt:      s.UpdatedAt,
			MergeableState: mergeableState(s.MergeStateHint),
			Additions:      s.Additions,
			Deletions:      s.Deletions,
			ChangedFiles:   s.ChangedFiles,
		},
		Repository: Repository{
			Name:     s.Repo,
			FullName: s.Owner + "/" + s.Repo,
			Owner:    User{Login: s.Owner},
		},
		Installation: Installation{ID: installationID},
	}
}
