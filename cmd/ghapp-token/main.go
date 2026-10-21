// Command ghapp-token prints a GitHub App installation access token to stdout.
// It is a drop-in port of get-ghapp-token.sh: same env vars, stderr messages
// and exit codes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mitonattou919/get-ghapp-token/internal/ghapp"
)

type env struct {
	args     []string
	getenv   func(string) string
	stdout   io.Writer
	stderr   io.Writer
	baseURL  string
	keychain func(service string) []byte
	now      func() time.Time
}

func main() {
	os.Exit(run(env{
		args:     os.Args,
		getenv:   os.Getenv,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		baseURL:  ghapp.DefaultBaseURL,
		keychain: ghapp.ReadKeychainPEM,
		now:      time.Now,
	}))
}

// logger appends to GITHUB_APP_LOG_FILE. It never writes to stdout, since
// callers capture stdout as the token.
type logger struct {
	path string
	now  func() time.Time
}

func (l logger) printf(format string, a ...any) {
	if l.path == "" {
		return
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s [get-ghapp-token] %s\n", l.now().UTC().Format("2006-01-02T15:04:05Z"), fmt.Sprintf(format, a...))
}

func run(e env) int {
	log := logger{path: e.getenv("GITHUB_APP_LOG_FILE"), now: e.now}
	errorf := func(format string, a ...any) { _, _ = fmt.Fprintf(e.stderr, format+"\n", a...) }

	appID := e.getenv("GITHUB_APP_ID")
	if appID == "" && len(e.args) > 1 {
		appID = e.args[1]
	}
	if appID == "" {
		log.printf("ERROR: APP_ID is not set")
		errorf("Error: APP_ID is not set")
		errorf("Usage: GITHUB_APP_ID=<id> %s", e.args[0])
		errorf("       %s <app_id>", e.args[0])
		return 1
	}

	log.printf("Starting token generation for app_id=%s", appID)

	service := e.getenv("GITHUB_APP_KEYCHAIN_SERVICE")
	if service == "" {
		service = "ghapp-token:" + appID
	}
	pemPath := e.getenv("GITHUB_APP_PEM_PATH")
	if pemPath == "" {
		pemPath = e.getenv("HOME") + "/.config/claude-code-bot/botname.private-key.pem"
	}

	pemBytes := e.keychain(service)
	fromKeychain := pemBytes != nil
	if !fromKeychain {
		if info, err := os.Stat(pemPath); err != nil || !info.Mode().IsRegular() {
			log.printf("ERROR: Private key not found (keychain service=%s, file=%s)", service, pemPath)
			errorf("Error: Private key not found")
			errorf("  Not present in Keychain (account: %s, service: %s)", ghapp.PEMKeychainAccount, service)
			errorf("  File not found: %s", pemPath)
			errorf("  Override the path with GITHUB_APP_PEM_PATH, or register in Keychain (see README)")
			return 1
		}
	}

	if fromKeychain {
		log.printf("Resolved private key from Keychain (service=%s)", service)
	} else {
		log.printf("Resolved private key from file: %s", pemPath)
	}

	jwt, err := signJWT(appID, pemBytes, pemPath, e.now())
	if err != nil {
		if fromKeychain {
			log.printf("ERROR: Failed to sign JWT using Keychain-stored PEM")
			errorf("Error: Failed to sign JWT. Check the PEM stored in Keychain.")
		} else {
			log.printf("ERROR: Failed to sign JWT using PEM file: %s", pemPath)
			errorf("Error: Failed to sign JWT. Check your PEM file.")
		}
		return 1
	}
	log.printf("Generated JWT")

	ctx := context.Background()
	client := ghapp.NewClient(jwt)
	client.BaseURL = e.baseURL

	installationID := e.getenv("GITHUB_APP_INSTALLATION_ID")
	if installationID != "" {
		log.printf("Using installation_id=%s from GITHUB_APP_INSTALLATION_ID", installationID)
	} else {
		installs, err := client.ListInstallations(ctx)
		if err != nil {
			log.printf("ERROR: Failed to reach GitHub API (GET /app/installations)")
			errorf("Error: Failed to reach GitHub API. Check your APP_ID and PEM file.")
			return 1
		}

		login := e.getenv("GITHUB_APP_INSTALLATION_LOGIN")
		installationID, err = ghapp.SelectInstallation(installs, login)
		var noMatch *ghapp.NoMatchingLoginError
		switch {
		case errors.As(err, &noMatch):
			log.printf("ERROR: No installation found for account '%s'", login)
			errorf("Error: No installation found for account '%s'.", login)
			errorf("  Available accounts: %s", strings.Join(noMatch.Available, ", "))
			return 1
		case errors.Is(err, ghapp.ErrNoInstallations):
			log.printf("ERROR: No installations found for this GitHub App")
			errorf("Error: No installations found for this GitHub App.")
			return 1
		case login != "":
			log.printf("Resolved installation_id=%s via GITHUB_APP_INSTALLATION_LOGIN=%s", installationID, login)
		default:
			log.printf("Resolved installation_id=%s (first installation returned by the API)", installationID)
		}
	}

	token, err := client.CreateAccessToken(ctx, installationID)
	switch {
	case errors.Is(err, ghapp.ErrTokenMissing):
		log.printf("ERROR: Token not found in response")
		errorf("Error: Token not found in response.")
		return 1
	case err != nil:
		log.printf("ERROR: Failed to obtain access token for installation_id=%s", installationID)
		errorf("Error: Failed to obtain access token.")
		return 1
	}

	log.printf("Issued installation access token for installation_id=%s", installationID)
	if _, err := fmt.Fprintln(e.stdout, token); err != nil {
		return 1
	}
	return 0
}

func signJWT(appID string, pemBytes []byte, pemPath string, now time.Time) (string, error) {
	if pemBytes == nil {
		var err error
		if pemBytes, err = os.ReadFile(pemPath); err != nil {
			return "", err
		}
	}
	key, err := ghapp.ParsePrivateKey(pemBytes)
	if err != nil {
		return "", err
	}
	return ghapp.NewJWT(appID, key, now)
}
