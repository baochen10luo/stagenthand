// Package config handles configuration loading via viper.
// Priority: flag > env > config file > defaults.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the top-level configuration structure.
type Config struct {
	LLM      LLMConfig      `mapstructure:"llm"`
	Image    ImageConfig    `mapstructure:"image"`
	Audio    AudioConfig    `mapstructure:"audio"`
	Video    VideoConfig    `mapstructure:"video"`
	Remotion RemotionConfig `mapstructure:"remotion"`
	Notify   NotifyConfig   `mapstructure:"notify"`
	Store    StoreConfig    `mapstructure:"store"`
	Server   ServerConfig   `mapstructure:"server"`
}

// LLMConfig holds language-model provider settings.
type LLMConfig struct {
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
	APIKey   string `mapstructure:"api_key"`
	BaseURL  string `mapstructure:"base_url"`
	// AWS Bedrock specific
	AWSAccessKeyID     string `mapstructure:"aws_access_key_id"`
	AWSSecretAccessKey string `mapstructure:"aws_secret_access_key"`
	AWSRegion          string `mapstructure:"aws_region"`
}

// ImageConfig holds image-generation provider settings.
type ImageConfig struct {
	Provider         string `mapstructure:"provider"`
	Model            string `mapstructure:"model"`
	APIKey           string `mapstructure:"api_key"` // Alias for AccessKeyID in AWS
	SecretKey        string `mapstructure:"secret_key"`
	Region           string `mapstructure:"region"`
	Width            int    `mapstructure:"width"`
	Height           int    `mapstructure:"height"`
	CharacterRefsDir string `mapstructure:"character_refs_dir"`
}

// AudioConfig holds audio-generation/BGM provider settings.
type AudioConfig struct {
	VoiceProvider   string `mapstructure:"voice_provider"`
	MusicProvider   string `mapstructure:"music_provider"`
	JamendoClientID string `mapstructure:"jamendo_client_id"`
}

// VideoConfig holds video-generation provider settings.
type VideoConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Provider string `mapstructure:"provider"` // "remotion" | "nova_reel"
	APIKey   string `mapstructure:"api_key"`
	// Nova Reel specific
	S3Bucket string `mapstructure:"s3_bucket"`
	Region   string `mapstructure:"region"`
}

// RemotionConfig holds Remotion render settings.
type RemotionConfig struct {
	TemplatePath string `mapstructure:"template_path"`
	Composition  string `mapstructure:"composition"`
}

// NotifyConfig holds notification settings.
type NotifyConfig struct {
	DiscordWebhook string `mapstructure:"discord_webhook"`
}

// StoreConfig holds SQLite persistence settings.
type StoreConfig struct {
	DBPath string `mapstructure:"db_path"`
}

// ServerConfig holds the HTTP API server settings.
type ServerConfig struct {
	Port int `mapstructure:"port"`
}

// Load reads configuration from the given file path (may be empty for defaults only).
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("llm.provider", "openai")
	v.SetDefault("llm.model", "gpt-4o")
	v.SetDefault("llm.aws_region", "us-east-1")
	v.SetDefault("image.provider", "nanobanana")
	v.SetDefault("image.model", "amazon.titan-image-generator-v2:0")
	v.SetDefault("image.region", "us-west-2")
	v.SetDefault("image.width", 1024)
	v.SetDefault("image.height", 576)
	v.SetDefault("video.enabled", false)
	v.SetDefault("video.provider", "grok")
	v.SetDefault("remotion.composition", "ShortDrama")
	v.SetDefault("store.db_path", "~/.shand/shand.db")
	v.SetDefault("server.port", 28080)

	// Env vars: SHAND_LLM_PROVIDER, SHAND_IMAGE_API_KEY, ...
	v.SetEnvPrefix("SHAND")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Config file
	if cfgFile == "" {
		home, _ := os.UserHomeDir()
		cfgFile = filepath.Join(home, ".shand", "config.yaml")
	}

	v.SetConfigFile(cfgFile)
	if err := v.ReadInConfig(); err != nil {
		// Ignore if the default config file does not exist.
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
