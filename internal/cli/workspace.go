package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/config"
)

// resolveWorkspace turns a --workspace value (id, slug or prefix) into a
// workspace UUID, falling back to the one the Multica CLI points at.
//
// Accepting slug and prefix matters because that is how the Multica CLI itself
// lets you name a workspace, and a tool that only took UUIDs would make people
// go look them up.
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

type workspaceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	IssuePrefix string `json:"issue_prefix"`
}

// workspaces lists the workspaces through the Multica CLI, which already knows
// how to authenticate. Shelling out here keeps this tool from reimplementing
// the account model.
func (e *Env) workspaces(ctx context.Context) ([]workspaceInfo, error) {
	out, err := e.Multica.Run(ctx, "workspace", "list", "--output", "json")
	if err != nil {
		return nil, err
	}
	var list []workspaceInfo
	if err := json.Unmarshal(out, &list); err != nil {
		var wrapped struct {
			Workspaces []workspaceInfo `json:"workspaces"`
		}
		if jerr := json.Unmarshal(out, &wrapped); jerr != nil {
			return nil, fmt.Errorf("parsing workspace list: %w", err)
		}
		list = wrapped.Workspaces
	}
	return list, nil
}

// label renders a workspace for humans, preferring the prefix because that is
// what shows up in issue keys.
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
