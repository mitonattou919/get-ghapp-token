package ghapp

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installFakeSecurity puts a shell-script `security` first on PATH, so the
// test runs the same on Linux and doesn't touch the real Keychain on macOS.
func installFakeSecurity(t *testing.T, body string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s ' \"$@\" > '" + argsFile + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "security"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return argsFile
}

func TestReadKeychainPEM(t *testing.T) {
	pemText := []byte("-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----\n")
	encoded := base64.StdEncoding.EncodeToString(pemText)

	tests := []struct {
		name   string
		script string
		want   []byte
	}{
		{"base64 entry with trailing newline", "printf '%s\\n' '" + encoded + "'", pemText},
		{"entry not found", "exit 44", nil},
		{"empty output", "exit 0", nil},
		{"not base64", "echo '!!not base64!!'", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := installFakeSecurity(t, tt.script)

			got := ReadKeychainPEM("my-service")

			if !bytes.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			args, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.TrimSpace(string(args)), "find-generic-password -a pem -s my-service -w"; got != want {
				t.Errorf("security args = %q, want %q", got, want)
			}
		})
	}

	t.Run("security not on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if got := ReadKeychainPEM("my-service"); got != nil {
			t.Errorf("got %q, want nil", got)
		}
	})
}
