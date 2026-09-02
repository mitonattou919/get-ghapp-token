#!/usr/bin/env bash
set -euo pipefail

# refresh-ghapp-token.sh
#
# Mints a fresh installation token via get-ghapp-token.sh and caches it
# in the macOS Keychain. Intended to run periodically via cron (see
# README) — this script does not install a cron entry itself.
#
# On failure, the previously cached token in Keychain is left untouched.

KEYCHAIN_SERVICE="claude-code-bot"
TOKEN_KEYCHAIN_ACCOUNT="github-installation-token"

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

TOKEN=$("$GET_TOKEN_SCRIPT" "$@") || {
  echo "Error: Failed to mint a new installation token. Existing cached token in Keychain left untouched." >&2
  exit 1
}

if [[ -z "$TOKEN" ]]; then
  echo "Error: Received an empty token. Existing cached token in Keychain left untouched." >&2
  exit 1
fi

security add-generic-password -U \
  -a "$TOKEN_KEYCHAIN_ACCOUNT" -s "$KEYCHAIN_SERVICE" -w "$TOKEN"

echo "Cached fresh installation token in Keychain (account: $TOKEN_KEYCHAIN_ACCOUNT)." >&2
