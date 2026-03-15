package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultAuthPath = ".awan/auth.json"

// Token represents locally persisted OAuth credentials.
type Token struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	TokenType    string    `json:"tokenType"`
	Expiry       time.Time `json:"expiry"`
}

// OAuthConfig contains provider endpoints and client settings.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	RedirectURI  string
	Scopes       []string
	StoragePath  string
}

// OAuthManager stores and refreshes provider tokens.
type OAuthManager struct {
	config     OAuthConfig
	httpClient *http.Client
}

// NewOAuthManager creates a configured OAuth manager.
func NewOAuthManager(config OAuthConfig) *OAuthManager {
	return &OAuthManager{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewOpenAIManagerFromEnv creates an OpenAI-oriented manager from env vars.
func NewOpenAIManagerFromEnv(storagePath string) *OAuthManager {
	return NewOAuthManager(OAuthConfig{
		ClientID:     os.Getenv("OPENAI_CLIENT_ID"),
		ClientSecret: os.Getenv("OPENAI_CLIENT_SECRET"),
		AuthURL:      os.Getenv("OPENAI_OAUTH_AUTH_URL"),
		TokenURL:     os.Getenv("OPENAI_OAUTH_TOKEN_URL"),
		RedirectURI:  os.Getenv("OPENAI_REDIRECT_URI"),
		Scopes:       strings.Fields(os.Getenv("OPENAI_OAUTH_SCOPES")),
		StoragePath:  storagePath,
	})
}

// AuthorizationURL returns the URL a client can open to start login.
func (m *OAuthManager) AuthorizationURL(state string) (string, error) {
	if m.config.AuthURL == "" {
		return "", errors.New("oauth authorization URL is not configured")
	}

	authURL, err := url.Parse(m.config.AuthURL)
	if err != nil {
		return "", err
	}

	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", m.config.ClientID)
	query.Set("redirect_uri", m.config.RedirectURI)
	if state != "" {
		query.Set("state", state)
	}
	if len(m.config.Scopes) > 0 {
		query.Set("scope", strings.Join(m.config.Scopes, " "))
	}
	authURL.RawQuery = query.Encode()

	return authURL.String(), nil
}

// Login exchanges an authorization code for tokens and stores them locally.
func (m *OAuthManager) Login(code string) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("authorization code is required")
	}

	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", m.config.ClientID)
	values.Set("client_secret", m.config.ClientSecret)
	values.Set("redirect_uri", m.config.RedirectURI)

	token, err := m.requestToken(values)
	if err != nil {
		return err
	}

	return m.saveToken(token)
}

// Logout removes stored tokens.
func (m *OAuthManager) Logout() error {
	path, err := m.storagePath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

// GetAccessToken returns a valid access token and refreshes it if needed.
func (m *OAuthManager) GetAccessToken() (string, error) {
	token, err := m.loadToken()
	if err != nil {
		return "", err
	}

	if token.AccessToken == "" {
		return "", errors.New("no access token available")
	}
	if token.Expiry.IsZero() || time.Now().Before(token.Expiry.Add(-1*time.Minute)) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		return "", errors.New("access token expired and no refresh token is available")
	}

	refreshed, err := m.refreshToken(token.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := m.saveToken(refreshed); err != nil {
		return "", err
	}

	return refreshed.AccessToken, nil
}

func (m *OAuthManager) refreshToken(refreshToken string) (*Token, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", m.config.ClientID)
	values.Set("client_secret", m.config.ClientSecret)

	token, err := m.requestToken(values)
	if err != nil {
		return nil, err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	return token, nil
}

func (m *OAuthManager) requestToken(values url.Values) (*Token, error) {
	if m.config.TokenURL == "" {
		return nil, errors.New("oauth token URL is not configured")
	}

	req, err := http.NewRequest(http.MethodPost, m.config.TokenURL, bytes.NewBufferString(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth token request failed with status %s", resp.Status)
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	token := &Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
	}
	if payload.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UTC()
	}

	return token, nil
}

func (m *OAuthManager) saveToken(token *Token) error {
	path, err := m.storagePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func (m *OAuthManager) loadToken() (*Token, error) {
	path, err := m.storagePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

func (m *OAuthManager) storagePath() (string, error) {
	if m.config.StoragePath != "" {
		return m.config.StoragePath, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, defaultAuthPath), nil
}
