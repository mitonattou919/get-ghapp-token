package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func writeTestPEM(t *testing.T, dir string) (pemBytes []byte, path string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	path = filepath.Join(dir, "app.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return pemBytes, path
}

// apiStub is a fake GitHub API. Zero values mean "happy path".
type apiStub struct {
	listStatus  int
	listBody    string
	tokenStatus int
	tokenBody   string
}

func (s apiStub) server(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations":
			status, body := s.listStatus, s.listBody
			if status == 0 {
				status = http.StatusOK
			}
			if body == "" {
				body = `[{"id":111,"account":{"login":"alice"}},{"id":222,"account":{"login":"bob"}}]`
			}
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, body)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			status, body := s.tokenStatus, s.tokenBody
			if status == 0 {
				status = http.StatusCreated
			}
			if body == "" {
				body = fmt.Sprintf(`{"token":"tok-%s"}`, strings.Split(r.URL.Path, "/")[3])
			}
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type result struct {
	code           int
	stdout, stderr string
	calls          []string
}

func runWith(t *testing.T, args []string, vars map[string]string, keychain []byte, api apiStub) result {
	t.Helper()
	var calls []string
	srv := api.server(t, &calls)
	var stdout, stderr bytes.Buffer
	code := run(env{
		args:     args,
		getenv:   func(k string) string { return vars[k] },
		stdout:   &stdout,
		stderr:   &stderr,
		baseURL:  srv.URL,
		keychain: func(string) []byte { return keychain },
		now:      func() time.Time { return fixedNow },
	})
	return result{code, stdout.String(), stderr.String(), calls}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	goodPEM, goodPath := writeTestPEM(t, dir)
	badPath := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(badPath, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(dir, "missing.pem")
	defaultPath := dir + "/.config/claude-code-bot/botname.private-key.pem"

	const (
		list  = "GET /app/installations"
		token = "POST /app/installations/%s/access_tokens"
	)

	tests := []struct {
		name       string
		args       []string
		env        map[string]string // merged over GITHUB_APP_ID=4242, GITHUB_APP_PEM_PATH=goodPath, HOME=dir
		keychain   []byte
		api        apiStub
		wantCode   int
		wantStdout string
		wantStderr string
		wantCalls  []string
	}{
		{
			name:       "first installation",
			wantStdout: "tok-111\n",
			wantCalls:  []string{list, fmt.Sprintf(token, "111")},
		},
		{
			name:       "installation selected by login",
			env:        map[string]string{"GITHUB_APP_INSTALLATION_LOGIN": "bob"},
			wantStdout: "tok-222\n",
			wantCalls:  []string{list, fmt.Sprintf(token, "222")},
		},
		{
			name:       "explicit installation id skips lookup",
			env:        map[string]string{"GITHUB_APP_INSTALLATION_ID": "999"},
			wantStdout: "tok-999\n",
			wantCalls:  []string{fmt.Sprintf(token, "999")},
		},
		{
			name:       "app id from first argument",
			args:       []string{"ghapp-token", "4242"},
			env:        map[string]string{"GITHUB_APP_ID": ""},
			wantStdout: "tok-111\n",
			wantCalls:  []string{list, fmt.Sprintf(token, "111")},
		},
		{
			name:       "keychain PEM takes precedence over file",
			env:        map[string]string{"GITHUB_APP_PEM_PATH": missingPath},
			keychain:   goodPEM,
			wantStdout: "tok-111\n",
			wantCalls:  []string{list, fmt.Sprintf(token, "111")},
		},
		{
			name:       "app id not set",
			env:        map[string]string{"GITHUB_APP_ID": ""},
			wantCode:   1,
			wantStderr: "Error: APP_ID is not set\nUsage: GITHUB_APP_ID=<id> ghapp-token\n       ghapp-token <app_id>\n",
		},
		{
			name:     "private key not found at default path",
			env:      map[string]string{"GITHUB_APP_PEM_PATH": ""},
			wantCode: 1,
			wantStderr: "Error: Private key not found\n" +
				"  Not present in Keychain (account: pem, service: ghapp-token:4242)\n" +
				"  File not found: " + defaultPath + "\n" +
				"  Override the path with GITHUB_APP_PEM_PATH, or register in Keychain (see README)\n",
		},
		{
			name:     "private key not found with custom keychain service",
			env:      map[string]string{"GITHUB_APP_PEM_PATH": missingPath, "GITHUB_APP_KEYCHAIN_SERVICE": "custom"},
			wantCode: 1,
			wantStderr: "Error: Private key not found\n" +
				"  Not present in Keychain (account: pem, service: custom)\n" +
				"  File not found: " + missingPath + "\n" +
				"  Override the path with GITHUB_APP_PEM_PATH, or register in Keychain (see README)\n",
		},
		{
			name:       "corrupt PEM file",
			env:        map[string]string{"GITHUB_APP_PEM_PATH": badPath},
			wantCode:   1,
			wantStderr: "Error: Failed to sign JWT. Check your PEM file.\n",
		},
		{
			name:       "corrupt keychain PEM",
			keychain:   []byte("garbage"),
			wantCode:   1,
			wantStderr: "Error: Failed to sign JWT. Check the PEM stored in Keychain.\n",
		},
		{
			name:       "installations request fails",
			api:        apiStub{listStatus: http.StatusUnauthorized},
			wantCode:   1,
			wantStderr: "Error: Failed to reach GitHub API. Check your APP_ID and PEM file.\n",
			wantCalls:  []string{list},
		},
		{
			name:       "login does not match",
			env:        map[string]string{"GITHUB_APP_INSTALLATION_LOGIN": "carol"},
			wantCode:   1,
			wantStderr: "Error: No installation found for account 'carol'.\n  Available accounts: alice, bob\n",
			wantCalls:  []string{list},
		},
		{
			name:       "no installations",
			api:        apiStub{listBody: "[]"},
			wantCode:   1,
			wantStderr: "Error: No installations found for this GitHub App.\n",
			wantCalls:  []string{list},
		},
		{
			name:       "token request fails",
			api:        apiStub{tokenStatus: http.StatusInternalServerError},
			wantCode:   1,
			wantStderr: "Error: Failed to obtain access token.\n",
			wantCalls:  []string{list, fmt.Sprintf(token, "111")},
		},
		{
			name:       "token missing from response",
			api:        apiStub{tokenBody: `{"expires_at":"2026-01-01T00:00:00Z"}`},
			wantCode:   1,
			wantStderr: "Error: Token not found in response.\n",
			wantCalls:  []string{list, fmt.Sprintf(token, "111")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{"GITHUB_APP_ID": "4242", "GITHUB_APP_PEM_PATH": goodPath, "HOME": dir}
			for k, v := range tt.env {
				vars[k] = v
			}
			args := tt.args
			if args == nil {
				args = []string{"ghapp-token"}
			}

			got := runWith(t, args, vars, tt.keychain, tt.api)

			if got.code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got.code, tt.wantCode)
			}
			if got.stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", got.stdout, tt.wantStdout)
			}
			if got.stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got.stderr, tt.wantStderr)
			}
			if !reflect.DeepEqual(got.calls, tt.wantCalls) {
				t.Errorf("API calls = %v, want %v", got.calls, tt.wantCalls)
			}
		})
	}
}

