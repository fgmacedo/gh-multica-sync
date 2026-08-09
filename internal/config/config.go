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
// Each workspace gets its own installation id on purpose. Multica binds an
// installation to one or more workspaces and fans every event out to all of
// them, so a shared id would mirror personal pull requests into a work board
// and vice versa. Separate ids keep the boards separate.
type Workspace struct {
	WorkspaceID    string   `json:"workspace_id"`
	InstallationID int64    `json:"installation_id"`
	Repos          []string `json:"repos"`
}

// File is what we persist: only what no other source knows.
type File struct {
	Workspaces []Workspace `json:"workspaces"`

	// Legacy single-workspace fields, kept only so an existing install keeps
	// working. Load folds them into Workspaces and Save never writes them back.
	LegacyInstallationID int64    `json:"installation_id,omitempty"`
	LegacyRepos          []string `json:"repos,omitempty"`
}

// Settings is the resolved view, ready to use.
type Settings struct {
	ServerURL     string
	Token         string
	WebhookSecret string
	AppSlug       string

	// CurrentWorkspaceID is the workspace the Multica CLI is pointed at. It is
	// the default target for commands that take one.
	CurrentWorkspaceID string

	Workspaces []Workspace

	ServerDir  string
	EnvPath    string
	ConfigPath string

	// Discovery errors do not abort resolution: `doctor` has to report what is
	// missing rather than die on the first problem.
	CLIErr error
	EnvErr error
}

func Path() string { return filepath.Join(discover.StateDir(), "config.json") }

// Load reads our own config. A missing file is not an error: it means a fresh
// install. The legacy layout is folded in using fallbackWorkspace, which is the
// only workspace such an install could have been using.
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
		slices.Sort(f.Workspaces[i].Repos)
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

	// The environment has the final say, which is what allows running against a
	// server on another machine, or testing without touching any file.
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

// RepoWorkspaces returns every workspace a repository is enabled in. A
// repository may legitimately belong to more than one: they are separate
// boards with separate installations, so the events stay separate too.
func (s Settings) RepoWorkspaces(repo string) []Workspace {
	var out []Workspace
	for _, w := range s.Workspaces {
		if slices.ContainsFunc(w.Repos, func(r string) bool { return strings.EqualFold(r, repo) }) {
			out = append(out, w)
		}
	}
	return out
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

// ParseInstallationID exists so the environment override keeps working for
// tests and for unusual setups.
func ParseInstallationID(v string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n, err == nil
}
