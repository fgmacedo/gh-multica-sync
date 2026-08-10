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
// issue produces a row nobody can see. An empty prefix disables the filter.
func MentionsIssue(s Snapshot, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return true
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
