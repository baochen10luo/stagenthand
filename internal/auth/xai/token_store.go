package xai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTokenEndpoint = "https://auth.x.ai/oauth2/token"
	defaultTokenPath     = "~/.hermes/auth.json"
	xaiClientID          = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiRefreshSkew       = 2 * time.Minute
)

var validateXAIEndpointFn = validateXAIEndpoint

var (
	ErrTokenNotFound = errors.New("xai oauth token not found")
	ErrTokenExpired  = errors.New("xai oauth token expired")
)

// Token is a normalized xAI OAuth credential record.
type Token struct {
	AccessToken   string
	RefreshToken  string
	IDToken       string
	TokenType     string
	ExpiresIn     int
	ExpiresAt     time.Time
	LastRefresh   time.Time
	TokenEndpoint string
	RedirectURI   string
	AuthMode      string
	CredentialID  string
}

func (t Token) expiryTime() time.Time {
	if !t.ExpiresAt.IsZero() {
		return t.ExpiresAt
	}
	if exp, ok := jwtExpiry(t.AccessToken); ok {
		return exp
	}
	if t.ExpiresIn > 0 && !t.LastRefresh.IsZero() {
		return t.LastRefresh.Add(time.Duration(t.ExpiresIn) * time.Second)
	}
	return time.Time{}
}

// ExpiryTime returns the best known token expiry time.
func (t Token) ExpiryTime() time.Time {
	return t.expiryTime()
}

func (t Token) expiring(now time.Time, skew time.Duration) bool {
	expiry := t.expiryTime()
	if expiry.IsZero() {
		return false
	}
	return !expiry.After(now.Add(skew))
}

// NeedsRefresh reports whether the token is expired or within the refresh skew.
func (t Token) NeedsRefresh(now time.Time) bool {
	return t.expiring(now, xaiRefreshSkew)
}

func (t Token) flatMap() map[string]any {
	data := map[string]any{
		"access_token":  t.AccessToken,
		"refresh_token": t.RefreshToken,
		"id_token":      t.IDToken,
		"token_type":    t.TokenType,
	}
	if t.ExpiresIn > 0 {
		data["expires_in"] = t.ExpiresIn
	}
	if !t.ExpiresAt.IsZero() {
		data["expires_at"] = t.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if !t.LastRefresh.IsZero() {
		data["last_refresh"] = t.LastRefresh.UTC().Format(time.RFC3339Nano)
	}
	if t.TokenEndpoint != "" {
		data["token_endpoint"] = t.TokenEndpoint
	}
	if t.RedirectURI != "" {
		data["redirect_uri"] = t.RedirectURI
	}
	if t.AuthMode != "" {
		data["auth_mode"] = t.AuthMode
	}
	return data
}

// TokenStore persists OAuth credentials.
type TokenStore interface {
	Load() (Token, error)
	Save(Token) error
}

type FileTokenStore struct {
	path string
}

func NewFileTokenStore(path string) *FileTokenStore {
	if strings.TrimSpace(path) == "" {
		path = defaultTokenPath
	}
	return &FileTokenStore{path: expandHome(path)}
}

func (s *FileTokenStore) Path() string {
	return s.path
}

func (s *FileTokenStore) Load() (Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{}, ErrTokenNotFound
		}
		return Token{}, err
	}
	return parseTokenFile(data)
}

