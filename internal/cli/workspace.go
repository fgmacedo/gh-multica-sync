package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/bootstrap"
	"github.com/fgmacedo/gh-multica-sync/internal/config"
)

// resolveWorkspace turns a --workspace value into a workspace UUID, falling
// back to the one the Multica CLI points at. Slug and prefix are accepted
// because that is how the Multica CLI itself lets you name a workspace.
func (e *Env) resolveWorkspace(ctx context.Context, s config.Settings, want string) (string, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		if s.CurrentWorkspaceID == "" {
			return "", fmt.Errorf("no workspace: run 'multica setup self-host' or pass --workspace")
		}
		return s.CurrentWorkspaceID, nil
	}

	list, err := e.workspaces(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range list {
		if strings.EqualFold(w.ID, want) || strings.EqualFold(w.Slug, want) ||
			strings.EqualFold(w.IssuePrefix, want) || strings.HasPrefix(w.ID, want) {
			return w.ID, nil
		}
	}
	return "", fmt.Errorf("workspace %q not found", want)
}

type workspaceInfo = bootstrap.WorkspaceInfo

// workspaces lists the workspaces through the Multica API, using the token the
// Multica CLI already stored.
func (e *Env) workspaces(ctx context.Context) ([]workspaceInfo, error) {
	s := e.Settle()
	if s.ServerURL == "" || s.Token == "" {
		return nil, fmt.Errorf("no Multica server or token configured")
	}
	return bootstrap.NewMultica(s.ServerURL, s.Token).Workspaces()
}

// label renders a workspace for humans, showing the prefix because that is what
// shows up in issue keys.
func label(list []workspaceInfo, id string) string {
	for _, w := range list {
		if w.ID == id {
			if w.IssuePrefix != "" {
				return fmt.Sprintf("%s (%s)", w.Name, w.IssuePrefix)
			}
			return w.Name
		}
	}
	return short(id)
}
