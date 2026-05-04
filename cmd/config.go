package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appconfig "github.com/baochen10luo/stagenthand/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage shand configuration profiles",
}

var configUseCmd = &cobra.Command{
	Use:   "use <profile>",
	Short: "Switch active config profile (e.g. local, cloud)",
	Long: `Switches ~/.shand/config.yaml to point at ~/.shand/config-<profile>.yaml.

Examples:
  shand config use local    # switch to aiark local pipeline
  shand config use cloud    # switch back to cloud pipeline`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profile := args[0]
		home, _ := os.UserHomeDir()
		shandDir := filepath.Join(home, ".shand")

		target := filepath.Join(shandDir, "config-"+profile+".yaml")
		if _, err := os.Stat(target); os.IsNotExist(err) {
			return fmt.Errorf("profile %q not found: expected %s", profile, target)
		}

		configPath := filepath.Join(shandDir, "config.yaml")

		// Remove existing config/symlink
		_ = os.Remove(configPath)

		if err := os.Symlink(target, configPath); err != nil {
			return fmt.Errorf("failed to switch profile: %w", err)
		}

		fmt.Fprintf(os.Stderr, "[Info] active profile → %s\n", profile)
		fmt.Fprintf(os.Stderr, "[Info] config.yaml → %s\n", target)
		// Reload config from new symlink so status reflects actual new settings.
		newCfg, _ := appconfig.Load(configPath)
		return printConfigStatusWithCfg(profile, newCfg)
	},
}

var configStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current active config profile and key settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		configPath := filepath.Join(home, ".shand", "config.yaml")
		return printConfigStatus(configPath)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available config profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		shandDir := filepath.Join(home, ".shand")

		entries, err := os.ReadDir(shandDir)
		if err != nil {
			return err
		}

		configPath := filepath.Join(shandDir, "config.yaml")
		activeTarget, _ := os.Readlink(configPath)

		fmt.Println("Available profiles:")
		found := false
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "config-") || !strings.HasSuffix(name, ".yaml") {
				continue
			}
			profile := strings.TrimSuffix(strings.TrimPrefix(name, "config-"), ".yaml")
			fullPath := filepath.Join(shandDir, name)
			marker := "  "
			if fullPath == activeTarget {
				marker = "* "
			}
			fmt.Printf("%s%s\t(%s)\n", marker, profile, fullPath)
			found = true
		}
		if !found {
			fmt.Println("  (no profiles found — create ~/.shand/config-<name>.yaml)")
		}
		return nil
	},
}

func printConfigStatus(configPath string) error {
	profile := "(direct file)"
	if target, err := os.Readlink(configPath); err == nil {
		base := filepath.Base(target)
		base = strings.TrimSuffix(base, ".yaml")
		base = strings.TrimPrefix(base, "config-")
		profile = base
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("profile: (none — no config.yaml found)")
		return nil
	}
	c, _ := appconfig.Load(configPath)
	return printConfigStatusWithCfg(profile, c)
}

func printConfigStatusWithCfg(profile string, c *appconfig.Config) error {
	fmt.Printf("profile:        %s\n", profile)
	if c == nil {
		return nil
	}
	fmt.Printf("llm.provider:   %s\n", c.LLM.Provider)
	fmt.Printf("llm.model:      %s\n", c.LLM.Model)
	if c.LLM.BaseURL != "" {
		fmt.Printf("llm.base_url:   %s\n", c.LLM.BaseURL)
	}
	fmt.Printf("image.provider: %s\n", c.Image.Provider)
	fmt.Printf("audio.voice:    %s\n", voiceProviderLabel(c.Audio.VoiceProvider))
	fmt.Printf("audio.music:    %s\n", musicProviderLabel(c.Audio.MusicProvider))
	return nil
}

func voiceProviderLabel(p string) string {
	if p == "" {
		return "polly (default)"
	}
	return p
}

func musicProviderLabel(p string) string {
	if p == "" {
		return "jamendo (default)"
	}
	return p
}

func init() {
	configCmd.AddCommand(configUseCmd)
	configCmd.AddCommand(configStatusCmd)
	configCmd.AddCommand(configListCmd)
	rootCmd.AddCommand(configCmd)
}
