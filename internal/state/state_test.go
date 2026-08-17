package state

import "testing"

func TestSweepKeyIgnoresCase(t *testing.T) {
	s := &Store{Sweeps: map[string]Sweep{}}
	s.RecordSweep("ws-1", "Owner/Repo", 30, 30)

	// The config matches repositories with EqualFold, so a sweep recorded from
	// the command line must be found from the config spelling.
	sw, ok := s.LastSweep("ws-1", "owner/repo")
	if !ok {
		t.Fatal("sweep recorded as Owner/Repo not found as owner/repo")
	}
	if sw.Examined != 30 || sw.Skipped != 30 {
		t.Errorf("got %d examined, %d skipped; want 30 and 30", sw.Examined, sw.Skipped)
	}
	if len(s.Sweeps) != 1 {
		t.Errorf("got %d entries, want the two spellings to share one", len(s.Sweeps))
	}
}

func TestSweepIsScopedToWorkspace(t *testing.T) {
	s := &Store{Sweeps: map[string]Sweep{}}
	s.RecordSweep("ws-1", "owner/repo", 30, 30)
	s.RecordSweep("ws-2", "owner/repo", 10, 2)

	for _, tc := range []struct {
		ws       string
		examined int
	}{{"ws-1", 30}, {"ws-2", 10}} {
		sw, ok := s.LastSweep(tc.ws, "owner/repo")
		if !ok || sw.Examined != tc.examined {
			t.Errorf("%s: got %d examined (found=%v), want %d", tc.ws, sw.Examined, ok, tc.examined)
		}
	}
}

// Entry keys are matched against a file written by earlier versions: changing
// their case would make every known pull request look new and re-emit it.
func TestKeyPreservesCase(t *testing.T) {
	if got := Key("ws-1", "Owner", "Repo", 12); got != "ws-1|Owner/Repo#12" {
		t.Errorf("Key = %q, want the spelling preserved", got)
	}
}
