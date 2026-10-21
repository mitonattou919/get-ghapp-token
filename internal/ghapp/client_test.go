package ghapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func inst(id int64, login string) Installation {
	var i Installation
	i.ID = id
	i.Account.Login = login
	return i
}

type recordedRequest struct {
	method, path, auth, accept string
}

// stubAPI serves a fixed status/body and records the single request it receives.
func stubAPI(t *testing.T, status int, body string) (*Client, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*rec = recordedRequest{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), r.Header.Get("Accept")}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-jwt")
	c.BaseURL = srv.URL
	return c, rec
}

func assertHeaders(t *testing.T, rec *recordedRequest) {
	t.Helper()
	if rec.auth != "Bearer test-jwt" {
		t.Errorf("Authorization = %q", rec.auth)
	}
	if rec.accept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", rec.accept)
	}
}

func TestListInstallations(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    []Installation
		wantErr bool
	}{
		{
			name:   "two installations",
			status: http.StatusOK,
			body:   `[{"id":1,"account":{"login":"alice"}},{"id":2,"account":{"login":"bob"}}]`,
			want:   []Installation{inst(1, "alice"), inst(2, "bob")},
		},
		{name: "empty list", status: http.StatusOK, body: `[]`, want: []Installation{}},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`, wantErr: true},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
		{name: "invalid JSON", status: http.StatusOK, body: `{not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := stubAPI(t, tt.status, tt.body)
			got, err := c.ListInstallations(context.Background())

			if rec.method != http.MethodGet || rec.path != "/app/installations" {
				t.Errorf("request = %s %s", rec.method, rec.path)
			}
			assertHeaders(t, rec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCreateAccessToken(t *testing.T) {
	tests := []struct {
		name           string
		installationID string
		status         int
		body           string
		wantPath       string
		want           string
		wantErr        error // nil means success; errAny means any non-sentinel error
	}{
		{
			name: "created", installationID: "123", status: http.StatusCreated,
			body: `{"token":"ghs_abc","expires_at":"2026-01-01T00:00:00Z"}`, wantPath: "/app/installations/123/access_tokens", want: "ghs_abc",
		},
		{
			name: "installation id is path-escaped", installationID: "12/34", status: http.StatusCreated,
			body: `{"token":"ghs_abc"}`, wantPath: "/app/installations/12%2F34/access_tokens", want: "ghs_abc",
		},
		{
			name: "token field missing", installationID: "123", status: http.StatusCreated,
			body: `{"expires_at":"2026-01-01T00:00:00Z"}`, wantPath: "/app/installations/123/access_tokens", wantErr: ErrTokenMissing,
		},
		{
			name: "token field empty", installationID: "123", status: http.StatusCreated,
			body: `{"token":""}`, wantPath: "/app/installations/123/access_tokens", wantErr: ErrTokenMissing,
		},
		{
			name: "not found", installationID: "123", status: http.StatusNotFound,
			body: `{"message":"Not Found"}`, wantPath: "/app/installations/123/access_tokens", wantErr: errAny,
		},
		{
			name: "invalid JSON", installationID: "123", status: http.StatusCreated,
			body: `{not json`, wantPath: "/app/installations/123/access_tokens", wantErr: errAny,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := stubAPI(t, tt.status, tt.body)
			got, err := c.CreateAccessToken(context.Background(), tt.installationID)

			if rec.method != http.MethodPost || rec.path != tt.wantPath {
				t.Errorf("request = %s %s, want POST %s", rec.method, rec.path, tt.wantPath)
			}
			assertHeaders(t, rec)
			switch tt.wantErr {
			case nil:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("token = %q, want %q", got, tt.want)
				}
			case errAny:
				if err == nil || errors.Is(err, ErrTokenMissing) {
					t.Errorf("want a request/decode error, got %v", err)
				}
			default:
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("err = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

var errAny = errors.New("any error")

func TestSelectInstallation(t *testing.T) {
	installs := []Installation{inst(1, "alice"), inst(2, "bob"), inst(3, "bob")}

	tests := []struct {
		name          string
		installs      []Installation
		login         string
		want          string
		wantErr       error
		wantAvailable []string
	}{
		{name: "no login picks first", installs: installs, want: "1"},
		{name: "login match", installs: installs, login: "bob", want: "2"},
		{name: "no installations", installs: nil, wantErr: ErrNoInstallations},
		{name: "login mismatch lists accounts", installs: installs, login: "carol", wantAvailable: []string{"alice", "bob", "bob"}},
		{name: "login with no installations", installs: nil, login: "carol", wantAvailable: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectInstallation(tt.installs, tt.login)

			if tt.wantAvailable != nil {
				var noMatch *NoMatchingLoginError
				if !errors.As(err, &noMatch) {
					t.Fatalf("want NoMatchingLoginError, got %v", err)
				}
				if noMatch.Login != tt.login || !reflect.DeepEqual(noMatch.Available, tt.wantAvailable) {
					t.Errorf("got %+v, want login %q available %v", noMatch, tt.login, tt.wantAvailable)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
