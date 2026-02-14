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
					slog.Error("dispatch outbound", "channel", msg.Channel, "err", err)
				}
			}
		}
	}
}
