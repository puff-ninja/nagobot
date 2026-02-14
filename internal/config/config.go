package config

import "path/filepath"

// Config is the root configuration for nagobot.
type Config struct {
	Agents    AgentsConfig    `json:"agents"`
	Channels  ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Tools     ToolsConfig     `json:"tools"`
}

// WorkspacePath returns the expanded workspace path.
func (c *Config) WorkspacePath() string {
	return expandHome(c.Agents.Defaults.Workspace)
}

// AgentsConfig holds agent settings.
type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

// AgentDefaults holds default agent parameters.
type AgentDefaults struct {
	Workspace         string  `json:"workspace"`
	Model             string  `json:"model"`
	MaxTokens         int     `json:"maxTokens"`
	Temperature       float64 `json:"temperature"`
	MaxToolIterations int     `json:"maxToolIterations"`
	MemoryWindow      int     `json:"memoryWindow"`
}

// ChannelsConfig holds all channel configurations.
type ChannelsConfig struct {
	Discord DiscordConfig `json:"discord"`
}

// DiscordConfig holds Discord channel settings.
type DiscordConfig struct {
	Enabled    bool     `json:"enabled"`
	Token      string   `json:"token"`
	AllowFrom  []string `json:"allowFrom"`
	GatewayURL string   `json:"gatewayUrl"`
	Intents    int      `json:"intents"`
}

// ProvidersConfig holds LLM provider settings.
type ProvidersConfig struct {
	Anthropic  ProviderConfig `json:"anthropic"`
	OpenAI     ProviderConfig `json:"openai"`
	OpenRouter ProviderConfig `json:"openrouter"`
	DeepSeek   ProviderConfig `json:"deepseek"`
	Gemini     ProviderConfig `json:"gemini"`
}

// ProviderConfig holds a single LLM provider's credentials.
type ProviderConfig struct {
	APIKey       string            `json:"apiKey"`
	APIBase      string            `json:"apiBase,omitempty"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
}

// ToolsConfig holds tool settings.
type ToolsConfig struct {
	Exec                ExecToolConfig `json:"exec"`
	RestrictToWorkspace bool           `json:"restrictToWorkspace"`
}

// ExecToolConfig holds shell exec tool settings.
type ExecToolConfig struct {
	Timeout int `json:"timeout"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         "~/.nagobot/workspace",
				Model:             "anthropic/claude-sonnet-4-5",
				MaxTokens:         8192,
				Temperature:       0.7,
				MaxToolIterations: 20,
				MemoryWindow:      50,
			},
		},
		Channels: ChannelsConfig{
			Discord: DiscordConfig{
				GatewayURL: "wss://gateway.discord.gg/?v=10&encoding=json",
				Intents:    37377,
			},
		},
		Tools: ToolsConfig{
			Exec: ExecToolConfig{Timeout: 60},
		},
	}
}

// ProviderMatch holds a matched provider config and its name.
type ProviderMatch struct {
	Name   string
	Config *ProviderConfig
}

// GetProvider returns the first provider config with an API key set,
// matching by model keyword if possible. Returns name and config.
func (c *Config) GetProvider() *ProviderMatch {
	model := c.Agents.Defaults.Model

	// Try keyword match first
	providers := []struct {
		name     string
		keywords []string
		config   *ProviderConfig
	}{
		{"anthropic", []string{"anthropic", "claude"}, &c.Providers.Anthropic},
		{"openai", []string{"openai", "gpt"}, &c.Providers.OpenAI},
		{"openrouter", []string{"openrouter"}, &c.Providers.OpenRouter},
		{"deepseek", []string{"deepseek"}, &c.Providers.DeepSeek},
		{"gemini", []string{"gemini"}, &c.Providers.Gemini},
	}

	for _, p := range providers {
		for _, kw := range p.keywords {
			if containsIgnoreCase(model, kw) && p.config.APIKey != "" {
				return &ProviderMatch{Name: p.name, Config: p.config}
			}
		}
	}

	// Fallback: first with API key
	for _, p := range providers {
		if p.config.APIKey != "" {
			return &ProviderMatch{Name: p.name, Config: p.config}
		}
	}

	return nil
}

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home := homeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
