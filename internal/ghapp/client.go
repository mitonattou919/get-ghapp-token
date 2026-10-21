package ghapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const DefaultBaseURL = "https://api.github.com"

var (
	ErrNoInstallations = errors.New("no installations found for this GitHub App")
	ErrTokenMissing    = errors.New("token not found in response")
)

type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
	} `json:"account"`
}

type NoMatchingLoginError struct {
	Login     string
	Available []string
}

func (e *NoMatchingLoginError) Error() string {
	return fmt.Sprintf("no installation found for account %q", e.Login)
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	JWT        string
}

func NewClient(jwt string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		JWT:        jwt,
	}
}

// ListInstallations fetches only the first page, matching the shell version.
func (c *Client) ListInstallations(ctx context.Context) ([]Installation, error) {
	var installs []Installation
	if err := c.do(ctx, http.MethodGet, "/app/installations", &installs); err != nil {
		return nil, err
	}
	return installs, nil
}

func (c *Client) CreateAccessToken(ctx context.Context, installationID string) (string, error) {
	var resp struct {
		Token string `json:"token"`
	}
	path := "/app/installations/" + url.PathEscape(installationID) + "/access_tokens"
	if err := c.do(ctx, http.MethodPost, path, &resp); err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", ErrTokenMissing
	}
	return resp.Token, nil
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.JWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: unexpected status %s", method, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// SelectInstallation picks by account login when given, otherwise the first one.
func SelectInstallation(installs []Installation, login string) (string, error) {
	if login != "" {
		available := make([]string, 0, len(installs))
		for _, inst := range installs {
			if inst.Account.Login == login {
				return strconv.FormatInt(inst.ID, 10), nil
			}
			available = append(available, inst.Account.Login)
		}
		return "", &NoMatchingLoginError{Login: login, Available: available}
	}
	if len(installs) == 0 {
		return "", ErrNoInstallations
	}
	return strconv.FormatInt(installs[0].ID, 10), nil
}
