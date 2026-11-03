package discord

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nik9play/deej/internal/config"
	"go.uber.org/zap"
)

func newAuthTestClient(t *testing.T) (*Client, *testSettings, *discordToken) {
	t.Helper()
	settings := &testSettings{values: &config.Settings{Discord: config.DiscordSettings{
		Enabled: true, ClientID: "123", ClientSecret: "secret",
	}}}
	client := New(settings, zap.NewNop().Sugar(), filepath.Join(t.TempDir(), TokenFileName), nil)
	token := &discordToken{
		ClientID: "123", AccessToken: "old", RefreshToken: "refresh",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if err := client.storeToken(settings.values.Discord, client.authGeneration, token); err != nil {
		t.Fatal(err)
	}
	return client, settings, token
}

type tokenRoundTripper func(*http.Request) (*http.Response, error)

func (f tokenRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type tokenRefreshResult struct {
	token *discordToken
	err   error
}

// Pause the HTTP response so invalidation can be tested without timing races
// or a real Discord account. Tests using the shared HTTP client are sequential.
func blockedTokenRefresh(t *testing.T, client *Client, cfg config.DiscordSettings, token *discordToken) (func(), <-chan tokenRefreshResult) {
	t.Helper()
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	result := make(chan tokenRefreshResult, 1)
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	original := discordHTTPClient
	discordHTTPClient = &http.Client{Transport: tokenRoundTripper(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"refreshed","refresh_token":"new-refresh","expires_in":3600}`)),
		}, nil
	})}
	go func() {
		defer close(finished)
		refreshed, err := client.ensureFreshToken(cfg, token)
		result <- tokenRefreshResult{refreshed, err}
	}()
	t.Cleanup(func() {
		resume()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("token refresh did not finish")
		}
		discordHTTPClient = original
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("token refresh did not start")
	}
	return resume, result
}

func receiveTokenRefresh(t *testing.T, result <-chan tokenRefreshResult) tokenRefreshResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("token refresh did not return")
		return tokenRefreshResult{}
	}
}

func readAuthTestToken(t *testing.T, client *Client) discordToken {
	t.Helper()
	data, err := os.ReadFile(client.tokenPath())
	if err != nil {
		t.Fatal(err)
	}
	var token discordToken
	if err := json.Unmarshal(data, &token); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestTokenRefreshPersistsCurrentAuthorization(t *testing.T) {
	client, settings, old := newAuthTestClient(t)
	resume, result := blockedTokenRefresh(t, client, settings.values.Discord, old)
	resume()
	got := receiveTokenRefresh(t, result)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.token.AccessToken != "refreshed" || got.token.RefreshToken != "new-refresh" || client.token.Load() != got.token {
		t.Fatalf("refresh not applied: %+v", got.token)
	}
	if saved := readAuthTestToken(t, client); saved.AccessToken != "refreshed" || saved.RefreshToken != "new-refresh" {
		t.Fatalf("refresh not persisted: %+v", saved)
	}
}

func TestUnlinkDuringTokenRefresh(t *testing.T) {
	client, settings, old := newAuthTestClient(t)
	resume, result := blockedTokenRefresh(t, client, settings.values.Discord, old)
	unlinked := make(chan error, 1)
	go func() { unlinked <- client.Unlink() }()
	select {
	case err := <-unlinked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Unlink waited for the HTTP response")
	}
	resume()
	if got := receiveTokenRefresh(t, result); !errors.Is(got.err, errAuthorizationChanged) {
		t.Fatalf("stale refresh error = %v", got.err)
	}
	if client.Status().Linked {
		t.Fatal("token refresh restored linked state after Unlink")
	}
	if _, err := os.Stat(client.tokenPath()); !os.IsNotExist(err) {
		t.Fatalf("token refresh recreated removed token file: %v", err)
	}
}

func TestTokenRefreshRejectsChangedCredentials(t *testing.T) {
	for _, field := range []string{"client ID", "client secret"} {
		t.Run(field, func(t *testing.T) {
			client, settings, old := newAuthTestClient(t)
			resume, result := blockedTokenRefresh(t, client, settings.values.Discord, old)
			updated := *settings.values
			if field == "client ID" {
				updated.Discord.ClientID = "456"
			} else {
				updated.Discord.ClientSecret = "new-secret"
			}
			// Even before the reload worker clears the token, the response
			// must be checked against the latest settings snapshot.
			settings.values = &updated
			resume()
			if got := receiveTokenRefresh(t, result); !errors.Is(got.err, errAuthorizationChanged) {
				t.Fatalf("stale credentials refresh error = %v", got.err)
			}
			if client.token.Load() != old || readAuthTestToken(t, client).AccessToken != old.AccessToken {
				t.Fatal("stale response overwrote the token")
			}
		})
	}
}

func TestStaleRefreshCannotOverwriteNewLink(t *testing.T) {
	client, settings, old := newAuthTestClient(t)
	cfg := settings.values.Discord
	resume, result := blockedTokenRefresh(t, client, cfg, old)
	if err := client.Unlink(); err != nil {
		t.Fatal(err)
	}
	newToken := &discordToken{ClientID: cfg.ClientID, AccessToken: "new-link", ExpiresAt: time.Now().Add(time.Hour)}
	if err := client.storeToken(cfg, client.authGeneration, newToken); err != nil {
		t.Fatal(err)
	}
	resume()
	if got := receiveTokenRefresh(t, result); !errors.Is(got.err, errAuthorizationChanged) {
		t.Fatalf("stale refresh error = %v", got.err)
	}
	if client.token.Load() != newToken || readAuthTestToken(t, client).AccessToken != newToken.AccessToken {
		t.Fatal("stale refresh replaced the newly linked account")
	}
}

func TestUnlinkInvalidatesPendingLinkAndLoadedToken(t *testing.T) {
	client, settings, old := newAuthTestClient(t)
	cfg, generation := settings.values.Discord, client.authGeneration
	if err := client.Unlink(); err != nil {
		t.Fatal(err)
	}
	if err := client.storeToken(cfg, generation, old); !errors.Is(err, errAuthorizationChanged) {
		t.Fatalf("pending link error = %v", err)
	}
	if _, err := client.ensureFreshToken(cfg, old); !errors.Is(err, errAuthorizationChanged) {
		t.Fatalf("previously loaded token error = %v", err)
	}
	if client.Status().Linked {
		t.Fatal("stale authorization restored linked state")
	}
	if _, err := os.Stat(client.tokenPath()); !os.IsNotExist(err) {
		t.Fatalf("stale authorization recreated token file: %v", err)
	}
}
