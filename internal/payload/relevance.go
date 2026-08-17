package payload

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// issueKeyRe caches one compiled pattern per prefix.
var issueKeyRe sync.Map

// MentionsIssue reports whether a pull request references an issue of the given
// workspace prefix, in its title, body or branch name: the same three places
// Multica scans, so this filter matches what the server would link.
//
// Multica shows pull requests inside cards, so mirroring one with no linked
// issue produces a row nobody can see. With no prefix nothing can reference a
// card, which is why an empty one mirrors nothing rather than everything: the
// caller that could not resolve a prefix has no idea what a card key looks like,
// and a sweep that guesses floods the board with pull requests nobody linked.
func MentionsIssue(s Snapshot, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	re, err := issueKeyPattern(prefix)
	if err != nil {
		return true
	}
	return re.MatchString(s.Title) || re.MatchString(s.Body) || re.MatchString(s.HeadRef)
}

func issueKeyPattern(prefix string) (*regexp.Regexp, error) {
	if cached, ok := issueKeyRe.Load(prefix); ok {
		return cached.(*regexp.Regexp), nil
	}
	// Without the left boundary, prefix JUS matches "BONJUS-1"; without the
	// right one, JUS-1 matches "JUS-12" and links to the wrong card.
	re, err := regexp.Compile(fmt.Sprintf(`(?i)(^|[^A-Za-z0-9])%s-[0-9]+([^0-9]|$)`, regexp.QuoteMeta(prefix)))
	if err != nil {
		return nil, err
	}
	issueKeyRe.Store(prefix, re)
	return re, nil
}
