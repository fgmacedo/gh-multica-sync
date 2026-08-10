# gh-multica-sync

Mirror GitHub pull requests into a **local** [Multica](https://github.com/multica-ai/multica) instance, with no GitHub App and no public tunnel.

Cards start showing their pull request, auto-linking by issue identifier works, and merging closes the card. Enabled per repository, per workspace: anything not on the list is never touched.

```
$ gh multica-sync doctor
  [ok     ] gh authenticated
  [ok     ] multica config: http://localhost:8080
  [ok     ] server responding: http://localhost:8080
  [ok     ] webhook secret: read from ~/.multica/server/.env
  [ok     ] workspace Personal (ACME): installation 9167915677, acme/widgets
  [ok     ] workspace Work (WRK): installation 9419679405, corp/platform

All set. Polling can run.
```

## The problem

On a local instance, the link between pull requests and cards does not work, for two independent reasons:

1. Multica attributes every `pull_request` event to a workspace by the **GitHub App** `installation.id`, and **silently drops** what it does not recognize. A repository webhook does not carry that field.
1. The backend listens on `127.0.0.1`, so GitHub cannot reach the delivery URL.

The supported answer is to create your own GitHub App and expose the backend through a tunnel. This tool gets the same result without either, by using the fact that everything happens on one machine: `gh` is already authenticated and the server is one `localhost` away.

## How it works

Three pieces:

1. **Bootstrap.** Creates a local installation and binds it to your workspace through the same callback the GitHub App flow uses. That callback accepts any numeric id as long as the `state` is signed by the server itself, and it is the server that issues that `state`. Nothing is forged on GitHub's side: this is your own instance accepting to be configured by you.
1. **Event synthesis.** Discovers pull requests through `gh`, builds the `pull_request` payload in the shape Multica expects, and signs it with HMAC-SHA256 using the `GITHUB_WEBHOOK_SECRET` of your own installation.
1. **Periodic sweep.** A timer walks the enabled repositories and emits an event only for what changed.

### Why a sweep and not a hook

This question comes up every time, and the answer explains the design:

- **Opening a pull request is not a git operation**, it is an API call. No hook exists for it, and there is no `post-push`: the last client-side hook is `pre-push`, which runs before the pull request exists.
- **Intercepting `gh` is not reliable.** An agent may use `gh pr create`, `gh api`, `curl`, or simply push and leave the pull request for you to open in the browser.
- **Merging does not happen on your machine.** It comes from the browser, later, and it is what closes the card. No local trigger can see it.

The sweep observes the **result** (the pull request exists, the pull request changed state), not the method. It costs one `gh pr list` per enabled repository per cycle, and exits in milliseconds when nothing is enabled.

The interval defaults to five minutes and is set with `install-timer --interval`, taking any Go duration (`90s`, `15m`, `1h`). One minute is the floor: below that the sweep stops being cheap, since every cycle spends an API call per enabled repository and GitHub's secondary limits react to sustained bursts. Run `sync` when you need a result now. `status` reports the interval by reading it back from the installed timer, so what it shows is what is actually scheduled.

There is an optional `install-hook` that adds a `pre-push` to **speed up** the sweep after a push. It detects no pull requests, and nothing depends on it.

## Install

```bash
gh extension install fgmacedo/gh-multica-sync
gh multica-sync doctor
```

`doctor` changes nothing and lists what is missing, with the command that fixes each item.

## Setup

```bash
gh multica-sync bootstrap --write-env   # set both variables in the server's .env
# restart the backend, as the command tells you
gh multica-sync bootstrap               # bind the installation
cd my-project && gh multica-sync enable
gh multica-sync install-timer           # sweep every 5 minutes (--interval 15m to change)
```

### Several workspaces

Multica's issue prefix lives on the workspace, not on the project, so keeping
personal and work cards apart means separate workspaces. This tool follows that:
`bootstrap`, `enable` and `disable` take `--workspace <id|slug|prefix>`, and
default to the workspace the Multica CLI points at.

```bash
gh multica-sync bootstrap --workspace work
cd corp-project && gh multica-sync enable --workspace work
```

Each workspace gets **its own installation id**, and that is not cosmetic:
Multica binds an installation to one or more workspaces and fans every event out
to all of them, so a shared id would mirror your personal pull requests into the
work board. Separate ids keep the boards separate.

`--write-env` sets `GITHUB_APP_SLUG` (a placeholder slug, used only to build a URL that is never visited) and `GITHUB_WEBHOOK_SECRET` (random) in your installation's `.env`. Restarting the backend is the only step that touches your server, which is why it never happens without you asking.

Almost nothing has to be asked because the Multica CLI already stores `server_url`, `workspace_id` and the token in `~/.multica/config.json`. This tool reads them from there and reuses the same token, without keeping a copy.

## Commands

| Command | What it does |
|---|---|
| `doctor` | Diagnose the environment without changing anything |
| `bootstrap [--write-env]` | Bind the local installation to the workspace |
| `enable [owner/repo] [--scope mine\|all]` | Enable a repository (with no argument, the current one) |
| `disable [owner/repo]` | Disable it |
| `status` | Settings, enabled repositories and mirrored pull requests |
| `sync [owner/repo] [n]` | Sync now, without waiting for the timer |
| `poll` | Sweep the enabled repositories and emit what changed |
| `install-timer [--interval 5m]` | LaunchAgent that sweeps periodically (macOS) |
| `install-hook` | Optional `pre-push` that speeds up the sweep |

## What gets mirrored

Two filters keep a busy repository quiet, both on by default:

**Only pull requests that reference a card.** A pull request whose title, body
or branch does not contain `<PREFIX>-<n>` for the workspace is skipped. This is
not a preference: Multica shows pull requests *inside cards*, so a mirrored pull
request with no linked issue is a row nobody can see. On a monorepo the
difference is stark. Pointing this tool at a repository with 153 open pull
requests and no card references produces zero events; the same repository
without the filter would emit a stream of them every sweep.

**Only your pull requests.** Discovery passes `--author @me`. Use
`gh multica-sync enable --scope all` on repositories where teammates open pull
requests against your cards.

A pull request named explicitly (`gh multica-sync sync owner/repo 42`) bypasses
the author scope: you asked for that one by name.

## Closing the card on merge

Multica scans title, body and branch name for the issue identifier, but it only marks **close intent** when it finds a keyword: `Closes MAC-12`, `Fixes MAC-12`, `Resolves MAC-12`, in the title or the body.

A title prefix or a branch reference **links without closing**. If you use agents to open pull requests, ask them for a `Closes <KEY>` line in the body. Without it the pull request still shows up on the card; it just will not advance the status on merge.

## Configuration

| Variable | Effect |
|---|---|
| `MULTICA_SERVER_DIR` | Server install directory (default `~/.multica/server`) |
| `MULTICA_SYNC_WEBHOOK_SECRET` | Webhook secret, for when the server lives on another machine |
| `MULTICA_SYNC_SERVER_URL` | Override the discovered `server_url` |
| `MULTICA_SYNC_HOME` | Where to keep config and state (default `~/.multica/pr-sync`) |

## Limitations, honestly

- **CI status stays empty on the card.** Multica treats `check_suite`, `check_run` and `status` webhooks as pure triggers and never reads their payload: it re-fetches from the API, which requires authenticating as a GitHub App (`GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY`). Synthesizing those events would only trigger a fetch that cannot happen. Mergeability, by contrast, does work, because it travels in the pull request payload itself.
- **Up to five minutes of latency.** `gh multica-sync sync` covers the times you are in a hurry.
- **The installation is fabricated.** If you later install a real GitHub App, remove the local installation first, from Multica's Settings screen, or the ids will conflict.
- **This is an unsupported integration.** If a Multica upgrade starts validating the installation against the GitHub API, it stops working. That is why every send first confirms the installation is still bound, and fails with a clear message instead of disappearing quietly.

## Scope

A tool for **your own self-hosted instance**, signing with a secret you set yourself. It does not circumvent licensing, billing, or anyone else's authentication. If you use a team's shared instance or Multica Cloud, you do not need this: the GitHub App already exists there.

For GitLab, Gitea and Forgejo, Multica ships a native token-based integration that needs no App at all. There the problem is only reachability, and the answer would be forwarding rather than synthesis. That is why this tool's name is GitHub-specific.

## Uninstall

```bash
launchctl bootout gui/$(id -u)/ai.multica.prsync
rm ~/Library/LaunchAgents/ai.multica.prsync.plist
rm -rf ~/.multica/pr-sync
gh extension remove gh-multica-sync
```

Also remove the local installation under Settings → GitHub in Multica, and the two variables from `.env` if you are done with it.

## Development

```bash
make test    # go test ./...
make lint    # gofmt + go vet
make build   # binary in ./bin
```

## License

Apache-2.0. See [LICENSE](LICENSE).
