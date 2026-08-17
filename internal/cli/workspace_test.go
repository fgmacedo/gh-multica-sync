package cli

import "testing"

func TestServerPrefix(t *testing.T) {
	list := []workspaceInfo{
		{ID: "a", IssuePrefix: "MCD"},
		{ID: "b", IssuePrefix: ""},
	}
	for _, tc := range []struct{ name, id, want string }{
		{"workspace with a prefix", "a", "MCD"},
		{"workspace the server reports without one", "b", ""},
		{"workspace absent from the list", "missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverPrefix(list, tc.id); got != tc.want {
				t.Errorf("serverPrefix(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

func TestServerPrefixOnEmptyList(t *testing.T) {
	if got := serverPrefix(nil, "a"); got != "" {
		t.Errorf("serverPrefix(nil) = %q, want empty so the caller refuses to sweep", got)
	}
}
