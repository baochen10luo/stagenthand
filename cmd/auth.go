package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	appconfig "github.com/baochen10luo/stagenthand/config"
	xauth "github.com/baochen10luo/stagenthand/internal/auth/xai"
	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/spf13/cobra"
)

var (
	xaiAuthTokenPath string
	xaiProbeTimeout  time.Duration
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Inspect authenticated provider state",
}

var authXAICmd = &cobra.Command{
	Use:   "xai",
	Short: "Inspect xAI OAuth credentials",
}

var authXAIStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show xAI OAuth token status without exposing secrets",
	RunE: func(cmd *cobra.Command, args []string) error {
		status, err := buildXAIOAuthStatus(xauth.NewFileTokenStore(currentXAITokenPath()), time.Now())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(status)
	},
}

var authXAIProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Make a minimal xAI OAuth Responses API request",
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenPath := currentXAITokenPath()
		probeCfg := xaiProbeConfig(tokenPath)
		client, err := llm.NewClient("xai-oauth", false, probeCfg)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), xaiProbeTimeout)
		defer cancel()

		raw, err := client.GenerateTransformation(
			ctx,
			"Return compact JSON only.",
			[]byte(`{"task":"Reply with exactly {\"ok\":true}."}`),
		)
		if err != nil {
			return err
		}

		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"ok":         true,
			"provider":   "xai-oauth",
			"model":      xaiProbeModel(probeCfg),
			"token_path": xauth.NewFileTokenStore(tokenPath).Path(),
			"response":   string(raw),
		})
	},
}

type xaiOAuthStatus struct {
	Provider            string `json:"provider"`
	TokenPath           string `json:"token_path"`
	Found               bool   `json:"found"`
	AccessTokenPresent  bool   `json:"access_token_present,omitempty"`
	RefreshTokenPresent bool   `json:"refresh_token_present,omitempty"`
	TokenType           string `json:"token_type,omitempty"`
	ExpiresAt           string `json:"expires_at,omitempty"`
	Expired             bool   `json:"expired,omitempty"`
	NeedsRefresh        bool   `json:"needs_refresh,omitempty"`
	TokenEndpoint       string `json:"token_endpoint,omitempty"`
	AuthMode            string `json:"auth_mode,omitempty"`
}

func buildXAIOAuthStatus(store *xauth.FileTokenStore, now time.Time) (xaiOAuthStatus, error) {
	status := xaiOAuthStatus{
		Provider:  "xai-oauth",
		TokenPath: store.Path(),
	}

	token, err := store.Load()
	if err != nil {
		if errors.Is(err, xauth.ErrTokenNotFound) || errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}

	status.Found = true
	status.AccessTokenPresent = token.AccessToken != ""
	status.RefreshTokenPresent = token.RefreshToken != ""
	status.TokenType = token.TokenType
	status.TokenEndpoint = token.TokenEndpoint
	status.AuthMode = token.AuthMode
	if expiry := token.ExpiryTime(); !expiry.IsZero() {
		status.ExpiresAt = expiry.UTC().Format(time.RFC3339)
		status.Expired = !expiry.After(now)
		status.NeedsRefresh = token.NeedsRefresh(now)
	}

	return status, nil
}

func currentXAITokenPath() string {
	if xaiAuthTokenPath != "" {
		return xaiAuthTokenPath
	}
	if cfg != nil && cfg.XAI.TokenPath != "" {
		return cfg.XAI.TokenPath
	}
	return ""
}

func xaiProbeConfig(tokenPath string) *appconfig.Config {
	if cfg == nil {
		return &appconfig.Config{
			LLM: appconfig.LLMConfig{
				Provider: "xai-oauth",
				Model:    "grok-4.3",
			},
			XAI: appconfig.XAIConfig{
				Model:     "grok-4.3",
				BaseURL:   "https://api.x.ai/v1",
				TokenPath: tokenPath,
			},
		}
	}

	probeCfg := *cfg
	probeCfg.LLM.Provider = "xai-oauth"
	if probeCfg.XAI.Model == "" {
		probeCfg.XAI.Model = "grok-4.3"
	}
	probeCfg.XAI.TokenPath = tokenPath
	return &probeCfg
}

func xaiProbeModel(c *appconfig.Config) string {
	if c == nil {
		return "grok-4.3"
	}
	if c.XAI.Model != "" {
		return c.XAI.Model
	}
	if c.LLM.Model != "" {
		return c.LLM.Model
	}
	return "grok-4.3"
}

func init() {
	authXAICmd.PersistentFlags().StringVar(&xaiAuthTokenPath, "token-path", "", "xAI OAuth token path (default ~/.hermes/auth.json)")
	authXAIProbeCmd.Flags().DurationVar(&xaiProbeTimeout, "timeout", 30*time.Second, "probe timeout")

	authXAICmd.AddCommand(authXAIStatusCmd)
	authXAICmd.AddCommand(authXAIProbeCmd)
	authCmd.AddCommand(authXAICmd)
	rootCmd.AddCommand(authCmd)
}
