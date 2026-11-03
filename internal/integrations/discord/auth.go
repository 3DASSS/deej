package discord

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nik9play/deej/internal/atomicfile"
	"github.com/nik9play/deej/internal/config"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var errAuthorizationChanged = errors.New("discord authorization changed during token request")

// Link runs the one interactive step of the integration: Discord shows the
// user a consent dialog, and approving it hands back an authorization code
// that deej exchanges for a token it can reuse from now on. This is deliberately
// not part of dial - a retry every few seconds would mean a popup every few
// seconds - so it lives behind the settings window's button
func (d *Client) Link() error {
	d.authLock.Lock()
	cfg := d.settings.Values().Discord
	generation := d.authGeneration
	d.authLock.Unlock()

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("save a Discord client ID and secret first")
	}

	conn, err := d.open(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	data, err := conn.requestWithTimeout("AUTHORIZE", map[string]any{
		"client_id": cfg.ClientID,
		"scopes":    discordScopes,
	}, discordAuthorizeTimeout)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}

	var authorized struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(data, &authorized); err != nil || authorized.Code == "" {
		return errors.New("discord returned no authorization code")
	}

	token, err := exchangeToken(cfg, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {authorized.Code},
		"redirect_uri": {discordRedirectURI},
	})
	if err != nil {
		return err
	}

	if err := d.storeToken(cfg, generation, token); err != nil {
		return err
	}

	d.logger.Info("Linked Discord account")
	d.reconnector.Reconnect(errors.New("account linked"))

	return nil
}

// Unlink forgets the stored token, which also drops the connection
func (d *Client) Unlink() error {
	err := d.clearToken()
	d.reconnector.Reconnect(errors.New("account unlinked"))
	if err != nil {
		return err
	}
	d.logger.Info("Unlinked Discord account")

	return nil
}

func (d *Client) clearToken() error {
	d.authLock.Lock()
	defer d.authLock.Unlock()
	d.authGeneration++
	d.token.Store(nil)
	if err := os.Remove(d.tokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Discord token: %w", err)
	}
	return nil
}

// discordToken is the OAuth token deej persists between runs. It's kept beside
// the config file rather than inside it: a token written by the backend would
// otherwise race the settings window's whole-document saves, and every write
// would churn the config watcher
type discordToken struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// expired reports whether the token needs refreshing, a minute early so one
// doesn't lapse midway through a request
func (t *discordToken) expired() bool {
	return time.Now().After(t.ExpiresAt.Add(-time.Minute))
}

func (d *Client) tokenPath() string {
	return d.tokenFile
}

func (d *Client) loadToken() {
	d.authLock.Lock()
	defer d.authLock.Unlock()

	data, err := os.ReadFile(d.tokenPath())
	if err != nil {
		return
	}

	var token discordToken
	if err := json.Unmarshal(data, &token); err != nil {
		d.logger.Warnw("Ignoring malformed Discord token file", "path", d.tokenPath(), "error", err)
		return
	}

	if token.AccessToken == "" || token.ClientID != d.settings.Values().Discord.ClientID {
		return
	}

	d.token.Store(&token)
	d.authGeneration++
	d.logger.Debug("Loaded stored Discord token")
}

// storeToken persists the token next to the config file. atomicfile.Write
// creates at 0600 and the rename preserves it, which is exactly what a
// credential file needs
func (d *Client) storeToken(cfg config.DiscordSettings, generation uint64, token *discordToken) error {
	d.authLock.Lock()
	defer d.authLock.Unlock()
	if generation != d.authGeneration || discordCredentialsChanged(cfg, d.settings.Values().Discord) {
		return errAuthorizationChanged
	}

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal Discord token: %w", err)
	}

	if err := atomicfile.Write(d.tokenPath(), data); err != nil {
		return fmt.Errorf("write Discord token: %w", err)
	}

	d.token.Store(token)
	d.authGeneration++

	return nil
}

func (d *Client) ensureFreshToken(cfg config.DiscordSettings, token *discordToken) (*discordToken, error) {
	d.authLock.Lock()
	generation := d.authGeneration
	current := d.token.Load() == token && !discordCredentialsChanged(cfg, d.settings.Values().Discord)
	d.authLock.Unlock()
	if !current {
		return nil, errAuthorizationChanged
	}

	if !token.expired() {
		return token, nil
	}

	if token.RefreshToken == "" {
		return nil, errors.New("the stored Discord token expired and can't be refreshed, link again")
	}

	d.logger.Debug("Discord token expired, refreshing")

	refreshed, err := exchangeToken(cfg, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token.RefreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("refresh Discord token: %w", err)
	}

	if err := d.storeToken(cfg, generation, refreshed); err != nil {
		return nil, err
	}

	return refreshed, nil
}

// exchangeToken posts to Discord's OAuth token endpoint, for both the initial
// authorization code and later refreshes
func exchangeToken(cfg config.DiscordSettings, form url.Values) (*discordToken, error) {
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	resp, err := discordHTTPClient.PostForm(discordTokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// the body carries Discord's own error description (and echoes nothing
		// secret), which is far more useful in the settings window than a bare
		// status code
		return nil, fmt.Errorf("token request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	if parsed.AccessToken == "" {
		return nil, errors.New("the token response had no access token")
	}

	return &discordToken{
		ClientID:     cfg.ClientID,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}
