// Package config resolves runtime settings and persists what cannot be
// discovered.
//
// Precedence, uniform for every setting:
// environment variable > our own config > Multica CLI config > default.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/discover"
)

// Workspace is one Multica workspace we sync into, with the repositories
// enabled for it.
//
// Each workspace gets its own installation id: Multica fans every event out to
// all workspaces bound to an installation, so a shared id would mirror personal
// pull requests into a work board and vice versa.
//
// The issue prefix is deliberately absent. The server owns it, renaming it
// there is a click, and the copy that used to live here went stale once: the
// sweep went on matching a key nobody used and filtered every pull request out
// with no output at all, which reads as a repository where nothing changed. A
// sweep now asks the server, and refuses to run without an answer.
type Workspace struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID int64  `json:"installation_id"`
	Repos          []Repo `json:"repos"`
}

// Scope decides how much of a repository a sweep looks at.
const (
	// ScopeMine only considers pull requests authored by the authenticated
	// user. It is the default: on a shared monorepo it is the difference
	// between looking at a handful of pull requests and looking at hundreds.
	ScopeMine = "mine"
	// ScopeAll considers every recent pull request, for when teammates open
	// pull requests against your cards.
	ScopeAll = "all"
)

// Repo is an enabled repository plus how widely to look at it.
type Repo struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

// UnmarshalJSON accepts both the object form and the bare string used by
// earlier versions, so an existing config keeps loading.
func (r *Repo) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		r.Name, r.Scope = name, ""
		return nil
	}
	type plain Repo
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*r = Repo(p)
	return nil
}

// EffectiveScope resolves the default, ScopeMine.
func (r Repo) EffectiveScope() string {
	if r.Scope == ScopeAll {
		return ScopeAll
	}
	return ScopeMine
}

// File is what we persist: only what no other source knows.
type File struct {
	Workspaces []Workspace `json:"workspaces"`

	// Legacy single-workspace fields, kept only so an existing install keeps
	// working. Load folds them into Workspaces and Save never writes them back.
	LegacyInstallationID int64  `json:"installation_id,omitempty"`
	LegacyRepos          []Repo `json:"repos,omitempty"`
}

// Settings is the resolved view, ready to use.
type Settings struct {
	ServerURL     string
	Token         string
	WebhookSecret string
	AppSlug       string

	// CurrentWorkspaceID is the workspace the Multica CLI is pointed at, the
	// default target for commands that take one.
	CurrentWorkspaceID string

	Workspaces []Workspace

	ServerDir  string
	EnvPath    string
	ConfigPath string

	// Discovery errors do not abort resolution: `doctor` reports everything
	// that is missing rather than dying on the first problem.
	CLIErr error
	EnvErr error
}

func Path() string { return filepath.Join(discover.StateDir(), "config.json") }

// Load reads our own config. A missing file means a fresh install, not an
// error. The legacy single-workspace layout is folded into fallbackWorkspace,
// the only workspace such an install could have been using.
func Load(fallbackWorkspace string) (File, error) {
	raw, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return File{}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("reading %s: %w", Path(), err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", Path(), err)
	}
	if len(f.Workspaces) == 0 && (f.LegacyInstallationID != 0 || len(f.LegacyRepos) > 0) && fallbackWorkspace != "" {
		f.Workspaces = []Workspace{{
			WorkspaceID:    fallbackWorkspace,
			InstallationID: f.LegacyInstallationID,
			Repos:          f.LegacyRepos,
		}}
	}
	f.LegacyInstallationID, f.LegacyRepos = 0, nil
	return f, nil
}

// Save writes our own config with restrictive permissions.
func Save(f File) error {
	if err := os.MkdirAll(discover.StateDir(), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", discover.StateDir(), err)
	}
	f.LegacyInstallationID, f.LegacyRepos = 0, nil
	for i := range f.Workspaces {
		slices.SortFunc(f.Workspaces[i].Repos, func(a, b Repo) int {
			return strings.Compare(a.Name, b.Name)
		})
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), append(raw, '\n'), 0o600)
}

// Find returns the entry for a workspace, creating it in memory if absent.
func (f *File) Find(workspaceID string) *Workspace {
	for i := range f.Workspaces {
		if f.Workspaces[i].WorkspaceID == workspaceID {
			return &f.Workspaces[i]
		}
	}
	f.Workspaces = append(f.Workspaces, Workspace{WorkspaceID: workspaceID})
	return &f.Workspaces[len(f.Workspaces)-1]
}

// Resolve merges discovery, our own config and the environment.
func Resolve() Settings {
	s := Settings{
		ServerDir:  discover.ServerDir(),
		ConfigPath: Path(),
	}

	cli, err := discover.LoadMulticaCLI()
	s.CLIErr = err
	s.ServerURL = cli.ServerURL
	s.CurrentWorkspaceID = cli.WorkspaceID
	s.Token = cli.Token

	env, envPath, err := discover.ServerEnv()
	s.EnvErr = err
	s.EnvPath = envPath
	s.WebhookSecret = env["GITHUB_WEBHOOK_SECRET"]
	s.AppSlug = env["GITHUB_APP_SLUG"]

	if f, err := Load(s.CurrentWorkspaceID); err == nil {
		s.Workspaces = f.Workspaces
	}

	overrideStr(&s.ServerURL, "MULTICA_SYNC_SERVER_URL")
	overrideStr(&s.CurrentWorkspaceID, "MULTICA_SYNC_WORKSPACE_ID")
	overrideStr(&s.Token, "MULTICA_SYNC_TOKEN")
	overrideStr(&s.WebhookSecret, "MULTICA_SYNC_WEBHOOK_SECRET")
	return s
}

// WebhookEndpoint is the POST target, derived from the discovered server URL.
func (s Settings) WebhookEndpoint() string {
	return strings.TrimSuffix(s.ServerURL, "/") + "/api/webhooks/github"
}

// Workspace returns the settings for one workspace.
func (s Settings) Workspace(workspaceID string) (Workspace, bool) {
	for _, w := range s.Workspaces {
		if w.WorkspaceID == workspaceID {
			return w, true
		}
	}
	return Workspace{}, false
}

// RepoWorkspaces returns every workspace a repository is enabled in. More than
// one is legitimate: separate boards, separate installations.
func (s Settings) RepoWorkspaces(repo string) []Workspace {
	var out []Workspace
	for _, w := range s.Workspaces {
		if _, ok := w.Repo(repo); ok {
			out = append(out, w)
		}
	}
	return out
}

// Repo finds an enabled repository inside a workspace.
func (w Workspace) Repo(name string) (Repo, bool) {
	for _, r := range w.Repos {
		if strings.EqualFold(r.Name, name) {
			return r, true
		}
	}
	return Repo{}, false
}

// EnabledRepos reports how many repositories are enabled across all workspaces.
func (s Settings) EnabledRepos() int {
	n := 0
	for _, w := range s.Workspaces {
		n += len(w.Repos)
	}
	return n
}

func overrideStr(dst *string, envKey string) {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		*dst = v
	}
}

// ParseInstallationID parses an installation id coming from the environment.
func ParseInstallationID(v string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n, err == nil
}
