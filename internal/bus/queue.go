package bus

import (
	"context"
	"log/slog"
	"sync"
)

// OutboundHandler is a callback for outbound messages on a specific channel.
type OutboundHandler func(ctx context.Context, msg *OutboundMessage) error

// MessageBus decouples chat channels from the agent core using Go channels.
type MessageBus struct {
	Inbound  chan *InboundMessage
	Outbound chan *OutboundMessage

	mu          sync.RWMutex
	subscribers map[string][]OutboundHandler
}

// NewMessageBus creates a new message bus with buffered channels.
func NewMessageBus() *MessageBus {
	return &MessageBus{
		Inbound:     make(chan *InboundMessage, 64),
		Outbound:    make(chan *OutboundMessage, 64),
		subscribers: make(map[string][]OutboundHandler),
	}
}

// PublishInbound sends a message from a channel to the agent.
func (b *MessageBus) PublishInbound(msg *InboundMessage) {
	b.Inbound <- msg
}

// PublishOutbound sends a response from the agent to channels.
func (b *MessageBus) PublishOutbound(msg *OutboundMessage) {
	b.Outbound <- msg
}

// Subscribe registers a handler for outbound messages on a specific channel.
func (b *MessageBus) Subscribe(channel string, handler OutboundHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[channel] = append(b.subscribers[channel], handler)
}

// DispatchOutbound reads from the outbound queue and dispatches to subscribers.
// Blocks until ctx is cancelled.
func (b *MessageBus) DispatchOutbound(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-b.Outbound:
			b.mu.RLock()
			handlers := b.subscribers[msg.Channel]
			b.mu.RUnlock()
			for _, h := range handlers {
				if err := h(ctx, msg); err != nil {
					slog.Warn("dispatch outbound failed, attempting recovery", "channel", msg.Channel, "err", err)
					b.recoverSend(ctx, h, msg, err)
				}
			}
		}
	}
}

// recoverSend tries fallback strategies when an outbound message fails to send.
// It attempts progressively simpler messages, and as a last resort sends a
// short error notification so the user knows something went wrong.
func (b *MessageBus) recoverSend(ctx context.Context, h OutboundHandler, original *OutboundMessage, originalErr error) {
	// Strategy 1: retry without media attachments.
	if len(original.Media) > 0 {
		noMedia := &OutboundMessage{
			Channel: original.Channel,
			ChatID:  original.ChatID,
			Content: original.Content,
		}
		if err := h(ctx, noMedia); err == nil {
			slog.Info("recovery: sent without media attachments", "channel", original.Channel)
			return
		}
	}

	// Strategy 2: retry with truncated content.
	if len(original.Content) > 1500 {
		truncated := &OutboundMessage{
			Channel: original.Channel,
			ChatID:  original.ChatID,
			Content: original.Content[:1500] + "\n\n[message truncated]",
		}
		if err := h(ctx, truncated); err == nil {
			slog.Info("recovery: sent truncated message", "channel", original.Channel)
			return
		}
	}

	// Strategy 3: send a brief error notification to the user.
	fallback := &OutboundMessage{
		Channel: original.Channel,
		ChatID:  original.ChatID,
		Content: "Sorry, I ran into a technical issue and couldn't deliver my response. Please try again.",
	}
	if err := h(ctx, fallback); err != nil {
		slog.Error("recovery: all strategies failed, unable to notify user", "channel", original.Channel, "err", err)
	}
}
