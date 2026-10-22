# GitHub Apps Installation Token Generator

A shell script that generates an Installation Access Token using a GitHub App's private key (PEM).
Useful for obtaining bot tokens when operating GitHub from tools like Claude Code.

The same tool also ships as a Go binary, `ghapp-token` — a drop-in alternative with the same
environment variables, output, error messages and exit codes. See [Go Binary](#go-binary-ghapp-token).

|  | Shell script (`get-ghapp-token.sh`) | Go binary (`ghapp-token`) |
|--|--|--|
| Runtime dependencies | bash, openssl, curl, jq | None (single static binary) |
| Platforms | macOS, Linux | Release binaries: macOS (arm64, amd64). `go install`: macOS, Linux |
| Install | Copy the script | `go install`, or download a release and verify `sha256sums.txt` |
| Auditing | Read the script top to bottom | Open source; reproducible builds; published checksums |
| Keychain | Reads the PEM; `refresh-ghapp-token.sh` caches tokens | Reads the PEM only |

The shell script remains the reference implementation and the zero-build option; the Go binary
suits non-interactive use (CI, Docker Sandboxes) where installing `openssl`/`curl`/`jq` is a nuisance.

## How It Works

```
Private Key (PEM) → Generate JWT → Get Installation ID → Get Installation Access Token → stdout
```

1. Generate a JWT (JSON Web Token) from the PEM file
2. Retrieve the App's Installation ID via the GitHub API
3. Issue an Installation Access Token and print it to stdout

### Architecture: Option A (macOS Keychain)

```
┌───────────────────────────────┐
│ macOS Keychain                │
│ service: ghapp-token:<APP_ID> │
│ account: pem (base64)         │
└───────────────────────────────┘
                │
                │ security find-generic-password -w
                ▼
┌─────────────────────────────────────────┐
│ get-ghapp-token.sh                      │
│ openssl base64 -d -> PEM                │
│ (in-memory only, never written to disk) │
└─────────────────────────────────────────┘
                     │
                     │ openssl dgst -sha256 -sign
                     ▼
          JWT (RS256, 10 min TTL)
                     │
                     │ Authorization: Bearer <JWT>
                     ▼
┌────────────────────────────────────────────┐
│ GitHub API                                 │
│ GET  /app/installations                    │
│ POST /app/installations/{id}/access_tokens │
└────────────────────────────────────────────┘
                       │
                       ▼
      Installation Access Token (stdout)
```

The PEM only ever exists as base64 text in Keychain and as decoded plaintext in a shell variable — it is never written to disk.

### Architecture: Option B (File-based)

```
┌──────────────────────────┐
│ $GITHUB_APP_PEM_PATH     │
│ botname.private-key.pem  │
│ (plain PEM file on disk) │
└──────────────────────────┘
              │
              │ openssl dgst -sha256 -sign <file>
              ▼
   JWT (RS256, 10 min TTL)
              │
              │ Authorization: Bearer <JWT>
              ▼
┌────────────────────────────────────────────┐
│ GitHub API                                 │
│ GET  /app/installations                    │
│ POST /app/installations/{id}/access_tokens │
└────────────────────────────────────────────┘
                       │
                       ▼
      Installation Access Token (stdout)
```

The PEM is read straight from disk and handed to `openssl` by path — nothing is decoded or cached in memory beforehand.

The Go binary follows the same flow, with Go's `crypto/rsa` in place of `openssl` and `net/http` in place of `curl`. It still calls `security` for the Keychain read. In both modes the Go binary reads the PEM into memory and never writes it anywhere.

## Prerequisites

- GitHub App must be installed on the target repository
- Shell script: bash, openssl, curl, jq
- Go binary: nothing at runtime. Building with `go install` needs Go 1.26+, or Go 1.21+ with the default `GOTOOLCHAIN=auto`, which downloads the pinned toolchain (some distro-packaged Go sets `GOTOOLCHAIN=local`, which disables this)

## Setup

### Option A: macOS Keychain (Recommended on macOS)

Store the private key in the macOS Keychain instead of on disk. It must be
base64-encoded before being stored — Keychain does not reliably round-trip
raw multi-line secrets — and is decoded only in-memory when signing.

The Keychain service is namespaced per App ID (`ghapp-token:<APP_ID>`), so
multiple GitHub Apps can each keep their own PEM/token without colliding:

```bash
security add-generic-password -U \
  -a "pem" -s "ghapp-token:1234567" \
  -w "$(openssl base64 -A < /path/to/your-app.private-key.pem)"
```

`-U` makes this idempotent — safe to re-run when rotating the key. Replace
`1234567` with your App ID, or override the service label entirely with
`GITHUB_APP_KEYCHAIN_SERVICE`.

The App ID itself is not stored in Keychain; it still must be provided via
`GITHUB_APP_ID` or a CLI argument (see below) — the script needs it up
front to know which Keychain service to look in.

### Option B: File-based

#### 1. Place the Private Key

Place the private key (`.pem` file) generated from the GitHub App settings page.

```bash
mkdir -p ~/.config/claude-code-bot
cp your-app.private-key.pem ~/.config/claude-code-bot/botname.private-key.pem
chmod 600 ~/.config/claude-code-bot/botname.private-key.pem
```

### 2. Set Environment Variables

Set the following environment variables.

| Variable | Description | Default |
|----------|-------------|---------|
| `GITHUB_APP_ID` | GitHub App's App ID | (required) |
| `GITHUB_APP_PEM_PATH` | Path to the private key file | `~/.config/claude-code-bot/botname.private-key.pem` |
| `GITHUB_APP_KEYCHAIN_SERVICE` | Override the Keychain service label | `ghapp-token:<APP_ID>` |
| `GITHUB_APP_INSTALLATION_ID` | Use this installation ID directly, skipping the lookup | (unset — look up instead) |
| `GITHUB_APP_INSTALLATION_LOGIN` | Pick the installation whose account login matches this value | (unset — use the first installation) |
| `GITHUB_APP_LOG_FILE` | Append a timestamped log line per step to this file | (unset — no logging) |

The App ID can be found at `General > App ID` in the GitHub App settings page.

### Targeting a specific installation

If the GitHub App is installed on more than one org/repo, the script otherwise just uses whichever installation the API returns first — set one of these to target a specific one instead:

```bash
# Skip the lookup entirely if you already know the installation ID
GITHUB_APP_INSTALLATION_ID=12345678 ./get-ghapp-token.sh

# Or filter by the installation's account login (org/user)
GITHUB_APP_INSTALLATION_LOGIN=your-org ./get-ghapp-token.sh
```

`GITHUB_APP_INSTALLATION_ID` takes priority over `GITHUB_APP_INSTALLATION_LOGIN` when both are set.

### Logging

Set `GITHUB_APP_LOG_FILE` to a file path to append a timestamped line for each step (PEM resolution, JWT generation, installation lookup, token issuance) — useful when running non-interactively (cron, Docker Sandboxes' `--command`) where stderr isn't easy to see:

```bash
GITHUB_APP_LOG_FILE=/tmp/ghapp-token.log ./get-ghapp-token.sh
```

Log output never goes to stdout — only the bare token does, so this is safe to combine with `TOKEN=$(./get-ghapp-token.sh)`. `refresh-ghapp-token.sh` honors the same variable (and passes it through to `get-ghapp-token.sh`, so both scripts' steps land in one file) when it's exported in the environment. Leaving it unset keeps the previous behavior exactly — no log file, no extra overhead.

#### Setting via shell

```bash
export GITHUB_APP_ID=1234567
export GITHUB_APP_PEM_PATH=~/.config/claude-code-bot/botname.private-key.pem
```

#### Setting via Claude Code's settings.json

Use `~/.claude/settings.json` to apply globally to your machine,
or `.claude/settings.local.json` for project-specific settings that apply only to you.

```json
{
  "env": {
    "GITHUB_APP_ID": "1234567"
  }
}
```

> **Warning**: Do **not** set `GITHUB_APP_PEM_PATH` in Claude Code's settings.json.
> Claude Code has file system access and could read the private key file if its path is exposed via environment variables.
> Set `GITHUB_APP_PEM_PATH` only in your shell profile (e.g. `~/.zshrc`, `~/.bashrc`) outside of Claude Code's configuration.

> **Note**: `settings.local.json` is automatically added to `.gitignore`, so there is no risk of accidentally committing it.

## Credential Lookup Priority

| Credential | Priority order |
|------------|-----------------|
| App ID | `GITHUB_APP_ID` env var → CLI argument |
| Private key | macOS Keychain (service `ghapp-token:<APP_ID>`, account `pem`, base64-encoded) → file at `GITHUB_APP_PEM_PATH` |
| Installation | `GITHUB_APP_INSTALLATION_ID` → `GITHUB_APP_INSTALLATION_LOGIN` match → first installation returned by the API |

If `security` is unavailable (non-macOS) or no Keychain entry is registered,
the script falls back to the file-based PEM lookup silently — no error, no
change in behavior from before Keychain support existed.

The Go binary uses exactly the same priority order.

## Usage

```bash
# With environment variables already set
./get-ghapp-token.sh

# Passing App ID as an argument
./get-ghapp-token.sh 1234567

# Passing environment variables inline
GITHUB_APP_ID=1234567 ./get-ghapp-token.sh
```

### Output

On success, the Installation Access Token is printed to stdout.

```
ghs_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

You can use this token to call the GitHub API.

```bash
TOKEN=$(./get-ghapp-token.sh)
curl -H "Authorization: Bearer $TOKEN" https://api.github.com/repos/your-org/your-repo
```

## Go Binary (`ghapp-token`)

`ghapp-token` is a Go port of `get-ghapp-token.sh`. It reads the same environment variables, uses the same [lookup priority](#credential-lookup-priority), and prints the same stderr messages, log lines and exit codes, so it can replace the script in existing automation. It uses only the Go standard library.

### Install with `go install`

```bash
go install github.com/mitonattou919/get-ghapp-token/cmd/ghapp-token@latest
```

The binary lands in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` (usually `~/go/bin`) when `GOBIN` is unset; make sure that directory is on your `PATH`.

### Install from a release

Release builds are published for macOS (`arm64` for Apple silicon, `amd64` for Intel) together with a `sha256sums.txt`. The build is configured to be reproducible (`-trimpath`, no cgo, timestamps and file owners fixed to the commit), so the same commit and toolchain should produce the same checksums. Check the archive against `sha256sums.txt` before installing. The steps are chained with `&&`, so nothing is extracted or installed unless the check prints `OK`:

```bash
VERSION=x.y.z   # see https://github.com/mitonattou919/get-ghapp-token/releases
ARCH=arm64      # or amd64
BASE=https://github.com/mitonattou919/get-ghapp-token/releases/download/v${VERSION}
ARCHIVE=ghapp-token_${VERSION}_darwin_${ARCH}.tar.gz

curl -fsSLO "${BASE}/${ARCHIVE}"
curl -fsSLO "${BASE}/sha256sums.txt"
grep " ${ARCHIVE}\$" sha256sums.txt | shasum -a 256 -c - \
  && tar xzf "${ARCHIVE}" ghapp-token \
  && install -d ~/.local/bin \
  && install -m 755 ghapp-token ~/.local/bin/ghapp-token
```

This installs into `~/.local/bin`, which needs no `sudo`; add it to your `PATH` if it isn't there yet. To install system-wide instead, use `sudo install -d /usr/local/bin` and `sudo install -m 755 ghapp-token /usr/local/bin/ghapp-token` (`/usr/local/bin` may not exist on a fresh Apple silicon Mac).

`sha256sums.txt` is published in the same release as the archives, so this check detects a corrupted or incomplete download, not a tampered release. Build provenance attestation is tracked in #28.

The binaries are not signed or notarized by Apple. Files fetched with `curl` aren't quarantined, so they run as-is; if you download an archive through a browser instead, Gatekeeper will block the binary until you remove the quarantine attribute (`xattr -d com.apple.quarantine ghapp-token`).

### Usage

Identical to the script — swap `./get-ghapp-token.sh` for `ghapp-token`:

```bash
ghapp-token                          # with GITHUB_APP_ID already set
ghapp-token 1234567                  # App ID as an argument
GITHUB_APP_ID=1234567 ghapp-token    # inline
TOKEN=$(ghapp-token)
```

### Scope

- **Keychain is read-only.** `ghapp-token` reads the base64 PEM from Keychain via `security`, exactly like the script, so the same Keychain entry works for both and no extra access prompts appear. Registering the PEM (`security add-generic-password`, see [Option A](#option-a-macos-keychain-recommended-on-macos)) stays a manual setup step.
- **No token caching.** [Automatic token refresh](#automatic-token-refresh-cron) is still done by `refresh-ghapp-token.sh`, which calls `get-ghapp-token.sh`.
- **Log lines** keep the `[get-ghapp-token]` tag, so existing log filters keep working. A log file created by `ghapp-token` gets `0600` permissions.

## Claude Code Integration

When using this script with Claude Code, define a shell function in your `~/.zshrc` or `~/.bashrc` to automatically set up the token before launching Claude.

```zsh
claude-gh() {
  export GITHUB_APP_ID=1234567
  export GITHUB_APP_PEM_PATH=~/.config/claude-code-bot/botname.private-key.pem
  export GH_TOKEN=$(~/.config/claude-code-bot/get-ghapp-token.sh)

  # Configure git credential helper for this process only (no global config changes)
  export GIT_CONFIG_COUNT=1
  export GIT_CONFIG_KEY_0=credential.helper
  export GIT_CONFIG_VALUE_0='!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f'

  claude "$@"
}
```

With the Go binary, only the token line changes:

```zsh
  export GH_TOKEN=$(ghapp-token)
```

Using `GIT_CONFIG_*` environment variables instead of `gh auth setup-git` scopes the git credential helper to the Claude process only, leaving your global `~/.gitconfig` untouched.

| Approach | Scope | Modifies config file |
|----------|-------|----------------------|
| `gh auth setup-git` | Global | Yes (`~/.gitconfig`) |
| `git config --local` | Project | Yes (`.git/config`) |
| `GIT_CONFIG_*` env vars | Process | No |

With this setup, both `gh` commands and `git` commands work with the GitHub App token inside Claude Code sessions.

## Docker Sandboxes Integration

Docker Sandboxes has a built-in `github` secret that is used to authenticate `git`/`gh` operations inside a sandbox. Instead of storing a static token, point it at `get-ghapp-token.sh` with `--command` so the sandbox mints a fresh installation token itself, and refreshes it on a schedule before it expires.

### Global scope

Registers the command for every sandbox:

```bash
sbx secret set github \
  --command '<path-to-script>/get-ghapp-token.sh <GitHub App Identity>' \
  --refresh 50m
```

### Sandbox scope

Add `--sandbox <sandbox name>` to the same command to scope it to a single sandbox instead:

```bash
sbx secret set github \
  --command '<path-to-script>/get-ghapp-token.sh <GitHub App Identity>' \
  --refresh 50m \
  --sandbox <sandbox name>
```

### `sbxenv.yaml`

The same secret can be declared per-project in `sbxenv.yaml`:

```yaml
secrets:
  github:
    command: /path/to/get-ghapp-token.sh ${{ env.args.appId }}
    refresh: 50m
```

### Using the Go binary

Point `--command` (or `command:`) at `ghapp-token` instead. Use an absolute path, since the command isn't run from your interactive shell and may not see your `PATH`. `command -v ghapp-token` prints it, whichever way you installed the binary:

```bash
sbx secret set github \
  --command "$(command -v ghapp-token) <GitHub App Identity>" \
  --refresh 50m
```

```yaml
secrets:
  github:
    command: /path/to/ghapp-token ${{ env.args.appId }}   # output of `command -v ghapp-token`
    refresh: 50m
```

Since Installation Access Tokens expire after 1 hour, keep `--refresh`/`refresh` under 60 minutes — 50m leaves a safety margin without re-minting on every single command.

## Automatic Token Refresh (Cron)

`refresh-ghapp-token.sh` mints a fresh installation token via
`get-ghapp-token.sh` and caches it in the macOS Keychain (service
`ghapp-token:<APP_ID>`, account `installation-token`), so other tooling
can read a ready-made token instantly instead of making a live API call on
every use. On failure, the previously cached token is left untouched.

Like `get-ghapp-token.sh`, it takes the App ID via `GITHUB_APP_ID` or as
the first argument (`./refresh-ghapp-token.sh 1234567`) — it needs this up
front to know which per-App Keychain service to write into.

```bash
chmod +x refresh-ghapp-token.sh
```

Installation Access Tokens expire after 1 hour, so a 15-minute cron cadence
is recommended — even if one scheduled run fails outright, the next
successful run is at most ~45 minutes later, leaving a real safety margin
before expiry:

```
*/15 * * * * GITHUB_APP_ID=1234567 /path/to/refresh-ghapp-token.sh >> ~/Library/Logs/refresh-ghapp-token.log 2>&1
```

This crontab line is not installed automatically — add it yourself via
`crontab -e`.

**Cron-specific caveats:**

- cron does not source `~/.zshrc`/`~/.bashrc`. Set `GITHUB_APP_ID` inline on
  the crontab line as shown above, and make sure `openssl`, `curl`, `jq`,
  and `security` are resolvable via cron's default `PATH` (Homebrew paths
  like `/opt/homebrew/bin` are often absent from it).
- `security` needs the login keychain unlocked to operate without a GUI
  prompt. This normally holds for any logged-in user, but a rebooted,
  not-yet-logged-in Mac can cause cron-invoked `security` calls to fail.

### Consuming the pre-warmed token

Downstream tooling can read the cached token directly from Keychain instead
of invoking `get-ghapp-token.sh` synchronously:

```bash
security find-generic-password -a "installation-token" -s "ghapp-token:1234567" -w
```

This is an optional pattern — for example, `claude-gh()` above could
alternatively read the pre-warmed token rather than minting one on every
invocation, trading a small window of possible staleness (up to the cron
interval) for skipping a live API call each time:

```zsh
claude-gh-cached() {
  export GH_TOKEN=$(security find-generic-password -a "installation-token" -s "ghapp-token:1234567" -w)
  export GIT_CONFIG_COUNT=1
  export GIT_CONFIG_KEY_0=credential.helper
  export GIT_CONFIG_VALUE_0='!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f'
  claude "$@"
}
```

## Security

- **Never commit the private key (PEM) to a repository**
- It is strongly recommended to add `*.pem` to `.gitignore`

```gitignore
*.pem
*.key
```

- Installation Access Tokens expire after **1 hour**
- Follow the principle of least privilege and grant only the minimum required permissions to the GitHub App
- Keychain storage (Option A) keeps the PEM off disk after initial import; the base64-encoded Keychain entry and the decoded in-memory PEM are never written to a file
- The cached installation token written by `refresh-ghapp-token.sh` is itself a bearer credential with the same 1-hour lifetime and should be treated with the same care as any token in memory
- Check Go release archives against `sha256sums.txt` before installing (see [Install from a release](#install-from-a-release)). This catches corrupted downloads; it doesn't prove where the archive was built, since the checksums ship in the same release

## Troubleshooting

| Error Message | Cause | Resolution |
|---------------|-------|------------|
| `APP_ID is not set` | `GITHUB_APP_ID` environment variable is not set | Set the environment variable or pass it as an argument |
| `Private key not found` | The PEM is absent from both Keychain (service `ghapp-token:<APP_ID>`, account `pem`) and `GITHUB_APP_PEM_PATH` | Register via `security add-generic-password` (base64-encoded) or fix `GITHUB_APP_PEM_PATH` |
| `Failed to sign JWT. Check the PEM stored in Keychain.` | The Keychain entry isn't valid base64, or decodes to a corrupt/invalid PEM | Re-run the `security add-generic-password -U` command with a fresh `openssl base64 -A` encode of a known-good PEM |
| `Failed to sign JWT. Check your PEM file.` | The PEM file is corrupted or in an invalid format | Regenerate the key from the GitHub App settings page |
| `Failed to reach GitHub API. Check your APP_ID and PEM file.` | Network error, or GitHub rejected the JWT (wrong App ID / PEM pair) | Verify the App ID and PEM combination |
| `No installations found for this GitHub App.` | The App isn't installed anywhere yet | Install the GitHub App from its settings page |
| `No installation found for account '<login>'` | `GITHUB_APP_INSTALLATION_LOGIN` doesn't match any installation's account login | Check the available accounts listed alongside the error, or unset the variable to use the first installation |
| `Failed to obtain access token.` | The token request was rejected (e.g. `GITHUB_APP_INSTALLATION_ID` doesn't belong to this App) or a network error | Check `GITHUB_APP_INSTALLATION_ID`, or unset it to look the installation up |
| `Token not found in response.` | GitHub answered without a token | Retry; check [GitHub status](https://www.githubstatus.com) if it persists |
| `Required command not found: <cmd>` (shell script only) | `openssl`, `curl` or `jq` isn't on `PATH` | Install it, or use the Go binary, which has no runtime dependencies |
| `'security' command not found` (from `refresh-ghapp-token.sh`) | Running on non-macOS | `refresh-ghapp-token.sh` requires the macOS Keychain; not supported elsewhere |
| Cron job silently does nothing / stale token never refreshes | Cron's minimal `PATH`/env doesn't have `GITHUB_APP_ID` or `openssl`/`curl`/`jq` | Set `GITHUB_APP_ID` inline on the crontab line; use absolute paths or extend `PATH` in the crontab |

## License

MIT
