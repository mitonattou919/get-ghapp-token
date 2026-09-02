#!/usr/bin/env bash
set -euo pipefail

# ── Keychain helpers (macOS only) ─────────────────────
# Silently falls back to the file-based lookup when `security` is
# unavailable (non-macOS) or no Keychain entry is registered.
KEYCHAIN_SERVICE="claude-code-bot"
PEM_KEYCHAIN_ACCOUNT="github-app-pem"

keychain_get() {
  local account="$1"
  if command -v security &>/dev/null; then
    security find-generic-password -a "$account" -s "$KEYCHAIN_SERVICE" -w 2>/dev/null || true
  fi
}

# ── Configuration ─────────────────────────────────────
# APP_ID: GitHub App ID
#   Option 1: Set GITHUB_APP_ID environment variable
#   Option 2: Pass as the first argument
#   e.g. GITHUB_APP_ID=1234567 ./get-token.sh
#        ./get-token.sh 1234567
APP_ID="${GITHUB_APP_ID:-${1:-}}"

# PEM: Private key
#   Priority: macOS Keychain (base64-encoded, account "github-app-pem")
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

# ── Validation ────────────────────────────────────────
if [[ -z "$APP_ID" ]]; then
  echo "Error: APP_ID is not set" >&2
  echo "Usage: GITHUB_APP_ID=<id> $0" >&2
  echo "       $0 <app_id>" >&2
  exit 1
fi

if [[ -z "$PEM_CONTENT" ]] && [[ ! -f "$PEM_PATH" ]]; then
  echo "Error: Private key not found" >&2
  echo "  Not present in Keychain (account: $PEM_KEYCHAIN_ACCOUNT, service: $KEYCHAIN_SERVICE)" >&2
  echo "  File not found: $PEM_PATH" >&2
  echo "  Override the path with GITHUB_APP_PEM_PATH, or register in Keychain (see README)" >&2
  exit 1
fi

for cmd in openssl curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
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
    echo "Error: Failed to sign JWT. Check the PEM stored in Keychain." >&2
    exit 1
  }
else
  SIG=$(echo -n "${HEADER}.${PAYLOAD}" \
    | openssl dgst -sha256 -sign "$PEM_PATH" 2>/dev/null \
    | b64url) || {
    echo "Error: Failed to sign JWT. Check your PEM file." >&2
    exit 1
  }
fi

JWT="${HEADER}.${PAYLOAD}.${SIG}"

# ── Get Installation ID ───────────────────────────────
INSTALLATIONS=$(curl -sf \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github+json" \
  https://api.github.com/app/installations) || {
  echo "Error: Failed to reach GitHub API. Check your APP_ID and PEM file." >&2
  exit 1
}

INSTALLATION_ID=$(echo "$INSTALLATIONS" | jq -r '.[0].id // empty')

if [[ -z "$INSTALLATION_ID" ]]; then
  echo "Error: No installations found for this GitHub App." >&2
  exit 1
fi

# ── Get Installation Access Token ─────────────────────
TOKEN_RESPONSE=$(curl -sf \
  -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/app/installations/$INSTALLATION_ID/access_tokens") || {
  echo "Error: Failed to obtain access token." >&2
  exit 1
}

TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.token // empty')

if [[ -z "$TOKEN" ]]; then
  echo "Error: Token not found in response." >&2
  exit 1
fi

echo "$TOKEN"
