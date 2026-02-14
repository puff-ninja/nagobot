package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ConfigPath returns the default config file path.
func ConfigPath() string {
	return filepath.Join(homeDir(), ".nagobot", "config.json")
}

// DataDir returns the nanobot data directory.
func DataDir() string {
	dir := filepath.Join(homeDir(), ".nagobot")
	os.MkdirAll(dir, 0o755)
	return dir
}

// Load reads configuration from disk, falling back to defaults.
func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
}

// LoadFrom reads configuration from a specific path.
func LoadFrom(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	// Parse as generic map first for camelCase→snake_case conversion
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	// Convert camelCase keys to Go-friendly format and re-marshal
	converted := convertKeys(raw)
	reData, _ := json.Marshal(converted)
	if err := json.Unmarshal(reData, cfg); err != nil {
		return cfg, fmt.Errorf("apply config: %w", err)
	}

	// Apply defaults for zero values
	if cfg.Agents.Defaults.Workspace == "" {
		cfg.Agents.Defaults.Workspace = "~/.nagobot/workspace"
	}
	if cfg.Agents.Defaults.MaxToolIterations == 0 {
		cfg.Agents.Defaults.MaxToolIterations = 20
	}
	if cfg.Agents.Defaults.MaxTokens == 0 {
		cfg.Agents.Defaults.MaxTokens = 8192
	}
	if cfg.Agents.Defaults.MemoryWindow == 0 {
		cfg.Agents.Defaults.MemoryWindow = 50
	}
	if cfg.Agents.Defaults.ContextLimit == 0 {
		cfg.Agents.Defaults.ContextLimit = 80000
	}
	if cfg.Channels.Discord.GatewayURL == "" {
		cfg.Channels.Discord.GatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	if cfg.Channels.Discord.Intents == 0 {
		cfg.Channels.Discord.Intents = 37377
	}
	if cfg.Tools.Exec.Timeout == 0 {
		cfg.Tools.Exec.Timeout = 60
	}

	return cfg, nil
}

// Save writes configuration to disk in camelCase JSON format.
func Save(cfg *Config) error {
	return SaveTo(cfg, ConfigPath())
}

// SaveTo writes configuration to a specific path.
func SaveTo(cfg *Config, path string) error {
	os.MkdirAll(filepath.Dir(path), 0o755)

	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	// Convert to camelCase
	var raw map[string]any
	json.Unmarshal(data, &raw)
	camel := convertToCamel(raw)

	out, err := json.MarshalIndent(camel, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, out, 0o644)
}

// convertKeys converts camelCase map keys to the JSON tag format (which is already camelCase).
// Since our Go struct tags already use camelCase, we just pass through.
// This function handles nested maps/slices.
func convertKeys(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, val := range v {
			// Convert camelCase key to the JSON struct tag format
			// Our tags are camelCase, so just pass through
			result[k] = convertKeys(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = convertKeys(item)
		}
		return result
	default:
		return data
	}
}

// convertToCamel converts snake_case keys to camelCase for output.
func convertToCamel(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, val := range v {
			result[snakeToCamel(k)] = convertToCamel(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = convertToCamel(item)
		}
		return result
	default:
		return data
	}
}

func camelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result = append(result, '_')
		}
		result = append(result, unicode.ToLower(r))
	}
	return string(result)
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// Upgrade reads the existing config file, deep-merges it on top of
// DefaultConfig (local values win), and saves the result.
// New fields from defaults are added; existing user values are preserved.
func Upgrade() (*Config, error) {
	path := ConfigPath()
	defaults := DefaultConfig()

	// Serialize defaults to map
	defaultData, _ := json.Marshal(defaults)
	var defaultMap map[string]any
	json.Unmarshal(defaultData, &defaultMap)

	// Read existing local config as raw map
	localData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var localMap map[string]any
	if err := json.Unmarshal(localData, &localMap); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Deep merge: local values override defaults
	merged := deepMerge(defaultMap, localMap)

	// Re-serialize through the struct to normalize and apply zero-value defaults
	cfg := DefaultConfig()
	converted := convertKeys(merged)
	reData, _ := json.Marshal(converted)
	if err := json.Unmarshal(reData, cfg); err != nil {
		return nil, fmt.Errorf("apply merged config: %w", err)
	}

	if err := Save(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// deepMerge recursively merges src into dst. Values from src take priority.
// For nested maps, merge recursively. For all other types, src wins.
func deepMerge(dst, src map[string]any) map[string]any {
	result := make(map[string]any, len(dst))
	for k, v := range dst {
		result[k] = v
	}
	for k, srcVal := range src {
		dstVal, exists := result[k]
		if !exists {
			result[k] = srcVal
			continue
		}
		// If both are maps, merge recursively
		dstMap, dstOK := dstVal.(map[string]any)
		srcMap, srcOK := srcVal.(map[string]any)
		if dstOK && srcOK {
			result[k] = deepMerge(dstMap, srcMap)
		} else {
			result[k] = srcVal
		}
	}
	return result
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp"
	}
	return home
}