func (s *FileTokenStore) Save(token Token) error {
	if token.TokenType == "" {
		token.TokenType = "Bearer"
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	if existing, err := os.ReadFile(s.path); err == nil {
		var root map[string]any
		if json.Unmarshal(existing, &root) == nil {
			if _, ok := root["providers"].(map[string]any); ok || filepath.Base(s.path) == "auth.json" {
				return writeHermesStyleAuth(s.path, root, token)
			}
		}
	}

	return writeJSONFile(s.path, token.flatMap())
}

func writeHermesStyleAuth(path string, root map[string]any, token Token) error {
	if root == nil {
		root = map[string]any{}
	}

	providers, _ := root["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		root["providers"] = providers
	}

	state, _ := providers["xai-oauth"].(map[string]any)
	if state == nil {
		state = map[string]any{}
	}

	if discovery, ok := state["discovery"].(map[string]any); ok {
		if token.TokenEndpoint != "" {
			discovery["token_endpoint"] = token.TokenEndpoint
		}
		state["discovery"] = discovery
	} else if token.TokenEndpoint != "" {
		state["discovery"] = map[string]any{"token_endpoint": token.TokenEndpoint}
	}

	if token.RedirectURI != "" {
		state["redirect_uri"] = token.RedirectURI
	}
	if token.AuthMode != "" {
		state["auth_mode"] = token.AuthMode
	}
	if !token.LastRefresh.IsZero() {
		state["last_refresh"] = token.LastRefresh.UTC().Format(time.RFC3339Nano)
	}
	state["tokens"] = token.flatMap()
	providers["xai-oauth"] = state
	root["active_provider"] = "xai-oauth"
	if _, ok := root["version"]; !ok {
		root["version"] = 1
	}
	updateCredentialPool(root, token)
	return writeJSONFile(path, root)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func parseTokenFile(data []byte) (Token, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return Token{}, err
	}

	if token, ok := parseCredentialPoolToken(root); ok {
		return token, nil
	}
	if token, ok := parseHermesStyleToken(root); ok {
		return token, nil
	}
	if token, ok := parseFlatToken(root); ok {
		return token, nil
	}

	return Token{}, ErrTokenNotFound
}

func parseCredentialPoolToken(root map[string]any) (Token, bool) {
	pool := mapValue(root["credential_pool"])
	if len(pool) == 0 {
		return Token{}, false
	}

	providers := []string{}
	if active := stringValue(root["active_provider"]); active != "" {
		providers = append(providers, active)
	}
	providers = append(providers, "xai-oauth", "xai")

	var fallback Token
	for _, provider := range providers {
		credentials, ok := pool[provider].([]any)
		if !ok {
			continue
		}
		for _, item := range credentials {
			credential := mapValue(item)
			if len(credential) == 0 {
				continue
			}
			if authType := stringValue(credential["auth_type"]); authType != "" && authType != "oauth" {
				continue
			}
			token := parseTokenMap(credential, credential)
			if token.AccessToken == "" {
				continue
			}
			token.CredentialID = stringValue(credential["id"])
			if token.TokenEndpoint == "" {
				token.TokenEndpoint = tokenEndpointFromProvider(root, provider)
			}
			if token.TokenType == "" {
				token.TokenType = "Bearer"
			}
			if fallback.AccessToken == "" {
				fallback = token
			}
			if stringValue(credential["last_status"]) == "exhausted" {
				continue
			}
			return token, true
		}
	}

	if fallback.AccessToken != "" {
		return fallback, true
	}
	return Token{}, false
}

func parseHermesStyleToken(root map[string]any) (Token, bool) {
	providers := mapValue(root["providers"])
	if len(providers) == 0 {
		return Token{}, false
	}

	candidates := []string{}
	if active := stringValue(root["active_provider"]); active != "" {
		candidates = append(candidates, active)
	}
	candidates = append(candidates, "xai-oauth", "xai")

	for _, name := range candidates {
		state := mapValue(providers[name])
		if len(state) == 0 {
			continue
		}
		token := parseProviderState(state)
		if token.AccessToken != "" {
			return token, true
		}
	}
	return Token{}, false
}

func parseFlatToken(root map[string]any) (Token, bool) {
	if stringValue(root["access_token"]) == "" {
		if tokenMap := mapValue(root["tokens"]); len(tokenMap) > 0 {
			token := parseTokenMap(tokenMap, root)
			if token.AccessToken != "" {
				return token, true
			}
		}
		return Token{}, false
	}
	token := parseTokenMap(root, root)
	return token, true
}

func parseProviderState(state map[string]any) Token {
	tokenMap := mapValue(state["tokens"])
	if len(tokenMap) == 0 {
		tokenMap = state
	}
	token := parseTokenMap(tokenMap, state)
	if token.TokenEndpoint == "" {
		token.TokenEndpoint = stringValue(mapValue(state["discovery"])["token_endpoint"])
	}
	if token.RedirectURI == "" {
		token.RedirectURI = stringValue(state["redirect_uri"])
	}
	if token.AuthMode == "" {
		token.AuthMode = stringValue(state["auth_mode"])
	}
	if token.LastRefresh.IsZero() {
		token.LastRefresh = timeValue(state["last_refresh"])
	}
	return token
}

func parseTokenMap(m map[string]any, parent map[string]any) Token {
	token := Token{
		AccessToken:   stringValue(m["access_token"]),
		RefreshToken:  stringValue(m["refresh_token"]),
		IDToken:       stringValue(m["id_token"]),
		TokenType:     stringValue(m["token_type"]),
		TokenEndpoint: stringValue(m["token_endpoint"]),
		RedirectURI:   stringValue(m["redirect_uri"]),
		AuthMode:      stringValue(m["auth_mode"]),
		ExpiresIn:     intValue(m["expires_in"]),
		ExpiresAt:     timeValue(m["expires_at"]),
		LastRefresh:   timeValue(m["last_refresh"]),
	}

	if token.TokenEndpoint == "" && parent != nil {
		if discovery := mapValue(parent["discovery"]); len(discovery) > 0 {
			token.TokenEndpoint = stringValue(discovery["token_endpoint"])
		}
	}

	if token.RedirectURI == "" && parent != nil {
		token.RedirectURI = stringValue(parent["redirect_uri"])
	}
	if token.AuthMode == "" && parent != nil {
		token.AuthMode = stringValue(parent["auth_mode"])
	}
	if token.LastRefresh.IsZero() && parent != nil {
		token.LastRefresh = timeValue(parent["last_refresh"])
	}
	return token
}

func tokenEndpointFromProvider(root map[string]any, provider string) string {
	providers := mapValue(root["providers"])
	if len(providers) == 0 {
		return ""
	}
	state := mapValue(providers[provider])
	if len(state) == 0 && provider != "xai-oauth" {
		state = mapValue(providers["xai-oauth"])
	}
	return stringValue(mapValue(state["discovery"])["token_endpoint"])
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}

func mapValue(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func intValue(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int8:
		return int(t)
	case int16:
		return int(t)
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}

func timeValue(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed
		}
		if n, err := time.ParseDuration(s); err == nil {
			return time.Now().Add(n)
		}
	case float64:
		if t <= 0 {
			return time.Time{}
		}
		if t > 1_000_000_000_000 {
			return time.UnixMilli(int64(t)).UTC()
		}
		return time.Unix(int64(t), 0).UTC()
	case int64:
		if t <= 0 {
			return time.Time{}
		}
		if t > 1_000_000_000_000 {
			return time.UnixMilli(t).UTC()
		}
		return time.Unix(t, 0).UTC()
	case int:
		return timeValue(int64(t))
	}
	return time.Time{}
}

