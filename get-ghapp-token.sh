#!/usr/bin/env bash
set -euo pipefail

# ── Logging (optional) ────────────────────────────────
# When GITHUB_APP_LOG_FILE is set, append a timestamped line per step to
# that file. Log output never goes to stdout, since callers do
# TOKEN=$(./get-ghapp-token.sh) — only the bare token belongs there.
# Unset (the default): no log file, no extra overhead.
LOG_FILE="${GITHUB_APP_LOG_FILE:-}"
log() {
  if [[ -n "$LOG_FILE" ]]; then
    printf '%s [get-ghapp-token] %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$LOG_FILE" 2>/dev/null || true
  fi
}

# ── Configuration ─────────────────────────────────────
# APP_ID: GitHub App ID
#   Option 1: Set GITHUB_APP_ID environment variable
#   Option 2: Pass as the first argument
#   e.g. GITHUB_APP_ID=1234567 ./get-token.sh
#        ./get-token.sh 1234567
APP_ID="${GITHUB_APP_ID:-${1:-}}"

# ── Validation ────────────────────────────────────────
if [[ -z "$APP_ID" ]]; then
  log "ERROR: APP_ID is not set"
  echo "Error: APP_ID is not set" >&2
  echo "Usage: GITHUB_APP_ID=<id> $0" >&2
  echo "       $0 <app_id>" >&2
  exit 1
fi

log "Starting token generation for app_id=$APP_ID"

# ── Keychain helpers (macOS only) ─────────────────────
# Silently falls back to the file-based lookup when `security` is
# unavailable (non-macOS) or no Keychain entry is registered. The
# service is namespaced per App ID so multiple GitHub Apps (agents) can
# each keep their own PEM/token without colliding; override with
# GITHUB_APP_KEYCHAIN_SERVICE for a custom label.
KEYCHAIN_SERVICE="${GITHUB_APP_KEYCHAIN_SERVICE:-ghapp-token:${APP_ID}}"
PEM_KEYCHAIN_ACCOUNT="pem"

keychain_get() {
  local account="$1"
  if command -v security &>/dev/null; then
    security find-generic-password -a "$account" -s "$KEYCHAIN_SERVICE" -w 2>/dev/null || true
  fi
}

# PEM: Private key
#   Priority: macOS Keychain (base64-encoded, account "pem")
#            > file at GITHUB_APP_PEM_PATH (or default path)
#   Storing raw multi-line PEM text in Keychain does not round-trip
#   reliably, so the Keychain entry holds base64-encoded content,
#   decoded here in-memory only — never written back out to disk.
PEM_PATH="${GITHUB_APP_PEM_PATH:-$HOME/.config/claude-code-bot/botname.private-key.pem}"

PEM_CONTENT=""
PEM_B64="$(keychain_get "$PEM_KEYCHAIN_ACCOUNT")"
if [[ -n "$PEM_B64" ]]; then
  PEM_CONTENT="$(printf '%s' "$PEM_B64" | openssl base64 -d -A 2>/dev/null || true)"
fi

if [[ -z "$PEM_CONTENT" ]] && [[ ! -f "$PEM_PATH" ]]; then
  log "ERROR: Private key not found (keychain service=$KEYCHAIN_SERVICE, file=$PEM_PATH)"
  echo "Error: Private key not found" >&2
  echo "  Not present in Keychain (account: $PEM_KEYCHAIN_ACCOUNT, service: $KEYCHAIN_SERVICE)" >&2
  echo "  File not found: $PEM_PATH" >&2
  echo "  Override the path with GITHUB_APP_PEM_PATH, or register in Keychain (see README)" >&2
  exit 1
fi

if [[ -n "$PEM_CONTENT" ]]; then
  log "Resolved private key from Keychain (service=$KEYCHAIN_SERVICE)"
else
  log "Resolved private key from file: $PEM_PATH"
fi

for cmd in openssl curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: Required command not found: $cmd"
    echo "Error: Required command not found: $cmd" >&2
    exit 1
  fi
done

# ── JWT generation ────────────────────────────────────
b64url() {
  openssl base64 -A | tr '+/' '-_' | tr -d '='
}

HEADER=$(echo -n '{"alg":"RS256","typ":"JWT"}' | b64url)

