package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strings"
)

// GenerateInstallationID draws a local installation id.
//
// Real GitHub ids are 8 or 9 digits today. Drawing in the high ten-digit range
// puts a collision with a real installation out of reach and makes the id
// recognizable at a glance when someone is debugging the database.
func GenerateInstallationID() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
	if err != nil {
		return 0, err
	}
	return 9_000_000_000 + n.Int64(), nil
}

// GenerateSecret draws a webhook secret.
func GenerateSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// DefaultAppSlug is the placeholder slug. It is only used to build the install
// URL the connect endpoint returns, and we never visit that URL. No App by
// this name exists, and none needs to.
const DefaultAppSlug = "multica-local"

var assignRe = regexp.MustCompile(`(?m)^(\s*)([A-Z_][A-Z0-9_]*)=.*$`)

// WriteEnvVars sets or replaces keys in a .env file, preserving everything
// else. A key that is absent is appended.
//
// Editing someone else's .env calls for care: replacement is anchored at the
// start of a line (so a comment mentioning the key is left alone) and the write
// is atomic, so a failure halfway through cannot leave a truncated file.
func WriteEnvVars(path string, vars map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	content := string(raw)

	remaining := map[string]string{}
	for k, v := range vars {
		remaining[k] = v
	}

	content = assignRe.ReplaceAllStringFunc(content, func(line string) string {
		m := assignRe.FindStringSubmatch(line)
		key := m[2]
		val, ok := remaining[key]
		if !ok {
			return line
		}
		delete(remaining, key)
		return m[1] + key + "=" + val
	})

	if len(remaining) > 0 {
		var b strings.Builder
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n# Added by gh-multica-sync\n")
		for k, v := range remaining {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
		content = b.String()
	}

	tmp := path + ".gh-multica-sync.tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}
