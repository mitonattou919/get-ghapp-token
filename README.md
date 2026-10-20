# GitHub Apps Installation Token Generator

A shell script that generates an Installation Access Token using a GitHub App's private key (PEM).
Useful for obtaining bot tokens when operating GitHub from tools like Claude Code.

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

## Prerequisites

- bash
- openssl
- curl
- jq
- GitHub App must be installed on the target repository

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

## Troubleshooting

| Error Message | Cause | Resolution |
|---------------|-------|------------|
| `APP_ID is not set` | `GITHUB_APP_ID` environment variable is not set | Set the environment variable or pass it as an argument |
| `Private key not found` | The PEM is absent from both Keychain (service `ghapp-token:<APP_ID>`, account `pem`) and `GITHUB_APP_PEM_PATH` | Register via `security add-generic-password` (base64-encoded) or fix `GITHUB_APP_PEM_PATH` |
| `Failed to sign JWT. Check the PEM stored in Keychain.` | The Keychain entry isn't valid base64, or decodes to a corrupt/invalid PEM | Re-run the `security add-generic-password -U` command with a fresh `openssl base64 -A` encode of a known-good PEM |
| `Failed to sign JWT. Check your PEM file.` | The PEM file is corrupted or in an invalid format | Regenerate the key from the GitHub App settings page |
| `Failed to access GitHub API` | Network error or invalid JWT | Verify the App ID and PEM combination |
| `Installation not found` | The App is not installed on the target repository | Install the GitHub App from its settings page |
| `No installation found for account '<login>'` | `GITHUB_APP_INSTALLATION_LOGIN` doesn't match any installation's account login | Check the available accounts listed alongside the error, or unset the variable to use the first installation |
| `'security' command not found` (from `refresh-ghapp-token.sh`) | Running on non-macOS | `refresh-ghapp-token.sh` requires the macOS Keychain; not supported elsewhere |
| Cron job silently does nothing / stale token never refreshes | Cron's minimal `PATH`/env doesn't have `GITHUB_APP_ID` or `openssl`/`curl`/`jq` | Set `GITHUB_APP_ID` inline on the crontab line; use absolute paths or extend `PATH` in the crontab |

## License

MIT
