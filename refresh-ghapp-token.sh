#!/usr/bin/env bash
set -euo pipefail

# refresh-ghapp-token.sh
#
# Mints a fresh installation token via get-ghapp-token.sh and caches it
# in the macOS Keychain. Intended to run periodically via cron (see
# README) — this script does not install a cron entry itself.
#
# On failure, the previously cached token in Keychain is left untouched.

# ── Logging (optional) ────────────────────────────────
# See get-ghapp-token.sh for the GITHUB_APP_LOG_FILE convention. It is
# also exported here so get-ghapp-token.sh's own steps land in the same
# log file when this script invokes it below.
LOG_FILE="${GITHUB_APP_LOG_FILE:-}"
log() {
  if [[ -n "$LOG_FILE" ]]; then
    printf '%s [refresh-ghapp-token] %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$LOG_FILE" 2>/dev/null || true
  fi
}

# APP_ID: GitHub App ID
#   Option 1: Set GITHUB_APP_ID environment variable
#   Option 2: Pass as the first argument
#   e.g. GITHUB_APP_ID=1234567 ./refresh-ghapp-token.sh
#        ./refresh-ghapp-token.sh 1234567
APP_ID="${GITHUB_APP_ID:-${1:-}}"

if [[ -z "$APP_ID" ]]; then
  log "ERROR: APP_ID is not set"
  echo "Error: APP_ID is not set" >&2
  echo "Usage: GITHUB_APP_ID=<id> $0" >&2
  echo "       $0 <app_id>" >&2
  exit 1
fi

log "Starting refresh for app_id=$APP_ID"

# Must match the service naming get-ghapp-token.sh derives, so the
# cached token lands under the same per-App-ID namespace as the PEM.
KEYCHAIN_SERVICE="${GITHUB_APP_KEYCHAIN_SERVICE:-ghapp-token:${APP_ID}}"
TOKEN_KEYCHAIN_ACCOUNT="installation-token"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
GET_TOKEN_SCRIPT="${SCRIPT_DIR}/get-ghapp-token.sh"

if ! command -v security &>/dev/null; then
  log "ERROR: 'security' command not found"
  echo "Error: 'security' command not found. This script only supports macOS Keychain." >&2
  exit 1
fi

if [[ ! -x "$GET_TOKEN_SCRIPT" ]]; then
  log "ERROR: get-ghapp-token.sh not found or not executable at $GET_TOKEN_SCRIPT"
  echo "Error: get-ghapp-token.sh not found or not executable at $GET_TOKEN_SCRIPT" >&2
  exit 1
fi

TOKEN=$(GITHUB_APP_ID="$APP_ID" "$GET_TOKEN_SCRIPT") || {
  log "ERROR: Failed to mint a new installation token; cached token left untouched"
  echo "Error: Failed to mint a new installation token. Existing cached token in Keychain left untouched." >&2
  exit 1
}

if [[ -z "$TOKEN" ]]; then
  log "ERROR: Received an empty token; cached token left untouched"
  echo "Error: Received an empty token. Existing cached token in Keychain left untouched." >&2
  exit 1
fi

security add-generic-password -U \
  -a "$TOKEN_KEYCHAIN_ACCOUNT" -s "$KEYCHAIN_SERVICE" -w "$TOKEN"

log "Cached fresh installation token in Keychain (service=$KEYCHAIN_SERVICE)"
echo "Cached fresh installation token in Keychain (service: $KEYCHAIN_SERVICE)." >&2