func TestRunLogFile(t *testing.T) {
	dir := t.TempDir()
	_, pemPath := writeTestPEM(t, dir)
	logPath := filepath.Join(dir, "ghapp.log")
	vars := map[string]string{
		"GITHUB_APP_ID":                 "4242",
		"GITHUB_APP_PEM_PATH":           pemPath,
		"GITHUB_APP_LOG_FILE":           logPath,
		"GITHUB_APP_INSTALLATION_LOGIN": "bob",
	}

	got := runWith(t, []string{"ghapp-token"}, vars, nil, apiStub{})
	if got.code != 0 || got.stdout != "tok-222\n" || got.stderr != "" {
		t.Fatalf("run = %+v; logging must not touch stdout/stderr", got)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"2026-01-02T03:04:05Z [get-ghapp-token] Starting token generation for app_id=4242",
		"2026-01-02T03:04:05Z [get-ghapp-token] Resolved private key from file: " + pemPath,
		"2026-01-02T03:04:05Z [get-ghapp-token] Generated JWT",
		"2026-01-02T03:04:05Z [get-ghapp-token] Resolved installation_id=222 via GITHUB_APP_INSTALLATION_LOGIN=bob",
		"2026-01-02T03:04:05Z [get-ghapp-token] Issued installation access token for installation_id=222",
	}, "\n") + "\n"
	if string(logBytes) != want {
		t.Errorf("log =\n%s\nwant\n%s", logBytes, want)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log file mode = %o, want 600", perm)
	}

	// A second run appends rather than truncating.
	runWith(t, []string{"ghapp-token"}, vars, nil, apiStub{})
	logBytes, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(logBytes), want) || len(logBytes) != 2*len(want) {
		t.Error("second run did not append to the existing log")
	}
}
