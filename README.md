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

```bash
security add-generic-password -U \
  -a "github-app-pem" -s "claude-code-bot" \
  -w "$(openssl base64 -A < /path/to/your-app.private-key.pem)"
```

`-U` makes this idempotent — safe to re-run when rotating the key.

The App ID is not stored in Keychain; it still must be provided via
`GITHUB_APP_ID` or a CLI argument (see below).

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

The App ID can be found at `General > App ID` in the GitHub App settings page.

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
| Private key | macOS Keychain (`github-app-pem`, base64-encoded) → file at `GITHUB_APP_PEM_PATH` |

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

## Automatic Token Refresh (Cron)

`refresh-ghapp-token.sh` mints a fresh installation token via
`get-ghapp-token.sh` and caches it in the macOS Keychain (account
`github-installation-token`, service `claude-code-bot`), so other tooling
can read a ready-made token instantly instead of making a live API call on
every use. On failure, the previously cached token is left untouched.

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
security find-generic-password -a "github-installation-token" -s "claude-code-bot" -w
```

This is an optional pattern — for example, `claude-gh()` above could
alternatively read the pre-warmed token rather than minting one on every
invocation, trading a small window of possible staleness (up to the cron
interval) for skipping a live API call each time:

```zsh
claude-gh-cached() {
  export GH_TOKEN=$(security find-generic-password -a "github-installation-token" -s "claude-code-bot" -w)
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
| `Private key not found` | The PEM is absent from both Keychain and `GITHUB_APP_PEM_PATH` | Register via `security add-generic-password` (base64-encoded) or fix `GITHUB_APP_PEM_PATH` |
| `Failed to sign JWT. Check the PEM stored in Keychain.` | The Keychain entry isn't valid base64, or decodes to a corrupt/invalid PEM | Re-run the `security add-generic-password -U` command with a fresh `openssl base64 -A` encode of a known-good PEM |
| `Failed to sign JWT. Check your PEM file.` | The PEM file is corrupted or in an invalid format | Regenerate the key from the GitHub App settings page |
| `Failed to access GitHub API` | Network error or invalid JWT | Verify the App ID and PEM combination |
| `Installation not found` | The App is not installed on the target repository | Install the GitHub App from its settings page |
| `'security' command not found` (from `refresh-ghapp-token.sh`) | Running on non-macOS | `refresh-ghapp-token.sh` requires the macOS Keychain; not supported elsewhere |
| Cron job silently does nothing / stale token never refreshes | Cron's minimal `PATH`/env doesn't have `GITHUB_APP_ID` or `openssl`/`curl`/`jq` | Set `GITHUB_APP_ID` inline on the crontab line; use absolute paths or extend `PATH` in the crontab |

## License

MIT
