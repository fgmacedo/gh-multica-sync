package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEnvVarsReplacesAndAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := `# a comment mentioning GITHUB_APP_SLUG that must not be touched
POSTGRES_DB=multica
GITHUB_APP_SLUG=
JWT_SECRET=abc
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteEnvVars(path, map[string]string{
		"GITHUB_APP_SLUG":       "multica-local",
		"GITHUB_WEBHOOK_SECRET": "s3cr3t",
	})
	if err != nil {
		t.Fatalf("WriteEnvVars: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)

	if !strings.Contains(s, "\nGITHUB_APP_SLUG=multica-local\n") {
		t.Errorf("existing key was not replaced:\n%s", s)
	}
	if !strings.Contains(s, "GITHUB_WEBHOOK_SECRET=s3cr3t") {
		t.Errorf("missing key was not appended:\n%s", s)
	}
	if !strings.Contains(s, "# a comment mentioning GITHUB_APP_SLUG that must not be touched") {
		t.Errorf("the comment was altered:\n%s", s)
	}
	if !strings.Contains(s, "POSTGRES_DB=multica") || !strings.Contains(s, "JWT_SECRET=abc") {
		t.Errorf("unrelated lines were lost:\n%s", s)
	}
	// Only the assignment line carries the "=", the comment mentions the key
	// without it. A second occurrence would mean compose sees the key twice.
	if strings.Count(s, "GITHUB_APP_SLUG=") != 1 {
		t.Errorf("key duplicated instead of replaced:\n%s", s)
	}
}

func TestWriteEnvVarsKeepsPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvVars(path, map[string]string{"B": "2"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions became %o, want 600: the file holds a secret", perm)
	}
}

func TestGenerateInstallationIDStaysInLocalRange(t *testing.T) {
	for i := 0; i < 50; i++ {
		id, err := GenerateInstallationID()
		if err != nil {
			t.Fatal(err)
		}
		if id < 9_000_000_000 || id >= 10_000_000_000 {
			t.Fatalf("id outside the reserved range: %d", id)
		}
	}
}
