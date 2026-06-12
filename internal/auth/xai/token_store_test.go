package xai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func jwtWithExp(exp int64) string {
	payload := map[string]any{"exp": exp}
	raw, _ := json.Marshal(payload)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return "header." + encoded + ".signature"
}

func writeAuthFile(t *testing.T, dir string, data any) string {
	t.Helper()
	path := filepath.Join(dir, "auth.json")
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshal auth json: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write auth json: %v", err)
	}
	return path
}

type cancelAfterRefreshRoundTripper struct {
	cancel context.CancelFunc
	body   *trackingRefreshBody
}

func (rt cancelAfterRefreshRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.cancel()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       rt.body,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type trackingRefreshBody struct {
	data         []byte
	readCalls    int
	closed       bool
	cancelOnRead context.CancelFunc
}

func (b *trackingRefreshBody) Read(p []byte) (int, error) {
	b.readCalls++
	if b.cancelOnRead != nil {
		cancel := b.cancelOnRead
		b.cancelOnRead = nil
		cancel()
	}
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (b *trackingRefreshBody) Close() error {
	b.closed = true
	return nil
}

func TestFileTokenStore_LoadHermesAuthJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	future := time.Now().Add(time.Hour).Unix()
	path := writeAuthFile(t, dir, map[string]any{
		"version":         1,
		"active_provider": "xai-oauth",
		"providers": map[string]any{
			"xai-oauth": map[string]any{
				"tokens": map[string]any{
					"access_token":  jwtWithExp(future),
					"refresh_token": "refresh-token",
					"id_token":      "id-token",
					"token_type":    "Bearer",
					"expires_in":    21600,
				},
				"last_refresh": "2026-05-29T18:16:29.252487Z",
				"auth_mode":    "oauth_pkce",
				"discovery": map[string]any{
					"token_endpoint": "https://auth.x.ai/oauth2/token",
				},
				"redirect_uri": "http://127.0.0.1:64606/callback",
			},
		},
	})

	store := NewFileTokenStore(path)
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.AccessToken != jwtWithExp(future) {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, jwtWithExp(future))
	}
	if got.RefreshToken != "refresh-token" {
		t.Fatalf("RefreshToken = %q, want refresh-token", got.RefreshToken)
	}
	if got.IDToken != "id-token" {
		t.Fatalf("IDToken = %q, want id-token", got.IDToken)
	}
	if got.TokenType != "Bearer" {
		t.Fatalf("TokenType = %q, want Bearer", got.TokenType)
	}
	if got.ExpiresIn != 21600 {
		t.Fatalf("ExpiresIn = %d, want 21600", got.ExpiresIn)
	}
	if got.TokenEndpoint != "https://auth.x.ai/oauth2/token" {
		t.Fatalf("TokenEndpoint = %q, want https://auth.x.ai/oauth2/token", got.TokenEndpoint)
	}
	if got.RedirectURI != "http://127.0.0.1:64606/callback" {
		t.Fatalf("RedirectURI = %q, want redirect uri", got.RedirectURI)
	}
}

func TestNewFileTokenStore_DefaultPath(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	store := NewFileTokenStore("")

	want := filepath.Join(home, ".hermes", "auth.json")
	if store.Path() != want {
		t.Fatalf("Path() = %q, want %q", store.Path(), want)
	}
}

func TestFileTokenStore_LoadHermesCredentialPoolPrefersUsableCredential(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	legacyAccess := jwtWithExp(time.Now().Add(-time.Hour).Unix())
	pooledAccess := jwtWithExp(time.Now().Add(time.Hour).Unix())
	path := writeAuthFile(t, dir, map[string]any{
		"version":         1,
		"active_provider": "xai-oauth",
		"providers": map[string]any{
			"xai-oauth": map[string]any{
				"tokens": map[string]any{
					"access_token":  legacyAccess,
					"refresh_token": "revoked-refresh-token",
					"token_type":    "Bearer",
				},
				"discovery": map[string]any{
					"token_endpoint": "https://auth.x.ai/oauth2/token",
				},
			},
		},
		"credential_pool": map[string]any{
			"xai-oauth": []any{
				map[string]any{
					"id":            "old",
					"auth_type":     "oauth",
					"access_token":  legacyAccess,
					"refresh_token": "old-refresh-token",
					"last_status":   "exhausted",
				},
				map[string]any{
					"id":            "new",
					"auth_type":     "oauth",
					"access_token":  pooledAccess,
					"refresh_token": "new-refresh-token",
					"last_status":   "ok",
					"last_refresh":  "2026-06-10T10:03:00.168582Z",
				},
			},
		},
	})

	got, err := NewFileTokenStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.AccessToken != pooledAccess {
		t.Fatalf("AccessToken = %q, want pooled credential", got.AccessToken)
	}
	if got.RefreshToken != "new-refresh-token" {
		t.Fatalf("RefreshToken = %q, want new-refresh-token", got.RefreshToken)
	}
	if got.CredentialID != "new" {
		t.Fatalf("CredentialID = %q, want new", got.CredentialID)
	}
	if got.TokenEndpoint != "https://auth.x.ai/oauth2/token" {
		t.Fatalf("TokenEndpoint = %q, want https://auth.x.ai/oauth2/token", got.TokenEndpoint)
	}
}

