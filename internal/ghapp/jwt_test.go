package ghapp

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	keyOnce sync.Once
	key     *rsa.PrivateKey
	keyErr  error
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	keyOnce.Do(func() { key, keyErr = rsa.GenerateKey(rand.Reader, 2048) })
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	return key
}

func pemEncode(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

func TestParsePrivateKey(t *testing.T) {
	k := testKey(t)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPKCS8, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		in      []byte
		wantErr bool
	}{
		{"PKCS#1", pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k)), false},
		{"PKCS#8", pemEncode("PRIVATE KEY", pkcs8), false},
		{"empty", nil, true},
		{"no PEM block", []byte("not a pem"), true},
		{"corrupt DER", pemEncode("RSA PRIVATE KEY", []byte("not der")), true},
		{"non-RSA key", pemEncode("PRIVATE KEY", ecPKCS8), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePrivateKey(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(k) {
				t.Error("parsed key does not match the original")
			}
		})
	}
}

func TestNewJWT(t *testing.T) {
	k := testKey(t)
	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name        string
		appID       string
		wantPayload string
	}{
		{"numeric app id", "12345", `{"iat":1699999940,"exp":1700000600,"iss":"12345"}`},
		{"app id is JSON-escaped", `a"b`, `{"iat":1699999940,"exp":1700000600,"iss":"a\"b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jwt, err := NewJWT(tt.appID, k, now)
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(jwt, "=+/") {
				t.Errorf("JWT is not unpadded base64url: %s", jwt)
			}
			parts := strings.Split(jwt, ".")
			if len(parts) != 3 {
				t.Fatalf("want 3 segments, got %d", len(parts))
			}

			dec := base64.RawURLEncoding
			header, err := dec.DecodeString(parts[0])
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(header), `{"alg":"RS256","typ":"JWT"}`; got != want {
				t.Errorf("header = %s, want %s", got, want)
			}
			payload, err := dec.DecodeString(parts[1])
			if err != nil {
				t.Fatal(err)
			}
			if string(payload) != tt.wantPayload {
				t.Errorf("payload = %s, want %s", payload, tt.wantPayload)
			}

			sig, err := dec.DecodeString(parts[2])
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&k.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
				t.Errorf("signature does not verify: %v", err)
			}
		})
	}
}
