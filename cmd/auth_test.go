package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
)

func TestBuildXAIOAuthStatus_MissingTokenFile(t *testing.T) {
	t.Parallel()

	store := xauth.NewFileTokenStore(filepath.Join(t.TempDir(), "missing.json"))
	status, err := buildXAIOAuthStatus(store, time.Now())
	if err != nil {
		t.Fatalf("buildXAIOAuthStatus() error = %v", err)
	}

	if status.Provider != "xai-oauth" {
		t.Fatalf("Provider = %q, want xai-oauth", status.Provider)
	}
	if status.TokenPath != store.Path() {
		t.Fatalf("TokenPath = %q, want %q", status.TokenPath, store.Path())
	}
	if status.Found {
		t.Fatal("Found = true, want false")
	}
}

func TestBuildXAIOAuthStatus_DoesNotExposeSecrets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	store := xauth.NewFileTokenStore(filepath.Join(t.TempDir(), "xai.json"))
	if err := store.Save(xauth.Token{
		AccessToken:   "secret-access-token",
		RefreshToken:  "secret-refresh-token",
		TokenType:     "Bearer",
		ExpiresAt:     now.Add(time.Hour),
		TokenEndpoint: "https://auth.x.ai/oauth2/token",
		AuthMode:      "oauth_pkce",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	status, err := buildXAIOAuthStatus(store, now)
	if err != nil {
		t.Fatalf("buildXAIOAuthStatus() error = %v", err)
	}

	if !status.Found {
		t.Fatal("Found = false, want true")
	}
	if !status.AccessTokenPresent {
		t.Fatal("AccessTokenPresent = false, want true")
	}
	if !status.RefreshTokenPresent {
		t.Fatal("RefreshTokenPresent = false, want true")
	}
	if status.Expired {
		t.Fatal("Expired = true, want false")
	}
	if status.NeedsRefresh {
		t.Fatal("NeedsRefresh = true, want false")
	}

	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(raw), "secret-access-token") {
		t.Fatal("status JSON exposed access token")
	}
	if strings.Contains(string(raw), "secret-refresh-token") {
		t.Fatal("status JSON exposed refresh token")
	}
}
