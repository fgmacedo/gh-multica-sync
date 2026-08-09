// Package bootstrap binds a local installation to your workspace, which is the
// step that makes Multica accept our events.
//
// Multica attributes a pull_request event to a workspace by installation.id and
// silently drops anything it does not recognize. That installation normally
// comes from the GitHub App flow; here it comes from the same callback that
// flow uses, which accepts any numeric id as long as the state is signed by the
// server itself. Nothing is forged on GitHub's side: this is your own instance
// accepting to be configured by you.
package bootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Multica is a minimal client for the Multica API.
type Multica struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewMultica(baseURL, token string) *Multica {
	return &Multica{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Token:   token,
		// Redirects are not followed: the setup callback answers 302 and the
		// outcome, success or failure, is in the Location header, not the body.
		HTTP: &http.Client{
			Timeout:       20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (m *Multica) do(method, path string) (*http.Response, error) {
	req, err := http.NewRequest(method, m.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if m.Token != "" {
		req.Header.Set("Authorization", "Bearer "+m.Token)
	}
	req.Header.Set("User-Agent", "gh-multica-sync")
	return m.HTTP.Do(req)
}

// Installation is an installation already bound to the workspace.
type Installation struct {
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	AccountLogin   string `json:"account_login"`
}

// InstallationsResponse is the github/installations response.
type InstallationsResponse struct {
	CanManage     bool           `json:"can_manage"`
	Configured    bool           `json:"configured"`
	Installations []Installation `json:"installations"`
}

// Installations lists the installations bound to the workspace. `configured`
// reflects whether GITHUB_APP_SLUG and GITHUB_WEBHOOK_SECRET are set on the
// server, which makes this endpoint the most reliable check of both
// prerequisites: it answers for the process that is running, not for a file on
// disk that may not have been reloaded yet.
func (m *Multica) Installations(workspaceID string) (InstallationsResponse, error) {
	var out InstallationsResponse
	resp, err := m.do(http.MethodGet, "/api/workspaces/"+workspaceID+"/github/installations")
	if err != nil {
		return out, fmt.Errorf("listing installations: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("listing installations: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("parsing installations: %w", err)
	}
	return out, nil
}

// Bound reports whether an installation_id is already bound.
func (r InstallationsResponse) Bound(id int64) bool {
	for _, inst := range r.Installations {
		if inst.InstallationID == id {
			return true
		}
	}
	return false
}

// connectState asks the server for the install URL and extracts the signed
// state it carries. This is the only way to obtain a valid state without the
// server's signing key, and it is why bootstrap needs a token.
func (m *Multica) connectState(workspaceID string) (string, error) {
	resp, err := m.do(http.MethodGet, "/api/workspaces/"+workspaceID+"/github/connect")
	if err != nil {
		return "", fmt.Errorf("requesting state: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("requesting state: HTTP %d", resp.StatusCode)
	}
	var body struct {
		URL        string `json:"url"`
		Configured bool   `json:"configured"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parsing connect response: %w", err)
	}
	if !body.Configured {
		return "", fmt.Errorf("the server does not have GITHUB_APP_SLUG and GITHUB_WEBHOOK_SECRET set yet")
	}
	u, err := url.Parse(body.URL)
	if err != nil {
		return "", fmt.Errorf("unexpected install URL: %w", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		return "", fmt.Errorf("the server returned an install URL with no state")
	}
	return state, nil
}

// Bind attaches installationID to the workspace through the same callback
// GitHub would call after an App installation.
func (m *Multica) Bind(workspaceID string, installationID int64) error {
	state, err := m.connectState(workspaceID)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("installation_id", fmt.Sprint(installationID))
	q.Set("state", state)
	resp, err := m.do(http.MethodGet, "/api/github/setup?"+q.Encode())
	if err != nil {
		return fmt.Errorf("calling the setup callback: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	loc := resp.Header.Get("Location")
	switch {
	case strings.Contains(loc, "github_connected=1"):
		return nil
	case strings.Contains(loc, "github_error="):
		_, reason, _ := strings.Cut(loc, "github_error=")
		reason, _, _ = strings.Cut(reason, "&")
		return fmt.Errorf("the server refused the binding: %s", reason)
	default:
		return fmt.Errorf("unexpected response from the setup callback: HTTP %d", resp.StatusCode)
	}
}

// WorkspaceInfo is a workspace as the API reports it.
type WorkspaceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	IssuePrefix string `json:"issue_prefix"`
}

// Workspaces lists the workspaces the token can see.
//
// This comes from the API rather than from the multica CLI because the CLI's
// list omits the issue prefix, and the prefix is what tells a sweep which pull
// requests reference a card.
func (m *Multica) Workspaces() ([]WorkspaceInfo, error) {
	resp, err := m.do(http.MethodGet, "/api/workspaces")
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing workspaces: HTTP %d", resp.StatusCode)
	}
	var direct []WorkspaceInfo
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Workspaces []WorkspaceInfo `json:"workspaces"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("parsing workspaces: %w", err)
	}
	return wrapped.Workspaces, nil
}
