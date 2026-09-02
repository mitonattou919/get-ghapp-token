#!/usr/bin/env bash
set -euo pipefail

# refresh-ghapp-token.sh
#
# Mints a fresh installation token via get-ghapp-token.sh and caches it
# in the macOS Keychain. Intended to run periodically via cron (see
# README) — this script does not install a cron entry itself.
#
# On failure, the previously cached token in Keychain is left untouched.

# APP_ID: GitHub App ID
#   Option 1: Set GITHUB_APP_ID environment variable
#   Option 2: Pass as the first argument
#   e.g. GITHUB_APP_ID=1234567 ./refresh-ghapp-token.sh
#        ./refresh-ghapp-token.sh 1234567
APP_ID="${GITHUB_APP_ID:-${1:-}}"

if [[ -z "$APP_ID" ]]; then
  echo "Error: APP_ID is not set" >&2
  echo "Usage: GITHUB_APP_ID=<id> $0" >&2
  echo "       $0 <app_id>" >&2
  exit 1
fi

# Must match the service naming get-ghapp-token.sh derives, so the
# cached token lands under the same per-App-ID namespace as the PEM.
KEYCHAIN_SERVICE="${GITHUB_APP_KEYCHAIN_SERVICE:-ghapp-token:${APP_ID}}"
TOKEN_KEYCHAIN_ACCOUNT="installation-token"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
GET_TOKEN_SCRIPT="${SCRIPT_DIR}/get-ghapp-token.sh"

if ! command -v security &>/dev/null; then
  echo "Error: 'security' command not found. This script only supports macOS Keychain." >&2
  exit 1
fi

if [[ ! -x "$GET_TOKEN_SCRIPT" ]]; then
  echo "Error: get-ghapp-token.sh not found or not executable at $GET_TOKEN_SCRIPT" >&2
  exit 1
fi

TOKEN=$(GITHUB_APP_ID="$APP_ID" "$GET_TOKEN_SCRIPT") || {
  echo "Error: Failed to mint a new installation token. Existing cached token in Keychain left untouched." >&2
  exit 1
}

if [[ -z "$TOKEN" ]]; then
  echo "Error: Received an empty token. Existing cached token in Keychain left untouched." >&2
  exit 1
fi

security add-generic-password -U \
  -a "$TOKEN_KEYCHAIN_ACCOUNT" -s "$KEYCHAIN_SERVICE" -w "$TOKEN"

echo "Cached fresh installation token in Keychain (service: $KEYCHAIN_SERVICE)." >&2
