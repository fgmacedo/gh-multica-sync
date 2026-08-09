package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fgmacedo/gh-multica-sync/internal/discover"
	"github.com/fgmacedo/gh-multica-sync/internal/state"
)

const launchAgentLabel = "ai.multica.prsync"

const launchAgentPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%[1]s</string>

    <!-- A login shell (without -i) to inherit the PATH from the profile: gh
         usually lives somewhere launchd does not know about. An interactive
         shell would also source .zshrc, and a fancy prompt writes noise to
         stderr, which would land in the error log and hide real problems. -->
    <key>ProgramArguments</key>
    <array>
        <string>/bin/sh</string>
        <string>-c</string>
        <string>exec %[2]s -lc 'exec gh multica-sync poll'</string>
    </array>

    <key>StartInterval</key>
    <integer>300</integer>

    <key>StandardOutPath</key>
    <string>%[3]s/poll.out.log</string>
    <key>StandardErrorPath</key>
    <string>%[3]s/poll.err.log</string>
</dict>
</plist>
`

func runInstallTimer(ctx context.Context, e *Env) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf(
			"automatic timer installation only exists on macOS.\n"+
				"  On Linux, schedule `gh multica-sync poll` every 5 minutes with a systemd timer or cron:\n"+
				"    */5 * * * * %s -lc 'gh multica-sync poll'", shellPath())
	}

	dir := discover.StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	content := fmt.Sprintf(launchAgentPlist, launchAgentLabel, shellPath(), dir)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), launchAgentLabel)
	// Unloading first is what makes this command repeatable: without it, a
	// second run would fail with "service already loaded".
	_ = exec.CommandContext(ctx, "launchctl", "bootout", target).Run()
	out, err := exec.CommandContext(ctx, "launchctl", "bootstrap",
		fmt.Sprintf("gui/%d", os.Getuid()), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("loading the LaunchAgent: %s", strings.TrimSpace(string(out)))
	}

	fmt.Fprintf(e.Out, "Timer installed: %s\n", path)
	fmt.Fprintf(e.Out, "Runs 'gh multica-sync poll' every 5 minutes. Logs in %s.\n", dir)
	fmt.Fprintf(e.Out, "To remove: launchctl bootout %s && rm %s\n", target, path)
	return nil
}

const prePushHook = `#!/bin/sh
# Installed by gh-multica-sync.
#
# This does NOT detect pull requests: opening a PR is a GitHub API call, not a
# git operation, and no hook exists for it. What this hook does is record that a
# push happened, so the next sweep looks at this repository sooner. Without it
# everything still works, just with more latency.
gh multica-sync mark-push >/dev/null 2>&1 &
exit 0
`

func runInstallHook(ctx context.Context, e *Env) error {
	owner, repo, err := e.Forge.CurrentRepo(ctx)
	if err != nil {
		return err
	}
	root, err := gitRoot(ctx)
	if err != nil {
		return err
	}
	path := filepath.Join(root, ".git", "hooks", "pre-push")

	if existing, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(existing), "gh-multica-sync") {
			return fmt.Errorf(
				"there is already a pre-push hook in %s that is not mine.\n"+
					"  I will not overwrite it. Add this line to it instead:\n"+
					"    gh multica-sync mark-push >/dev/null 2>&1 &", path)
		}
		fmt.Fprintf(e.Out, "Hook already installed in %s.\n", path)
		return nil
	}

	if err := os.WriteFile(path, []byte(prePushHook), 0o755); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "Hook installed in %s (%s/%s).\n", path, owner, repo)
	fmt.Fprintln(e.Out, "It only speeds up the sweep. Nothing depends on it.")
	return nil
}

// runMarkPush is called by the hook.
func runMarkPush(ctx context.Context, e *Env, args []string) error {
	owner, repo, err := e.resolveRepo(ctx, args)
	if err != nil {
		return err
	}
	st, err := state.Load()
	if err != nil {
		return err
	}
	st.MarkPush(owner + "/" + repo)
	return st.Save()
}

func gitRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// shellPath returns a usable login shell. zsh is the default on modern macOS;
// bash covers the rest.
func shellPath() string {
	for _, c := range []string{"/bin/zsh", "/bin/bash"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "/bin/sh"
}
