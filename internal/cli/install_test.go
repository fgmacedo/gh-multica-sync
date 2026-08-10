package cli

import (
	"testing"
	"time"
)

func TestValidateInterval(t *testing.T) {
	cases := []struct {
		name    string
		in      time.Duration
		want    int
		wantErr bool
	}{
		{"default", defaultInterval, 300, false},
		{"exactly the minimum", time.Minute, 60, false},
		{"an hour", time.Hour, 3600, false},
		{"ninety seconds", 90 * time.Second, 90, false},
		{"below the minimum", 30 * time.Second, 0, true},
		{"zero", 0, 0, true},
		{"negative", -time.Minute, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := validateInterval(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateInterval(%s) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Fatalf("validateInterval(%s) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// The plist is the source of truth for the interval, so the read-back has to
// survive the exact formatting the template produces.
func TestInstalledIntervalParsesTheTemplate(t *testing.T) {
	rendered := renderPlist(t, 900)
	m := startIntervalRe.FindStringSubmatch(rendered)
	if m == nil {
		t.Fatalf("StartInterval not found in the rendered plist:\n%s", rendered)
	}
	if m[1] != "900" {
		t.Fatalf("parsed %q, want 900", m[1])
	}
}

func renderPlist(t *testing.T, seconds int) string {
	t.Helper()
	return sprintPlist(seconds)
}
