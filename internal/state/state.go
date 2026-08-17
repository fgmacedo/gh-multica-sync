// Package state records what we have already seen of each pull request, so a
// poll only emits an event when something actually changed.
//
// Without it every cycle would resend every pull request. Multica would cope,
// since the upsert is idempotent, but the cost would scale with history
// instead of with change.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fgmacedo/gh-multica-sync/internal/discover"
	"github.com/fgmacedo/gh-multica-sync/internal/payload"
)

// Entry is the last known view of a pull request, plus when it was synced.
type Entry struct {
	Snapshot payload.Snapshot `json:"snapshot"`
	SyncedAt time.Time        `json:"synced_at"`
	Action   string           `json:"last_action"`
}

// Sweep is what the last pass over a repository saw.
//
// Skipped is the count that has no other witness: a pull request with no card
// reference is dropped without a line of output, so a repository being examined
// and fully discarded looks exactly like a repository where nothing changed.
// That is how a stale issue prefix stayed invisible for two days.
type Sweep struct {
	Examined int       `json:"examined"`
	Skipped  int       `json:"skipped"`
	At       time.Time `json:"at"`
}

// Store is the whole file: entries keyed by owner/repo#number, an avatar cache
// by login, push marks left by the optional hook, and the last sweep per
// repository.
type Store struct {
	Entries map[string]Entry     `json:"entries"`
	Avatars map[string]string    `json:"avatars,omitempty"`
	Pushes  map[string]time.Time `json:"pushes,omitempty"`
	Sweeps  map[string]Sweep     `json:"sweeps,omitempty"`
}

func Path() string { return filepath.Join(discover.StateDir(), "state.json") }

// Key identifies a pull request within a workspace. The workspace is part of
// the key because the same pull request can be mirrored into more than one
// board: keying only by repository would make the second board miss every event
// the first one already consumed.
func Key(workspaceID, owner, repo string, number int32) string {
	return fmt.Sprintf("%s|%s/%s#%d", workspaceID, owner, repo, number)
}

// Load reads the state. A missing file yields an empty, usable Store.
//
// legacyWorkspace re-keys entries written before keys carried a workspace: an
// unrecognized key looks like a pull request we have never seen, and the next
// sweep would re-emit all of them, closing cards that merged long ago.
func Load(legacyWorkspace string) (*Store, error) {
	s := &Store{
		Entries: map[string]Entry{},
		Avatars: map[string]string{},
		Pushes:  map[string]time.Time{},
		Sweeps:  map[string]Sweep{},
	}
	raw, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("reading %s: %w", Path(), err)
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", Path(), err)
	}
	if s.Entries == nil {
		s.Entries = map[string]Entry{}
	}
	if s.Avatars == nil {
		s.Avatars = map[string]string{}
	}
	if s.Pushes == nil {
		s.Pushes = map[string]time.Time{}
	}
	if s.Sweeps == nil {
		s.Sweeps = map[string]Sweep{}
	}
	if legacyWorkspace != "" {
		for k, v := range s.Entries {
			if strings.Contains(k, "|") {
				continue
			}
			delete(s.Entries, k)
			s.Entries[legacyWorkspace+"|"+k] = v
		}
	}
	return s, nil
}

// Save writes the state atomically. A poll interrupted mid-write would
// otherwise leave truncated JSON, and the next cycle would re-emit everything
// as if it were new.
func (s *Store) Save() error {
	dir := discover.StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), Path())
}

// Previous returns the last snapshot of a pull request, or nil the first time.
func (s *Store) Previous(workspaceID string, snap payload.Snapshot) *payload.Snapshot {
	e, ok := s.Entries[Key(workspaceID, snap.Owner, snap.Repo, snap.Number)]
	if !ok {
		return nil
	}
	prev := e.Snapshot
	return &prev
}

// Record stores the new view of a pull request.
func (s *Store) Record(workspaceID string, snap payload.Snapshot, action string) {
	s.Entries[Key(workspaceID, snap.Owner, snap.Repo, snap.Number)] = Entry{
		Snapshot: snap,
		SyncedAt: time.Now().UTC(),
		Action:   action,
	}
}

// RecordSweep stores what the pass over a repository saw, keyed like the
// entries so two boards watching the same repository each keep their own count.
func (s *Store) RecordSweep(workspaceID, repo string, examined, skipped int) {
	s.Sweeps[workspaceID+"|"+repo] = Sweep{Examined: examined, Skipped: skipped, At: time.Now().UTC()}
}

// LastSweep returns the last pass over a repository, if there was one.
func (s *Store) LastSweep(workspaceID, repo string) (Sweep, bool) {
	sw, ok := s.Sweeps[workspaceID+"|"+repo]
	return sw, ok
}

// MarkPush records that a push happened in a repository. The optional pre-push
// hook writes this, and it is what makes the next poll look again sooner.
func (s *Store) MarkPush(repo string) { s.Pushes[repo] = time.Now().UTC() }

// RecentPush reports whether a push is recent enough to justify a second look.
func (s *Store) RecentPush(repo string, window time.Duration) bool {
	t, ok := s.Pushes[repo]
	return ok && time.Since(t) < window
}
