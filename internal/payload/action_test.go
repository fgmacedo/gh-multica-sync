package payload

import "testing"

func snap(state string, mods ...func(*Snapshot)) Snapshot {
	s := Snapshot{
		Owner: "acme", Repo: "widgets", Number: 7,
		Title: "t", Body: "b", State: state,
		HeadSHA: "aaa", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	for _, m := range mods {
		m(&s)
	}
	return s
}

func TestDeriveAction(t *testing.T) {
	draft := func(s *Snapshot) { s.Draft = true }
	sha := func(v string) func(*Snapshot) { return func(s *Snapshot) { s.HeadSHA = v } }
	title := func(v string) func(*Snapshot) { return func(s *Snapshot) { s.Title = v } }

	cases := []struct {
		name string
		prev *Snapshot
		cur  Snapshot
		want string
		ok   bool
	}{
		{"first sight, open", nil, snap("OPEN"), ActionOpened, true},
		{"first sight, already merged", nil, snap("MERGED"), ActionClosed, true},
		{"first sight, already closed", nil, snap("CLOSED"), ActionClosed, true},
		{"open to merged", ptr(snap("OPEN")), snap("MERGED"), ActionClosed, true},
		{"open to closed", ptr(snap("OPEN")), snap("CLOSED"), ActionClosed, true},
		{"reopened", ptr(snap("CLOSED")), snap("OPEN"), ActionReopened, true},
		{"closed then merged", ptr(snap("CLOSED")), snap("MERGED"), ActionClosed, true},
		{"left draft", ptr(snap("OPEN", draft)), snap("OPEN"), ActionReadyForReview, true},
		{"became draft", ptr(snap("OPEN")), snap("OPEN", draft), ActionConvertedToDraft, true},
		{"new commit", ptr(snap("OPEN")), snap("OPEN", sha("bbb")), ActionSynchronize, true},
		{"title changed", ptr(snap("OPEN")), snap("OPEN", title("other")), ActionEdited, true},
		{"nothing changed", ptr(snap("OPEN")), snap("OPEN"), "", false},
		{"stable merged does not re-emit", ptr(snap("MERGED")), snap("MERGED"), "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := DeriveAction(c.prev, c.cur)
			if ok != c.ok || got != c.want {
				t.Fatalf("DeriveAction() = (%q, %v), want (%q, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}

// Closing wins over a new commit landing in the same interval: to the board,
// the merge is the fact that matters.
func TestDeriveActionClosingBeatsSynchronize(t *testing.T) {
	prev := snap("OPEN")
	cur := snap("MERGED", func(s *Snapshot) { s.HeadSHA = "zzz" })
	if got, _ := DeriveAction(&prev, cur); got != ActionClosed {
		t.Fatalf("want %q, got %q", ActionClosed, got)
	}
}

func TestWebhookStateAndMerged(t *testing.T) {
	for _, c := range []struct {
		state      string
		wantState  string
		wantMerged bool
	}{
		{"OPEN", "open", false},
		{"CLOSED", "closed", false},
		{"MERGED", "closed", true},
	} {
		s := snap(c.state)
		ev := Build(s, ActionOpened, 42)
		if ev.PullRequest.State != c.wantState || ev.PullRequest.Merged != c.wantMerged {
			t.Fatalf("%s: state=%q merged=%v, want %q/%v",
				c.state, ev.PullRequest.State, ev.PullRequest.Merged, c.wantState, c.wantMerged)
		}
	}
}

func TestMergeableStateUnknownIsEmpty(t *testing.T) {
	if got := mergeableState("SOMETHING_NEW"); got != "" {
		t.Fatalf("want empty for an unknown value, got %q", got)
	}
	if got := mergeableState("clean"); got != "clean" {
		t.Fatalf("want clean, got %q", got)
	}
}

func ptr(s Snapshot) *Snapshot { return &s }
