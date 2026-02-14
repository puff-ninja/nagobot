package channel

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/config"
)

// Discord implements the Channel interface using discordgo.
type Discord struct {
	config  config.DiscordConfig
	bus     *bus.MessageBus
	session *discordgo.Session

	typingMu     sync.Mutex
	typingCancel map[string]context.CancelFunc
}

// NewDiscord creates a new Discord channel.
func NewDiscord(cfg config.DiscordConfig, b *bus.MessageBus) *Discord {
	return &Discord{
		config:       cfg,
		bus:          b,
		typingCancel: make(map[string]context.CancelFunc),
	}
}

func (d *Discord) Name() string { return "discord" }

// Start opens the Discord gateway session and blocks until ctx is cancelled.
func (d *Discord) Start(ctx context.Context) error {
	if d.config.Token == "" {
		return fmt.Errorf("discord bot token not configured")
	}

	session, err := discordgo.New("Bot " + d.config.Token)
	if err != nil {
		return fmt.Errorf("create discord session: %w", err)
	}

	session.Identify.Intents = discordgo.Intent(d.config.Intents)
	session.AddHandler(d.onMessageCreate)

	if err := session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}
	d.session = session
	slog.Info("Discord gateway READY")

	<-ctx.Done()
	return ctx.Err()
}

// Stop closes the Discord gateway session.
func (d *Discord) Stop() error {
	d.typingMu.Lock()
	for _, cancel := range d.typingCancel {
		cancel()
	}
	d.typingCancel = make(map[string]context.CancelFunc)
	d.typingMu.Unlock()

	if d.session != nil {
		return d.session.Close()
	}
	return nil
}

// Send sends a message (with optional file attachments) to a Discord channel.
func (d *Discord) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	defer d.stopTyping(msg.ChatID)

	if d.session == nil {
		return fmt.Errorf("discord session not open")
	}

	ms := &discordgo.MessageSend{
		Content: msg.Content,
	}

	if msg.ReplyTo != "" {
		ms.Reference = &discordgo.MessageReference{MessageID: msg.ReplyTo}
		ms.AllowedMentions = &discordgo.MessageAllowedMentions{
			RepliedUser: false,
		}
	}

	// Attach files from local paths.
	var openFiles []*os.File
	defer func() {
		for _, f := range openFiles {
			f.Close()
		}
	}()
	for _, filePath := range msg.Media {
		f, err := os.Open(filePath)
		if err != nil {
			slog.Warn("Failed to open media file", "path", filePath, "err", err)
			continue
		}
		openFiles = append(openFiles, f)
		ms.Files = append(ms.Files, &discordgo.File{
			Name:   filepath.Base(filePath),
			Reader: f,
		})
	}

	if len(ms.Files) > 0 {
		names := make([]string, len(ms.Files))
		for i, f := range ms.Files {
			names[i] = f.Name
		}
		slog.Info("Discord sending media attachments", "channel", msg.ChatID, "files", names)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, err := d.session.ChannelMessageSendComplex(msg.ChatID, ms)
		if err == nil {
			return nil
		}

		// Check for rate limiting via discordgo's REST error.
		if restErr, ok := err.(*discordgo.RESTError); ok {
			if restErr.Response != nil && restErr.Response.StatusCode == 429 {
				slog.Warn("Discord rate limited, retrying", "attempt", attempt)
				time.Sleep(time.Second)
				continue
			}
		}

		lastErr = err
		if attempt < 2 {
			time.Sleep(time.Second)
			continue
		}
	}
	return fmt.Errorf("send discord message: %w", lastErr)
}

func (d *Discord) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	if m.Author.ID == "" || m.ChannelID == "" {
		return
	}
	if !IsAllowed(m.Author.ID, d.config.AllowFrom) {
		return
	}

	content := m.Content
	if content == "" {
		content = "[empty message]"
	}

	d.startTyping(m.ChannelID)

	metadata := map[string]any{
		"message_id": m.ID,
		"guild_id":   m.GuildID,
	}
	if m.ReferencedMessage != nil {
		metadata["reply_to"] = m.ReferencedMessage.ID
	}

	var media []string
	for _, att := range m.Attachments {
		media = append(media, att.URL)
	}

	d.bus.PublishInbound(&bus.InboundMessage{
		Channel:   "discord",
		SenderID:  m.Author.ID,
		ChatID:    m.ChannelID,
		Content:   content,
		Timestamp: time.Now(),
		Media:     media,
		Metadata:  metadata,
	})
}

func (d *Discord) startTyping(channelID string) {
	d.stopTyping(channelID)

	ctx, cancel := context.WithCancel(context.Background())
	d.typingMu.Lock()
	d.typingCancel[channelID] = cancel
	d.typingMu.Unlock()

	go func() {
		for {
			if d.session != nil {
				d.session.ChannelTyping(channelID)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(8 * time.Second):
			}
		}
	}()
}

func (d *Discord) stopTyping(channelID string) {
	d.typingMu.Lock()
	defer d.typingMu.Unlock()
	if cancel, ok := d.typingCancel[channelID]; ok {
		cancel()
		delete(d.typingCancel, channelID)
	}
}
