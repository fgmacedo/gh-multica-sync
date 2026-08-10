package payload

import "strings"

// The pull_request webhook actions we emit: only the ones that change something
// on Multica's side, not every action GitHub defines.
const (
	ActionOpened           = "opened"
	ActionClosed           = "closed"
	ActionReopened         = "reopened"
	ActionSynchronize      = "synchronize"
	ActionReadyForReview   = "ready_for_review"
	ActionConvertedToDraft = "converted_to_draft"
	ActionEdited           = "edited"
)

// DeriveAction decides which action represents the difference between what we
// already knew about a pull request (prev, nil the first time) and what the
// forge reports now. ok=false means nothing changed and no event is sent, which
// is what keeps polling idempotent.
//
// Order matters: closing and reopening come first, so a pull request that
// merged and also moved its head within one interval reads as closed rather
// than as synchronized.
func DeriveAction(prev *Snapshot, cur Snapshot) (string, bool) {
	if prev == nil {
		// Enabling the tool on a repository with history is the common case,
		// and "opened" for a pull request already closed would describe
		// something that never happened.
		if cur.Open() {
			return ActionOpened, true
		}
		return ActionClosed, true
	}

	wasOpen, isOpen := prev.Open(), cur.Open()
	switch {
	case wasOpen && !isOpen:
		return ActionClosed, true
	case !wasOpen && isOpen:
		return ActionReopened, true
	}

	// Already non-open but not yet merged: without an event here the card never
	// receives the merge signal that drives auto-advance.
	if !prev.Merged() && cur.Merged() {
		return ActionClosed, true
	}

	if prev.Draft && !cur.Draft {
		return ActionReadyForReview, true
	}
	if !prev.Draft && cur.Draft {
		return ActionConvertedToDraft, true
	}

	if !strings.EqualFold(prev.HeadSHA, cur.HeadSHA) {
		return ActionSynchronize, true
	}

	// Title and body feed auto-linking, so a change to either has to reach
	// Multica even with no new commit.
	if prev.Title != cur.Title || prev.Body != cur.Body {
		return ActionEdited, true
	}

	return "", false
}
