package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/joebot/nagobot/internal/bus"
	"github.com/joebot/nagobot/internal/config"
)

const discordAPIBase = "https://discord.com/api/v10"

// Discord implements the Discord gateway WebSocket protocol.
type Discord struct {
	config     config.DiscordConfig
	bus        *bus.MessageBus
	conn       *websocket.Conn
	seq        *int64
	httpClient *http.Client

	cancel       context.CancelFunc
	typingMu     sync.Mutex
	typingCancel map[string]context.CancelFunc
}

// NewDiscord creates a new Discord channel.
func NewDiscord(cfg config.DiscordConfig, b *bus.MessageBus) *Discord {
	return &Discord{
		config:       cfg,
		bus:          b,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		typingCancel: make(map[string]context.CancelFunc),
	}
}

func (d *Discord) Name() string { return "discord" }

// Start connects to the Discord gateway and processes events.
func (d *Discord) Start(ctx context.Context) error {
	if d.config.Token == "" {
		return fmt.Errorf("discord bot token not configured")
	}

	ctx, d.cancel = context.WithCancel(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		slog.Info("Connecting to Discord gateway...")
		conn, _, err := websocket.Dial(ctx, d.config.GatewayURL, nil)
		if err != nil {
			slog.Warn("Discord gateway dial error", "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		conn.SetReadLimit(1 << 20) // 1MB
		d.conn = conn
		err = d.gatewayLoop(ctx)
		conn.Close(websocket.StatusNormalClosure, "reconnecting")
		d.conn = nil

		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("Discord gateway disconnected, reconnecting in 5s...", "err", err)
		time.Sleep(5 * time.Second)
	}
}

// Stop disconnects from Discord.
func (d *Discord) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	d.typingMu.Lock()
	for _, cancel := range d.typingCancel {
		cancel()
	}
	d.typingCancel = make(map[string]context.CancelFunc)
	d.typingMu.Unlock()
	if d.conn != nil {
		d.conn.Close(websocket.StatusNormalClosure, "shutdown")
	}
	return nil
}

// Send sends a message through the Discord REST API.
func (d *Discord) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	defer d.stopTyping(msg.ChatID)

	url := fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, msg.ChatID)
	payload := map[string]any{"content": msg.Content}

	if msg.ReplyTo != "" {
		payload["message_reference"] = map[string]any{"message_id": msg.ReplyTo}
		payload["allowed_mentions"] = map[string]any{"replied_user": false}
	}

	body, _ := json.Marshal(payload)

	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bot "+d.config.Token)

		resp, err := d.httpClient.Do(req)
		if err != nil {
			if attempt == 2 {
				return fmt.Errorf("send discord message: %w", err)
			}
			time.Sleep(time.Second)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 {
			var rateLimited struct {
				RetryAfter float64 `json:"retry_after"`
			}
			json.Unmarshal(respBody, &rateLimited)
			wait := time.Duration(rateLimited.RetryAfter * float64(time.Second))
			if wait <= 0 {
				wait = time.Second
			}
			slog.Warn("Discord rate limited", "retry_after", wait)
			time.Sleep(wait)
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("discord API error %d: %s", resp.StatusCode, string(respBody))
		}
		return nil
	}
	return fmt.Errorf("discord: max retries exceeded")
}

func (d *Discord) gatewayLoop(ctx context.Context) error {
	for {
		_, data, err := d.conn.Read(ctx)
		if err != nil {
			return err
		}

		var event struct {
			Op int            `json:"op"`
			T  string         `json:"t"`
			S  *int64         `json:"s"`
			D  json.RawMessage `json:"d"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			slog.Warn("Invalid JSON from Discord", "err", err)
			continue
		}

		if event.S != nil {
			d.seq = event.S
		}

		switch event.Op {
		case 10: // HELLO
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			json.Unmarshal(event.D, &hello)
			go d.heartbeatLoop(ctx, time.Duration(hello.HeartbeatInterval)*time.Millisecond)
			d.identify(ctx)

		case 0: // DISPATCH
			switch event.T {
			case "READY":
				slog.Info("Discord gateway READY")
			case "MESSAGE_CREATE":
				d.handleMessageCreate(ctx, event.D)
			}

		case 7: // RECONNECT
			slog.Info("Discord gateway requested reconnect")
			return nil

		case 9: // INVALID_SESSION
			slog.Warn("Discord gateway invalid session")
			return nil
		}
	}
}

func (d *Discord) identify(ctx context.Context) {
	identify := map[string]any{
		"op": 2,
		"d": map[string]any{
			"token":   d.config.Token,
			"intents": d.config.Intents,
			"properties": map[string]any{
				"os":      "nagobot",
				"browser": "nagobot",
				"device":  "nagobot",
			},
		},
	}
	data, _ := json.Marshal(identify)
	d.conn.Write(ctx, websocket.MessageText, data)
}

func (d *Discord) heartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			payload := map[string]any{"op": 1, "d": d.seq}
			data, _ := json.Marshal(payload)
			if err := d.conn.Write(ctx, websocket.MessageText, data); err != nil {
				slog.Warn("Discord heartbeat failed", "err", err)
				return
			}
		}
	}
}

func (d *Discord) handleMessageCreate(ctx context.Context, raw json.RawMessage) {
	var msg struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Author    struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
		ReferencedMessage *struct {
			ID string `json:"id"`
		} `json:"referenced_message"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	if msg.Author.Bot {
		return
	}
	if msg.Author.ID == "" || msg.ChannelID == "" {
		return
	}
	if !IsAllowed(msg.Author.ID, d.config.AllowFrom) {
		return
	}

	content := msg.Content
	if content == "" {
		content = "[empty message]"
	}

	d.startTyping(ctx, msg.ChannelID)

	metadata := map[string]any{
		"message_id": msg.ID,
		"guild_id":   msg.GuildID,
	}
	if msg.ReferencedMessage != nil {
		metadata["reply_to"] = msg.ReferencedMessage.ID
	}

	d.bus.PublishInbound(&bus.InboundMessage{
		Channel:   "discord",
		SenderID:  msg.Author.ID,
		ChatID:    msg.ChannelID,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  metadata,
	})
}

func (d *Discord) startTyping(ctx context.Context, channelID string) {
	d.stopTyping(channelID)

	typingCtx, cancel := context.WithCancel(ctx)
	d.typingMu.Lock()
	d.typingCancel[channelID] = cancel
	d.typingMu.Unlock()

	go func() {
		url := fmt.Sprintf("%s/channels/%s/typing", discordAPIBase, channelID)
		for {
			req, _ := http.NewRequestWithContext(typingCtx, "POST", url, nil)
			req.Header.Set("Authorization", "Bot "+d.config.Token)
			d.httpClient.Do(req)

			select {
			case <-typingCtx.Done():
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
