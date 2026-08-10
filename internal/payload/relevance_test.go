package payload

import "testing"

func TestMentionsIssue(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		snap   Snapshot
		want   bool
	}{
		{"title prefix", "MAC", Snapshot{Title: "MAC-12: delegate decoding"}, true},
		{"closing keyword in body", "MAC", Snapshot{Body: "Closes MAC-12"}, true},
		{"branch reference", "JUS", Snapshot{HeadRef: "feat/JUS-7-add-linter"}, true},
		{"lowercase key", "JUS", Snapshot{Title: "fix jus-7 typo"}, true},
		{"no reference at all", "JUS", Snapshot{Title: "chore(deps): bump lodash"}, false},

		// On a Jusbrasil monorepo the bare word "jus" is everywhere, and
		// GitHub's own search tokenizer matches it. The -N suffix is what
		// separates a card reference from a company name.
		{"company name is not a key", "JUS", Snapshot{Title: "feat(shared-topbar): show Jus IA freemium CTA"}, false},
		{"snake case mention is not a key", "JUS", Snapshot{Body: "variante teams do lock (jus_ia_teams_on_lock)"}, false},

		{"longer word ending in the prefix", "JUS", Snapshot{Title: "BONJUS-1 unrelated"}, false},
		{"key is not a prefix of a longer key", "MAC", Snapshot{Title: "MAC-123 something"}, true},

		{"empty prefix disables the filter", "", Snapshot{Title: "anything"}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MentionsIssue(c.snap, c.prefix); got != c.want {
				t.Fatalf("MentionsIssue(%q) = %v, want %v", c.prefix, got, c.want)
			}
		})
	}
}

// MAC-1 and MAC-12 are different cards.
func TestMentionsIssueDoesNotMatchNumericPrefix(t *testing.T) {
	if !MentionsIssue(Snapshot{Title: "MAC-12 work"}, "MAC") {
		t.Fatal("MAC-12 should match prefix MAC")
	}
	re, err := issueKeyPattern("MAC")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("MAC-12") || !re.MatchString("MAC-1") {
		t.Fatal("both keys should match the prefix pattern")
	}
}
