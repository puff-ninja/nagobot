package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joebot/nagobot/internal/agent"
	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/channel"
	"github.com/joebot/nagobot/internal/cli"
	"github.com/joebot/nagobot/internal/config"
	"github.com/joebot/nagobot/internal/llm"
)

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
		cli.RunOnboard()
	case "version", "--version", "-v":
		fmt.Println(cli.TitleStyle.Render(
			fmt.Sprintf("  %s nagobot v%s", cli.Logo, cli.Version),
		))
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	dim := cli.DimStyle.Render
	fmt.Println()
	fmt.Println(cli.TitleStyle.Render(fmt.Sprintf("  %s nagobot", cli.Logo)) + dim(" — Personal AI Assistant"))
	fmt.Println()
	fmt.Println("  " + cli.BoldStyle.Render("Usage"))
	fmt.Println()
	fmt.Printf("    nagobot %-14s %s\n", "agent", dim("Interactive chat"))
	fmt.Printf("    nagobot %-14s %s\n", "agent -m \"…\"", dim("Single message"))
	fmt.Printf("    nagobot %-14s %s\n", "gateway", dim("Start channel gateway"))
	fmt.Printf("    nagobot %-14s %s\n", "status", dim("Show configuration"))
	fmt.Printf("    nagobot %-14s %s\n", "onboard", dim("Initialize setup"))
	fmt.Printf("    nagobot %-14s %s\n", "version", dim("Show version"))
	fmt.Println()
}

// --- agent command ---

func cmdAgent() {
	cfg := mustLoadConfig()
	provider := mustMakeProvider(cfg)
	redirectLogs()

	msgBus := bus.NewMessageBus()
	loop := agent.NewLoop(agent.LoopConfig{
		Bus:                 msgBus,
		Provider:            provider,
		Workspace:           cfg.WorkspacePath(),
		Model:               cfg.Agents.Defaults.Model,
		MaxIterations:       cfg.Agents.Defaults.MaxToolIterations,
		MemoryWindow:        cfg.Agents.Defaults.MemoryWindow,
		ContextLimit:        cfg.Agents.Defaults.ContextLimit,
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
		if err := cli.RunSingleMessage(loop, ctx, message); err != nil {
			os.Exit(1)
		}
	} else {
		if err := cli.RunChat(loop, ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
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
		ContextLimit:        cfg.Agents.Defaults.ContextLimit,
		ExecTimeout:         cfg.Tools.Exec.Timeout,
		RestrictToWorkspace: cfg.Tools.RestrictToWorkspace,
		BraveAPIKey:         cfg.Tools.Web.Search.APIKey,
	})

	fmt.Println()
	fmt.Println(cli.TitleStyle.Render(fmt.Sprintf("  %s nagobot Gateway", cli.Logo)))
	fmt.Println()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start Discord if enabled
	var discord *channel.Discord
	if cfg.Channels.Discord.Enabled {
		discord = channel.NewDiscord(cfg.Channels.Discord, msgBus, loop.Commands())
		msgBus.Subscribe("discord", func(ctx context.Context, msg *bus.OutboundMessage) error {
			return discord.Send(ctx, msg)
		})
		fmt.Println("  " + cli.OkStyle.Render("✓") + " Discord")
	} else {
		fmt.Println("  " + cli.DimStyle.Render("✗") + " Discord " + cli.DimStyle.Render("(not enabled)"))
	}

	fmt.Println()

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

	fmt.Println(cli.DimStyle.Render("  Press Ctrl+C to stop"))
	<-ctx.Done()
	fmt.Println("\n  Shutting down...")
	if discord != nil {
		discord.Stop()
	}
}

// --- status command ---

func cmdStatus() {
	cfg, _ := config.Load()
	cli.RunStatus(cfg)
}

// --- helpers ---

func redirectLogs() {
	logPath := filepath.Join(config.DataDir(), "agent.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, nil)))
}

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
		fmt.Println()
		fmt.Println(cli.ErrStyle.Render("  Error: No API key configured"))
		fmt.Println(cli.DimStyle.Render("  Set one in ~/.nagobot/config.json under providers section"))
		fmt.Println()
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
