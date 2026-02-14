package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/joebot/nagobot/internal/agent"
	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/channel"
	"github.com/joebot/nagobot/internal/config"
	"github.com/joebot/nagobot/internal/llm"
)

const version = "0.1.0"
const logo = "🤖"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "agent":
		cmdAgent()
	case "gateway":
		cmdGateway()
	case "status":
		cmdStatus()
	case "onboard":
		cmdOnboard()
	case "version", "--version", "-v":
		fmt.Printf("%s nagobot v%s\n", logo, version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`%s nagobot - Personal AI Assistant (Go)

Usage:
  nagobot agent [-m "message"]   Chat with the agent
  nagobot gateway                Start gateway (connects channels)
  nagobot status                 Show configuration status
  nagobot onboard                Initialize config and workspace
  nagobot version                Show version
`, logo)
}

// --- agent command ---

func cmdAgent() {
	cfg := mustLoadConfig()
	provider := mustMakeProvider(cfg)

	msgBus := bus.NewMessageBus()

	loop := agent.NewLoop(agent.LoopConfig{
		Bus:                 msgBus,
		Provider:            provider,
		Workspace:           cfg.WorkspacePath(),
		Model:               cfg.Agents.Defaults.Model,
		MaxIterations:       cfg.Agents.Defaults.MaxToolIterations,
		MemoryWindow:        cfg.Agents.Defaults.MemoryWindow,
		ExecTimeout:         cfg.Tools.Exec.Timeout,
		RestrictToWorkspace: cfg.Tools.RestrictToWorkspace,
		BraveAPIKey:         cfg.Tools.Web.Search.APIKey,
	})

	// Check for -m flag
	message := ""
	for i := 2; i < len(os.Args); i++ {
		if (os.Args[i] == "-m" || os.Args[i] == "--message") && i+1 < len(os.Args) {
			message = os.Args[i+1]
			break
		}
	}

	ctx := context.Background()

	if message != "" {
		// Single message mode
		resp, err := loop.ProcessDirect(ctx, message, "cli:default")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n%s nagobot\n%s\n\n", logo, resp)
	} else {
		// Interactive mode
		fmt.Printf("%s Interactive mode (type 'exit' or Ctrl+C to quit)\n\n", logo)

		ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("You: ")
			if !scanner.Scan() {
				break
			}
			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}
			if isExitCommand(input) {
				fmt.Println("\nGoodbye!")
				break
			}

			resp, err := loop.ProcessDirect(ctx, input, "cli:default")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				continue
			}
			fmt.Printf("\n%s nagobot\n%s\n\n", logo, resp)
		}
	}
}

// --- gateway command ---

func cmdGateway() {
	cfg := mustLoadConfig()
	provider := mustMakeProvider(cfg)

	msgBus := bus.NewMessageBus()

	loop := agent.NewLoop(agent.LoopConfig{
		Bus:                 msgBus,
		Provider:            provider,
		Workspace:           cfg.WorkspacePath(),
		Model:               cfg.Agents.Defaults.Model,
		MaxIterations:       cfg.Agents.Defaults.MaxToolIterations,
		MemoryWindow:        cfg.Agents.Defaults.MemoryWindow,
		ExecTimeout:         cfg.Tools.Exec.Timeout,
		RestrictToWorkspace: cfg.Tools.RestrictToWorkspace,
		BraveAPIKey:         cfg.Tools.Web.Search.APIKey,
	})

	fmt.Printf("%s Starting nagobot gateway...\n", logo)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start Discord if enabled
	var discord *channel.Discord
	if cfg.Channels.Discord.Enabled {
		discord = channel.NewDiscord(cfg.Channels.Discord, msgBus)
		msgBus.Subscribe("discord", func(ctx context.Context, msg *bus.OutboundMessage) error {
			return discord.Send(ctx, msg)
		})
		fmt.Println("[ok] Discord channel enabled")
	} else {
		fmt.Println("[--] Discord channel not enabled")
	}

	// Start components
	go msgBus.DispatchOutbound(ctx)
	go loop.Run(ctx)

	if discord != nil {
		go func() {
			if err := discord.Start(ctx); err != nil && ctx.Err() == nil {
				slog.Error("Discord channel error", "err", err)
			}
		}()
	}

	fmt.Printf("%s Gateway running. Press Ctrl+C to stop.\n", logo)
	<-ctx.Done()

	fmt.Println("\nShutting down...")
	if discord != nil {
		discord.Stop()
	}
}

// --- status command ---

