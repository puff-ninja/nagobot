package cli

import (
	"fmt"
	"os"

	"github.com/joebot/nagobot/internal/config"
)

// RunStatus displays the current configuration status with styled output.
func RunStatus(cfg *config.Config) {
	cfgPath := config.ConfigPath()

	fmt.Println()
	fmt.Println(TitleStyle.Render(fmt.Sprintf("  %s nagobot Status", Logo)))
	fmt.Println()

	fmt.Printf("  %-12s %s  %s\n", "Config", StatusBadge(fileExists(cfgPath)), DimStyle.Render(cfgPath))

	wsPath := cfg.WorkspacePath()
	fmt.Printf("  %-12s %s  %s\n", "Workspace", StatusBadge(fileExists(wsPath)), DimStyle.Render(wsPath))

	fmt.Printf("  %-12s %s\n", "Model", cfg.Agents.Defaults.Model)
	fmt.Println()

	fmt.Println("  " + BoldStyle.Render("Providers"))
	providers := []struct {
		name   string
		config config.ProviderConfig
	}{
		{"Anthropic", cfg.Providers.Anthropic},
		{"OpenAI", cfg.Providers.OpenAI},
		{"OpenRouter", cfg.Providers.OpenRouter},
		{"DeepSeek", cfg.Providers.DeepSeek},
		{"Gemini", cfg.Providers.Gemini},
	}
	for _, p := range providers {
		fmt.Printf("    %s  %s\n", StatusBadge(p.config.APIKey != ""), p.name)
	}
	fmt.Println()

	fmt.Println("  " + BoldStyle.Render("Channels"))
	fmt.Printf("    %s  Discord\n", StatusBadge(cfg.Channels.Discord.Enabled))
	fmt.Println()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
