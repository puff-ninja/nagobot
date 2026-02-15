package config

import "path/filepath"

// Config is the root configuration for nagobot.
type Config struct {
	Agents    AgentsConfig    `json:"agents"`
	Channels  ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Tools     ToolsConfig     `json:"tools"`
	Services  ServicesConfig  `json:"services"`
	MCP       MCPConfig       `json:"mcp"`
}

// MCPConfig holds MCP (Model Context Protocol) settings.
type MCPConfig struct {
	Servers map[string]MCPServerConfig `json:"servers"`
}

// MCPServerConfig holds a single MCP server's configuration.
// Use Command for stdio transport, URL for HTTP transport.
type MCPServerConfig struct {
	// stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// HTTP transport
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
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
	ContextLimit      int     `json:"contextLimit"`
}

// ChannelsConfig holds all channel configurations.
type ChannelsConfig struct {
	Discord DiscordConfig `json:"discord"`
}

// ServicesConfig holds external service configurations.
type ServicesConfig struct {
	GoogleSTT GoogleSTTConfig  `json:"googleStt"`
	Heartbeat HeartbeatConfig  `json:"heartbeat"`
	Cron      CronConfig       `json:"cron"`
}

// CronConfig holds cron service settings.
type CronConfig struct {
	Enabled bool `json:"enabled"`
}

// HeartbeatConfig holds heartbeat service settings.
type HeartbeatConfig struct {
	Enabled    bool `json:"enabled"`
	IntervalS  int  `json:"intervalSeconds"`
}

// GoogleSTTConfig holds Google Cloud Speech-to-Text settings.
type GoogleSTTConfig struct {
	APIKey       string `json:"apiKey"`
	LanguageCode string `json:"languageCode"`
}

// DiscordConfig holds Discord channel settings.
type DiscordConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token"`
	AllowFrom []string `json:"allowFrom"`
	Intents   int      `json:"intents"`
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
	Web                 WebToolsConfig `json:"web"`
	Exec                ExecToolConfig `json:"exec"`
	RestrictToWorkspace bool           `json:"restrictToWorkspace"`
}

// WebToolsConfig holds web tool settings.
type WebToolsConfig struct {
	Search WebSearchConfig `json:"search"`
}

// WebSearchConfig holds Brave Search API settings.
type WebSearchConfig struct {
	APIKey string `json:"apiKey"`
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
				ContextLimit:      80000,
			},
		},
		Channels: ChannelsConfig{
			Discord: DiscordConfig{
				Intents: 37377,
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
