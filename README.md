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

### Option A: macOS Keychain (Recommended)

Store both the App ID and private key directly in the macOS Keychain.
No PEM file on disk, no environment variables needed.

```bash
# Register App ID
security add-generic-password \
  -a "github-app-id" -s "claude-code-bot" -w "1234567"

# Register private key
security add-generic-password \
  -a "github-app-pem" -s "claude-code-bot" \
  -w "$(cat /path/to/your-app.private-key.pem)"
```

The App ID can be found at `General > App ID` in the GitHub App settings page.

To update an existing entry, add the `-U` flag:

```bash
security add-generic-password -U \
  -a "github-app-id" -s "claude-code-bot" -w "1234567"
```

### Option B: File-based

#### 1. Place the Private Key

Place the private key (`.pem` file) generated from the GitHub App settings page.

```bash
mkdir -p ~/.config/claude-code-bot
cp your-app.private-key.pem ~/.config/claude-code-bot/botname.private-key.pem
chmod 600 ~/.config/claude-code-bot/botname.private-key.pem
```

#### 2. Set Environment Variables

Set the following environment variables.

| Variable | Description | Default |
|----------|-------------|---------|
| `GITHUB_APP_ID` | GitHub App's App ID | (required if not in Keychain) |
| `GITHUB_APP_PEM_PATH` | Path to the private key file | `~/.config/claude-code-bot/botname.private-key.pem` |

The App ID can be found at `General > App ID` in the GitHub App settings page.

##### Setting via shell

```bash
export GITHUB_APP_ID=1234567
export GITHUB_APP_PEM_PATH=~/.config/claude-code-bot/botname.private-key.pem
```

##### Setting via Claude Code's settings.json

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
|------------|---------------|
| App ID | `GITHUB_APP_ID` env var → CLI argument → macOS Keychain |
| Private key | macOS Keychain → file at `GITHUB_APP_PEM_PATH` |

## Usage

```bash
# Credentials stored in Keychain (no arguments needed)
./get-ghapp-token.sh

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

## Security

- **Never commit the private key (PEM) to a repository**
- It is strongly recommended to add `*.pem` to `.gitignore`

```gitignore
*.pem
*.key
```

- Using macOS Keychain (Option A) is recommended: the PEM never needs to touch the filesystem after initial import
- Installation Access Tokens expire after **1 hour**
- Follow the principle of least privilege and grant only the minimum required permissions to the GitHub App

## Troubleshooting

| Error Message | Cause | Resolution |
|---------------|-------|------------|
| `APP_ID is not set` | App ID not found in env var, argument, or Keychain | Set `GITHUB_APP_ID`, pass as argument, or store in Keychain |
| `Private key not found` | PEM not found in Keychain or at the file path | Store PEM in Keychain or check `GITHUB_APP_PEM_PATH` |
| `Failed to sign JWT` | The PEM is corrupted or in an invalid format | Regenerate the key from the GitHub App settings page |
| `Failed to reach GitHub API` | Network error or invalid JWT | Verify the App ID and PEM combination |
| `No installations found` | The App is not installed on the target repository | Install the GitHub App from its settings page |

## License

MIT