func TestFileTokenStore_SaveAndLoadFlatToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "xai.json")
	store := NewFileTokenStore(path)

	want := Token{
		AccessToken:   jwtWithExp(time.Now().Add(time.Hour).Unix()),
		RefreshToken:  "rt",
		IDToken:       "id",
		TokenType:     "Bearer",
		ExpiresIn:     3600,
		LastRefresh:   time.Date(2026, time.May, 29, 18, 16, 29, 0, time.UTC),
		TokenEndpoint: "https://auth.x.ai/oauth2/token",
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, want.AccessToken)
	}
	if got.RefreshToken != want.RefreshToken {
		t.Fatalf("RefreshToken = %q, want %q", got.RefreshToken, want.RefreshToken)
	}
	if got.IDToken != want.IDToken {
		t.Fatalf("IDToken = %q, want %q", got.IDToken, want.IDToken)
	}
	if got.TokenType != want.TokenType {
		t.Fatalf("TokenType = %q, want %q", got.TokenType, want.TokenType)
	}
	if got.ExpiresIn != want.ExpiresIn {
		t.Fatalf("ExpiresIn = %d, want %d", got.ExpiresIn, want.ExpiresIn)
	}
	if got.TokenEndpoint != want.TokenEndpoint {
		t.Fatalf("TokenEndpoint = %q, want %q", got.TokenEndpoint, want.TokenEndpoint)
	}
}

func TestFileTokenSource_BearerToken_RefreshesAndPersistsHermesAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	oldAccess := jwtWithExp(time.Now().Add(-time.Hour).Unix())
	oldRefresh := "refresh-token-old"

	newAccess := jwtWithExp(time.Now().Add(time.Hour).Unix())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/oauth2/token" {
			t.Fatalf("path = %s, want /oauth2/token", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q, want application/x-www-form-urlencoded", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != oldRefresh {
			t.Fatalf("refresh_token = %q, want %q", got, oldRefresh)
		}
		if got := r.Form.Get("client_id"); got != xaiClientID {
			t.Fatalf("client_id = %q, want %q", got, xaiClientID)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  newAccess,
			"refresh_token": "refresh-token-rotated",
			"id_token":      "new-id",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	oldValidate := validateXAIEndpointFn
	validateXAIEndpointFn = func(endpoint string) (string, error) {
		return endpoint, nil
	}
	t.Cleanup(func() {
		validateXAIEndpointFn = oldValidate
	})

	authPath := writeAuthFile(t, dir, map[string]any{
		"version":         1,
		"active_provider": "xai-oauth",
		"providers": map[string]any{
			"xai-oauth": map[string]any{
				"tokens": map[string]any{
					"access_token":  oldAccess,
					"refresh_token": oldRefresh,
					"id_token":      "old-id",
					"token_type":    "Bearer",
					"expires_in":    21600,
				},
				"last_refresh": "2026-05-29T18:16:29.252487Z",
				"auth_mode":    "oauth_pkce",
				"discovery": map[string]any{
					"token_endpoint": server.URL + "/oauth2/token",
				},
			},
		},
	})

	store := NewFileTokenStore(authPath)
	source := NewFileTokenSource(store)
	got, err := source.BearerToken(t.Context())
	if err != nil {
		t.Fatalf("BearerToken() error = %v", err)
	}
	if got != newAccess {
		t.Fatalf("BearerToken() = %q, want %q", got, newAccess)
	}

	updated, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after refresh error = %v", err)
	}
	if updated.AccessToken != newAccess {
		t.Fatalf("saved access token = %q, want %q", updated.AccessToken, newAccess)
	}
	if updated.RefreshToken != "refresh-token-rotated" {
		t.Fatalf("saved refresh token = %q, want refresh-token-rotated", updated.RefreshToken)
	}
	if updated.IDToken != "new-id" {
		t.Fatalf("saved id token = %q, want new-id", updated.IDToken)
	}
	if updated.TokenType != "Bearer" {
		t.Fatalf("saved token type = %q, want Bearer", updated.TokenType)
	}
}