func jwtExpiry(accessToken string) (time.Time, bool) {
	if !strings.Contains(accessToken, ".") {
		return time.Time{}, false
	}
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return time.Time{}, false
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return time.Time{}, false
	}
	exp := claims["exp"]
	switch t := exp.(type) {
	case float64:
		if t <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(t), 0).UTC(), true
	case int64:
		if t <= 0 {
			return time.Time{}, false
		}
		return time.Unix(t, 0).UTC(), true
	case int:
		if t <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(t), 0).UTC(), true
	default:
		return time.Time{}, false
	}
}

func updateCredentialPool(root map[string]any, token Token) {
	if token.CredentialID == "" {
		return
	}
	pool := mapValue(root["credential_pool"])
	if len(pool) == 0 {
		return
	}
	credentials, ok := pool["xai-oauth"].([]any)
	if !ok {
		return
	}
	for _, item := range credentials {
		credential := mapValue(item)
		if len(credential) == 0 || stringValue(credential["id"]) != token.CredentialID {
			continue
		}
		credential["access_token"] = token.AccessToken
		credential["refresh_token"] = token.RefreshToken
		credential["last_status"] = "ok"
		credential["last_refresh"] = token.LastRefresh.UTC().Format(time.RFC3339Nano)
		return
	}
}

