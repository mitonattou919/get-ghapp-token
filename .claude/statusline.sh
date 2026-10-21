#!/usr/bin/env bash

input="$(cat)"

# Call jq only once and grab everything together (using \x1f as the
# delimiter instead of tabs, since empty fields would otherwise get
# squeezed out and shift the columns)
IFS=$'\x1f' read -r model cwd context pr five week < <(
  jq -r '[
    .model.display_name // "-",
    .workspace.current_dir // "-",
    .context_window.used_percentage // 0,
    .pr.number // "",
    .rate_limits.five_hour.used_percentage // "",
    .rate_limits.seven_day.used_percentage // ""
  ] | map(tostring) | join("\u001f")' <<<"$input"
)

repo="${cwd##*/}"

# Show the short commit hash when in detached HEAD state
branch="$(git -C "$cwd" --no-optional-locks branch --show-current 2>/dev/null)"
if [ -z "$branch" ]; then
  branch="$(git -C "$cwd" --no-optional-locks rev-parse --short HEAD 2>/dev/null)"
fi
if [ -n "$branch" ]; then
  repo="$repo ($branch)"
fi

gh_identity="${SBX_GITHUB_IDENTITY:-}"
if [ -z "$gh_identity" ] && command -v gh >/dev/null 2>&1; then
  cache="${XDG_CACHE_HOME:-$HOME/.cache}/claude-statusline/gh-login"
  # Only refetch when the cache is missing, empty, or older than 1 hour
  # (using GraphQL since REST /user returns 403 with an app token)
  if [ -z "$(find "$cache" -mmin -60 -size +0 2>/dev/null)" ]; then
    mkdir -p "${cache%/*}"
    # macOS has no timeout, so try gtimeout, and run without a timeout
    # at all if neither is available
    to=()
    if command -v timeout >/dev/null 2>&1; then
      to=(timeout 3)
    elif command -v gtimeout >/dev/null 2>&1; then
      to=(gtimeout 3)
    fi
    "${to[@]}" gh api graphql -f query='{viewer{login}}' --jq .data.viewer.login >"$cache" 2>/dev/null || : >"$cache"
  fi
  gh_identity="$(cat "$cache" 2>/dev/null)"
fi

printf '[%s] %s | gh:%s | ctx:%s%%' \
  "$model" \
  "$repo" \
  "${gh_identity:-unknown}" \
  "${context%.*}"

if [ -n "$five" ]; then
  printf ' | 5h:%s%%' "${five%.*}"
fi

if [ -n "$week" ]; then
  printf ' | 7d:%s%%' "${week%.*}"
fi

if [ -n "$pr" ]; then
  printf ' | pr#%s' "$pr"
fi

printf '\n'