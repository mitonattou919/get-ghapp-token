# Review guide

Repo-specific points for reviewers, human or automated.

## Shell parity

`cmd/ghapp-token` (Go) is a drop-in replacement for `get-ghapp-token.sh`. A change to either must keep them in sync on:

- environment variable names and the App ID argument
- lookup priority (PEM: Keychain, then file; installation: ID, then login, then first)
- stderr message text, `GITHUB_APP_LOG_FILE` lines and exit codes
- stdout carrying only the token

The `TestRun` table in `cmd/ghapp-token/main_test.go` is the executable form of this contract. Behaviour changes need a matching table change and, where user-visible, a README update (Troubleshooting lists the exact messages).

## Credentials

- PEM contents, JWTs and installation tokens must never reach logs, errors or files.
- The Keychain is read-only from Go; the PEM is held in memory only.

## Dependencies

The Go code uses the standard library only. Flag any new `require` in `go.mod`.

## Workflows

- Pin every action by full commit SHA with a version comment.
- Declare least-privilege `permissions`, at job level for anything beyond `contents: read`.
- `persist-credentials: false` on checkout.
- Jobs that hold secrets must not run code from the PR.
- Keep `.goreleaser.yaml` reproducible (`-trimpath`, no cgo, fixed mtimes/owners), and keep releases draft until attested.
