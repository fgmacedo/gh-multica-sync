package payload

import "strings"

// The pull_request webhook actions we emit. Not every action GitHub defines is
// here: only the ones that change something on Multica's side.
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
// forge reports now. It returns ok=false when nothing changed, and no event is
// sent in that case: this is what keeps polling idempotent and cheap.
//
// Order matters. Closing and reopening come first because they carry the
// transition the board cares about most: a pull request that was merged and
// whose head also moved within the same interval is, to Multica, a closed pull
// request rather than a synchronized one.
func DeriveAction(prev *Snapshot, cur Snapshot) (string, bool) {
	if prev == nil {
		// First time we see this pull request. Finding it already closed is
		// common when the tool is enabled on a repository with history:
		// emitting "opened" here would describe something that never happened.
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

	// Merged without passing through closed within our observation window: the
	// previous state was already non-open, but not merged. This still deserves
	// an event, otherwise the card never receives the merge signal that drives
	// auto-advance.
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