func TestFileTokenSource_BearerToken_NilContextUsesBackgroundForCachedToken(t *testing.T) {
	t.Parallel()

	store := NewFileTokenStore(filepath.Join(t.TempDir(), "xai.json"))
	want := jwtWithExp(time.Now().Add(time.Hour).Unix())
	if err := store.Save(Token{
		AccessToken:  want,
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := NewFileTokenSource(store).BearerToken(nil)
	if err != nil {
		t.Fatalf("BearerToken(nil) error = %v", err)
	}
	if got != want {
		t.Fatalf("BearerToken(nil) = %q, want cached access token", got)
	}
}

func TestFileTokenSource_BearerToken_NilContextUsesBackgroundForRefresh(t *testing.T) {
	t.Parallel()

	oldValidate := validateXAIEndpointFn
	validateXAIEndpointFn = func(endpoint string) (string, error) {
		return endpoint, nil
	}
	t.Cleanup(func() {
		validateXAIEndpointFn = oldValidate
	})

	newAccess := jwtWithExp(time.Now().Add(time.Hour).Unix())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context() == nil {
			t.Fatal("request context is nil")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  newAccess,
			"refresh_token": "refresh-token-rotated",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	store := NewFileTokenStore(filepath.Join(t.TempDir(), "xai.json"))
	if err := store.Save(Token{
		AccessToken:   jwtWithExp(time.Now().Add(-time.Hour).Unix()),
		RefreshToken:  "refresh-token",
		TokenType:     "Bearer",
		TokenEndpoint: server.URL + "/oauth2/token",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := NewFileTokenSource(store).BearerToken(nil)
	if err != nil {
		t.Fatalf("BearerToken(nil) error = %v", err)
	}
	if got != newAccess {
		t.Fatalf("BearerToken(nil) = %q, want refreshed access token", got)
	}
}

func TestFileTokenSource_BearerToken_RejectsCanceledContextBeforeLoad(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewFileTokenStore(filepath.Join(t.TempDir(), "missing.json"))

	_, err := NewFileTokenSource(store).BearerToken(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BearerToken() error = %v, want context.Canceled", err)
	}
}

func TestFileTokenSource_BearerToken_RejectsCanceledContextBeforeReadingRefreshResponse(t *testing.T) {
	t.Parallel()

	store := NewFileTokenStore(filepath.Join(t.TempDir(), "xai.json"))
	oldAccess := jwtWithExp(time.Now().Add(-time.Hour).Unix())
	if err := store.Save(Token{
		AccessToken:   oldAccess,
		RefreshToken:  "refresh-token",
		TokenType:     "Bearer",
		TokenEndpoint: "https://auth.x.ai/oauth2/token",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	body := &trackingRefreshBody{data: []byte(`{"access_token":"new-access","refresh_token":"rotated-refresh"}`)}
	source := NewFileTokenSource(store)
	source.client = &http.Client{Transport: cancelAfterRefreshRoundTripper{
		cancel: cancel,
		body:   body,
	}}

	got, err := source.BearerToken(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BearerToken() error = %v, want context.Canceled", err)
	}
	if got != "" {
		t.Fatalf("BearerToken() = %q, want empty token on canceled context", got)
	}
	if body.readCalls != 0 {
		t.Fatalf("refresh response body reads = %d, want 0", body.readCalls)
	}
	if !body.closed {
		t.Fatal("refresh response body should be closed")
	}

	saved, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after canceled refresh error = %v", err)
	}
	if saved.AccessToken != oldAccess {
		t.Fatalf("saved access token = %q, want original token", saved.AccessToken)
	}
	if saved.RefreshToken != "refresh-token" {
		t.Fatalf("saved refresh token = %q, want original refresh token", saved.RefreshToken)
	}
}

func TestFileTokenSource_BearerToken_RejectsCanceledContextAfterReadingRefreshResponseBeforeSave(t *testing.T) {
	t.Parallel()

	store := NewFileTokenStore(filepath.Join(t.TempDir(), "xai.json"))
	oldAccess := jwtWithExp(time.Now().Add(-time.Hour).Unix())
	if err := store.Save(Token{
		AccessToken:   oldAccess,
		RefreshToken:  "refresh-token",
		TokenType:     "Bearer",
		TokenEndpoint: "https://auth.x.ai/oauth2/token",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	newAccess := jwtWithExp(time.Now().Add(time.Hour).Unix())
	body := &trackingRefreshBody{
		data:         []byte(`{"access_token":"` + newAccess + `","refresh_token":"rotated-refresh"}`),
		cancelOnRead: cancel,
	}
	source := NewFileTokenSource(store)
	source.client = &http.Client{Transport: cancelAfterRefreshRoundTripper{
		cancel: func() {},
		body:   body,
	}}

	got, err := source.BearerToken(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BearerToken() error = %v, want context.Canceled", err)
	}
	if got != "" {
		t.Fatalf("BearerToken() = %q, want empty token on canceled context", got)
	}
	if body.readCalls == 0 {
		t.Fatal("refresh response body was not read before cancellation")
	}
	if !body.closed {
		t.Fatal("refresh response body should be closed")
	}

	saved, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after canceled refresh error = %v", err)
	}
	if saved.AccessToken != oldAccess {
		t.Fatalf("saved access token = %q, want original token", saved.AccessToken)
	}
	if saved.RefreshToken != "refresh-token" {
		t.Fatalf("saved refresh token = %q, want original refresh token", saved.RefreshToken)
	}
}

func TestFileTokenSource_BearerToken_MissingFile(t *testing.T) {
	t.Parallel()

	store := NewFileTokenStore(filepath.Join(t.TempDir(), "missing.json"))
	_, err := NewFileTokenSource(store).BearerToken(t.Context())
	if err == nil {
		t.Fatal("BearerToken() error = nil, want missing token error")
	}
	if !errors.Is(err, ErrTokenNotFound) && !os.IsNotExist(err) {
		t.Fatalf("BearerToken() error = %v, want ErrTokenNotFound", err)
	}
}
