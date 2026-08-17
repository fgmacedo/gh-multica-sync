package cli

import (
	"strings"
	"testing"

	"github.com/fgmacedo/gh-multica-sync/internal/state"
)

func TestSweepLine(t *testing.T) {
	tests := []struct {
		name     string
		sweep    *state.Sweep
		contains string
	}{
		{"never swept", nil, ""},
		{"nothing in range", &state.Sweep{Examined: 0}, "no pull request in range"},
		{"everything discarded", &state.Sweep{Examined: 30, Skipped: 30}, "30 examined, none referencing MCD-<n>"},
		{"some mirrored", &state.Sweep{Examined: 30, Skipped: 26}, "30 examined, 4 referencing MCD-<n>"},
		{"all mirrored", &state.Sweep{Examined: 3, Skipped: 0}, "3 examined, 3 referencing MCD-<n>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &state.Store{Sweeps: map[string]state.Sweep{}}
			if tc.sweep != nil {
				st.RecordSweep("ws-1", "owner/repo", tc.sweep.Examined, tc.sweep.Skipped)
			}
			got := sweepLine(st, "ws-1", "owner/repo", "MCD")
			if tc.contains == "" {
				if got != "" {
					t.Fatalf("got %q, want nothing for a repository never swept", got)
				}
				return
			}
			if !strings.Contains(got, tc.contains) {
				t.Errorf("got %q, want it to contain %q", got, tc.contains)
			}
		})
	}
}
