package ghapp

import (
	"encoding/base64"
	"os/exec"
	"strings"
)

const PEMKeychainAccount = "pem"

// ReadKeychainPEM returns nil when `security` is unavailable (non-macOS), the
// entry is missing, or it isn't valid base64, so callers fall back to the file.
// The entry holds base64 because raw multi-line PEM doesn't round-trip in Keychain.
func ReadKeychainPEM(service string) []byte {
	if _, err := exec.LookPath("security"); err != nil {
		return nil
	}
	out, err := exec.Command("security", "find-generic-password", "-a", PEMKeychainAccount, "-s", service, "-w").Output()
	if err != nil {
		return nil
	}
	encoded := strings.TrimSpace(string(out))
	if encoded == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return nil
	}
	return decoded
}