func cmdStatus() {
	cfgPath := config.ConfigPath()
	cfg, _ := config.Load()

	fmt.Printf("%s nagobot Status\n\n", logo)

	exists := "[ok]"
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		exists = "[no]"
	}
	fmt.Printf("Config: %s %s\n", cfgPath, exists)

	wsPath := cfg.WorkspacePath()
	wsExists := "[ok]"
	if _, err := os.Stat(wsPath); os.IsNotExist(err) {
		wsExists = "[no]"
	}
	fmt.Printf("Workspace: %s %s\n", wsPath, wsExists)
	fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)

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
		status := "not set"
		if p.config.APIKey != "" {
			status = "[ok]"
		}
		fmt.Printf("%s: %s\n", p.name, status)
	}

	fmt.Printf("\nDiscord: ")
	if cfg.Channels.Discord.Enabled {
		fmt.Println("[ok] enabled")
	} else {
		fmt.Println("not enabled")
	}
}

// --- onboard command ---

func cmdOnboard() {
	cfgPath := config.ConfigPath()
	scanner := bufio.NewScanner(os.Stdin)

	var cfg *config.Config

	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Config already exists at %s\n", cfgPath)
		fmt.Println("  [u] Upgrade — add new fields, keep your existing values")
		fmt.Println("  [o] Overwrite — replace with fresh defaults")
		fmt.Println("  [s] Skip — do not modify config")
		fmt.Print("Choose [u/o/s]: ")
		choice := ""
		if scanner.Scan() {
			choice = strings.ToLower(strings.TrimSpace(scanner.Text()))
		}
		switch choice {
		case "u", "upgrade":
			upgraded, err := config.Upgrade()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error upgrading config: %s\n", err)
				os.Exit(1)
			}
			cfg = upgraded
			fmt.Printf("[ok] Upgraded config at %s\n", cfgPath)
		case "o", "overwrite":
			cfg = config.DefaultConfig()
			if err := config.Save(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %s\n", err)
				os.Exit(1)
			}
			fmt.Printf("[ok] Overwritten config at %s\n", cfgPath)
		default:
			fmt.Println("[--] Config unchanged")
			cfg, _ = config.Load()
		}
	} else {
		cfg = config.DefaultConfig()
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("[ok] Created config at %s\n", cfgPath)
	}

	ws := cfg.WorkspacePath()
	os.MkdirAll(ws, 0o755)
	fmt.Printf("[ok] Created workspace at %s\n", ws)

	createWorkspaceTemplates(ws)

	fmt.Printf("\n%s nagobot is ready!\n", logo)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Add your API key to ~/.nagobot/config.json")
	fmt.Println("  2. Chat: nagobot agent -m \"Hello!\"")
}

func createWorkspaceTemplates(workspace string) {
	templates := map[string]string{
		"AGENTS.md": `# Agent Instructions

You are a helpful AI assistant. Be concise, accurate, and friendly.

## Guidelines

- Always explain what you're doing before taking actions
- Ask for clarification when the request is ambiguous
- Use tools to help accomplish tasks
- Remember important information in your memory files
`,
		"SOUL.md": `# Soul

I am nagobot, a lightweight AI assistant.

## Personality

- Helpful and friendly
- Concise and to the point
- Curious and eager to learn
`,
		"USER.md": `# User

Information about the user goes here.

## Preferences

- Communication style: (casual/formal)
- Timezone: (your timezone)
- Language: (your preferred language)
`,
	}

	for filename, content := range templates {
		path := filepath.Join(workspace, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte(content), 0o644)
			fmt.Printf("  Created %s\n", filename)
		}
	}

	memDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memDir, 0o755)
	memFile := filepath.Join(memDir, "MEMORY.md")
	if _, err := os.Stat(memFile); os.IsNotExist(err) {
		os.WriteFile(memFile, []byte(`# Long-term Memory

This file stores important information that should persist across sessions.
`), 0o644)
		fmt.Println("  Created memory/MEMORY.md")
	}

	skillsDir := filepath.Join(workspace, "skills")
	os.MkdirAll(skillsDir, 0o755)
	fmt.Println("  Created skills/")
}

// --- helpers ---

func mustLoadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", err)
	}
	return cfg
}

func mustMakeProvider(cfg *config.Config) llm.Provider {
	match := cfg.GetProvider()
	if match == nil || match.Config.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: No API key configured.")
		fmt.Fprintln(os.Stderr, "Set one in ~/.nagobot/config.json under providers section")
		os.Exit(1)
	}

	p := match.Config
	model := cfg.Agents.Defaults.Model

	switch match.Name {
	case "anthropic":
		return llm.NewAnthropicProvider(p.APIKey, p.APIBase, model, p.ExtraHeaders)
	default:
		return llm.NewOpenAIProvider(p.APIKey, p.APIBase, model, p.ExtraHeaders)
	}
}

func isExitCommand(s string) bool {
	s = strings.ToLower(s)
	return s == "exit" || s == "quit" || s == "/exit" || s == "/quit" || s == ":q"
}
