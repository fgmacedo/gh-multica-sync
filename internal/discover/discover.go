// Package discover reads what already exists on the machine instead of asking
// for it again.
//
// The Multica CLI stores server_url, workspace_id and the token in
// ~/.multica/config.json, and the server stores the webhook secret in the .env
// of its install directory. Reusing those two sources is what keeps setup down
// to a couple of commands.
package discover

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MulticaCLI is the slice of ~/.multica/config.json we care about.
type MulticaCLI struct {
	ServerURL   string `json:"server_url"`
	AppURL      string `json:"app_url"`
	WorkspaceID string `json:"workspace_id"`
	Token       string `json:"token"`
	Path        string `json:"-"`
}

// CLIConfigPath returns the path to the Multica CLI config.
func CLIConfigPath() string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_CONFIG")); v != "" {
		return v
	}
	return filepath.Join(home(), ".multica", "config.json")
}

// LoadMulticaCLI reads the Multica CLI config.
func LoadMulticaCLI() (MulticaCLI, error) {
	path := CLIConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return MulticaCLI{Path: path}, fmt.Errorf("reading %s: %w", path, err)
	}
	var c MulticaCLI
	if err := json.Unmarshal(raw, &c); err != nil {
		return MulticaCLI{Path: path}, fmt.Errorf("parsing %s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

// ServerDir returns the server install directory.
func ServerDir() string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_SERVER_DIR")); v != "" {
		return v
	}
	return filepath.Join(home(), ".multica", "server")
}

// DotEnv reads a .env in docker compose format: one assignment per line,
// comments with #, optional quotes around the value. It does not expand
// variables: the goal is to read two keys, not to reimplement compose.
func DotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[key] = value
	}
	return out, sc.Err()
}

// ServerEnv reads the .env of the server installation.
func ServerEnv() (map[string]string, string, error) {
	path := filepath.Join(ServerDir(), ".env")
	env, err := DotEnv(path)
	return env, path, err
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// StateDir is where this tool keeps its config and state. It deliberately sits
// outside the server directory: the Multica installer runs `git reset --hard`
// there and would silently wipe anything of ours.
func StateDir() string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_SYNC_HOME")); v != "" {
		return v
	}
	return filepath.Join(home(), ".multica", "pr-sync")
}
