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

// File is what we persist: only what no other source knows.
type File struct {
	InstallationID int64    `json:"installation_id"`
	Repos          []string `json:"repos"`
}

// Settings is the resolved view, ready to use.
type Settings struct {
	ServerURL      string
	WorkspaceID    string
	Token          string
	WebhookSecret  string
	AppSlug        string
	InstallationID int64
	Repos          []string

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
// install.
func Load() (File, error) {
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
	return f, nil
}

// Save writes our own config with restrictive permissions.
func Save(f File) error {
	if err := os.MkdirAll(discover.StateDir(), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", discover.StateDir(), err)
	}
	slices.Sort(f.Repos)
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), append(raw, '\n'), 0o600)
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
	s.WorkspaceID = cli.WorkspaceID
	s.Token = cli.Token

	env, envPath, err := discover.ServerEnv()
	s.EnvErr = err
	s.EnvPath = envPath
	s.WebhookSecret = env["GITHUB_WEBHOOK_SECRET"]
	s.AppSlug = env["GITHUB_APP_SLUG"]

	if f, err := Load(); err == nil {
		s.InstallationID = f.InstallationID
		s.Repos = f.Repos
	}

	// The environment has the final say, which is what allows running against a
	// server on another machine, or testing without touching any file.
	overrideStr(&s.ServerURL, "MULTICA_SYNC_SERVER_URL")
	overrideStr(&s.WorkspaceID, "MULTICA_SYNC_WORKSPACE_ID")
	overrideStr(&s.Token, "MULTICA_SYNC_TOKEN")
	overrideStr(&s.WebhookSecret, "MULTICA_SYNC_WEBHOOK_SECRET")
	if v := strings.TrimSpace(os.Getenv("MULTICA_SYNC_INSTALLATION_ID")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.InstallationID = n
		}
	}
	return s
}

// WebhookEndpoint is the POST target, derived from the discovered server URL.
func (s Settings) WebhookEndpoint() string {
	return strings.TrimSuffix(s.ServerURL, "/") + "/api/webhooks/github"
}

// RepoEnabled reports whether a repository is enabled. A repository outside the
// list is never touched, which is the tool's central guarantee.
func (s Settings) RepoEnabled(repo string) bool {
	return slices.ContainsFunc(s.Repos, func(r string) bool {
		return strings.EqualFold(r, repo)
	})
}

func overrideStr(dst *string, envKey string) {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		*dst = v
	}
}
