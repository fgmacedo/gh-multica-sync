package payload

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// issueKeyRe caches one compiled pattern per prefix. Poll compiles nothing per
// pull request, and the map stays tiny: one entry per workspace.
var issueKeyRe sync.Map

// MentionsIssue reports whether a pull request references an issue of the given
// workspace prefix, in its title, body or branch name. Those are the three
// places Multica scans, so matching the same three keeps this filter aligned
// with what the server would actually link.
//
// Why filter at all: a mirrored pull request with no linked issue is invisible
// in Multica, because pull requests are shown inside cards. Sending it costs a
// request, a row and log noise, and shows nobody anything. On a busy monorepo
// that is the difference between a handful of relevant events and a constant
// stream of irrelevant ones.
//
// An empty prefix disables the filter, which keeps the tool usable if the
// workspace prefix cannot be resolved for some reason.
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
	// The boundaries matter more than they look. Without the left one, prefix
	// JUS would match "BONJUS-1"; without the right one, JUS-1 would match
	// "JUS-12", linking a pull request to the wrong card.
	re, err := regexp.Compile(fmt.Sprintf(`(?i)(^|[^A-Za-z0-9])%s-[0-9]+([^0-9]|$)`, regexp.QuoteMeta(prefix)))
	if err != nil {
		return nil, err
	}
	issueKeyRe.Store(prefix, re)
	return re, nil
}