NOW=$(date +%s)
PAYLOAD=$(echo -n "{\"iat\":$((NOW - 60)),\"exp\":$((NOW + 600)),\"iss\":\"$APP_ID\"}" | b64url)

if [[ -n "$PEM_CONTENT" ]]; then
  SIG=$(echo -n "${HEADER}.${PAYLOAD}" \
    | openssl dgst -sha256 -sign <(printf '%s\n' "$PEM_CONTENT") 2>/dev/null \
    | b64url) || {
    log "ERROR: Failed to sign JWT using Keychain-stored PEM"
    echo "Error: Failed to sign JWT. Check the PEM stored in Keychain." >&2
    exit 1
  }
else
  SIG=$(echo -n "${HEADER}.${PAYLOAD}" \
    | openssl dgst -sha256 -sign "$PEM_PATH" 2>/dev/null \
    | b64url) || {
    log "ERROR: Failed to sign JWT using PEM file: $PEM_PATH"
    echo "Error: Failed to sign JWT. Check your PEM file." >&2
    exit 1
  }
fi

JWT="${HEADER}.${PAYLOAD}.${SIG}"
log "Generated JWT"

# ── Get Installation ID ───────────────────────────────
# By default the App's first installation is used, which only makes sense
# when the App is installed in exactly one place. If it is installed on
# multiple orgs/repos, target a specific one with:
#   GITHUB_APP_INSTALLATION_ID=<id>       skips the lookup entirely
#   GITHUB_APP_INSTALLATION_LOGIN=<login> filters by account login (org/user)
if [[ -n "${GITHUB_APP_INSTALLATION_ID:-}" ]]; then
  INSTALLATION_ID="$GITHUB_APP_INSTALLATION_ID"
  log "Using installation_id=$INSTALLATION_ID from GITHUB_APP_INSTALLATION_ID"
else
  INSTALLATIONS=$(curl -sf \
    -H "Authorization: Bearer $JWT" \
    -H "Accept: application/vnd.github+json" \
    https://api.github.com/app/installations) || {
    log "ERROR: Failed to reach GitHub API (GET /app/installations)"
    echo "Error: Failed to reach GitHub API. Check your APP_ID and PEM file." >&2
    exit 1
  }

  if [[ -n "${GITHUB_APP_INSTALLATION_LOGIN:-}" ]]; then
    INSTALLATION_ID=$(echo "$INSTALLATIONS" | jq -r --arg login "$GITHUB_APP_INSTALLATION_LOGIN" \
      '.[] | select(.account.login == $login) | .id' | head -n 1)
    if [[ -z "$INSTALLATION_ID" ]]; then
      log "ERROR: No installation found for account '$GITHUB_APP_INSTALLATION_LOGIN'"
      echo "Error: No installation found for account '$GITHUB_APP_INSTALLATION_LOGIN'." >&2
      echo "  Available accounts: $(echo "$INSTALLATIONS" | jq -r '[.[].account.login] | join(", ")')" >&2
      exit 1
    fi
    log "Resolved installation_id=$INSTALLATION_ID via GITHUB_APP_INSTALLATION_LOGIN=$GITHUB_APP_INSTALLATION_LOGIN"
  else
    INSTALLATION_ID=$(echo "$INSTALLATIONS" | jq -r '.[0].id // empty')
    log "Resolved installation_id=$INSTALLATION_ID (first installation returned by the API)"
  fi
fi

if [[ -z "$INSTALLATION_ID" ]]; then
  log "ERROR: No installations found for this GitHub App"
  echo "Error: No installations found for this GitHub App." >&2
  exit 1
fi

# ── Get Installation Access Token ─────────────────────
TOKEN_RESPONSE=$(curl -sf \
  -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/app/installations/$INSTALLATION_ID/access_tokens") || {
  log "ERROR: Failed to obtain access token for installation_id=$INSTALLATION_ID"
  echo "Error: Failed to obtain access token." >&2
  exit 1
}

TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.token // empty')

if [[ -z "$TOKEN" ]]; then
  log "ERROR: Token not found in response"
  echo "Error: Token not found in response." >&2
  exit 1
fi

log "Issued installation access token for installation_id=$INSTALLATION_ID"
echo "$TOKEN"