type FileTokenSource struct {
	store  TokenStore
	now    func() time.Time
	client *http.Client
}

func NewFileTokenSource(store TokenStore) *FileTokenSource {
	return &FileTokenSource{
		store: store,
		now:   time.Now,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *FileTokenSource) BearerToken(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	token, err := s.store.Load()
	if err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", ErrTokenNotFound
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	if !token.expiring(now(), xaiRefreshSkew) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		return "", ErrTokenExpired
	}
	endpoint := token.TokenEndpoint
	if endpoint == "" {
		endpoint = defaultTokenEndpoint
	}
	refreshed, err := refreshXAIToken(ctx, s.client, endpoint, token.RefreshToken)
	if err != nil {
		return "", err
	}
	if refreshed.AccessToken == "" {
		return "", fmt.Errorf("%w: missing access_token", ErrTokenNotFound)
	}
	if refreshed.TokenType == "" {
		refreshed.TokenType = "Bearer"
	}
	if refreshed.TokenEndpoint == "" {
		refreshed.TokenEndpoint = endpoint
	}
	if refreshed.RedirectURI == "" {
		refreshed.RedirectURI = token.RedirectURI
	}
	if refreshed.AuthMode == "" {
		refreshed.AuthMode = token.AuthMode
	}
	refreshed.CredentialID = token.CredentialID
	if refreshed.LastRefresh.IsZero() {
		refreshed.LastRefresh = now().UTC()
	}
	if refreshed.ExpiresIn == 0 {
		refreshed.ExpiresIn = token.ExpiresIn
	}
	if err := s.store.Save(refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func refreshXAIToken(ctx context.Context, client *http.Client, tokenEndpoint, refreshToken string) (Token, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Token{}, ErrTokenExpired
	}
	endpoint, err := validateXAIEndpointFn(tokenEndpoint)
	if err != nil {
		return Token{}, err
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", xaiClientID)
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, err
	}
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		msg := parseXAIErrorMessage(body)
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if resp.StatusCode == http.StatusForbidden {
			return Token{}, fmt.Errorf("xAI OAuth request was forbidden: %s", msg)
		}
		return Token{}, fmt.Errorf("xAI token refresh failed with HTTP %d: %s", resp.StatusCode, msg)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Token{}, fmt.Errorf("xAI token refresh returned invalid JSON: %w", err)
	}

	refreshed := Token{
		AccessToken:  stringValue(payload["access_token"]),
		RefreshToken: stringValue(payload["refresh_token"]),
		IDToken:      stringValue(payload["id_token"]),
		TokenType:    stringValue(payload["token_type"]),
		ExpiresIn:    intValue(payload["expires_in"]),
		ExpiresAt:    timeValue(payload["expires_at"]),
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = refreshToken
	}
	refreshed.LastRefresh = time.Now().UTC()
	refreshed.TokenEndpoint = endpoint
	return refreshed, nil
}

func validateXAIEndpoint(raw string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		endpoint = defaultTokenEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("xAI discovery returned an invalid token_endpoint: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("xAI discovery returned a non-HTTPS token_endpoint: %s", endpoint)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "x.ai" && !strings.HasSuffix(host, ".x.ai") {
		return "", fmt.Errorf("xAI discovery token_endpoint host %q is not on the xAI origin", host)
	}
	return endpoint, nil
}

func parseXAIErrorMessage(data []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Error.Code != "" {
			return payload.Error.Code
		}
	}
	return strings.TrimSpace(string(data))
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return os.ExpandEnv(path)
}
