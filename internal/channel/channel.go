package channel

import (
	"context"

	"github.com/joebot/nagobot/internal/bus"
)

// Channel is the interface for chat platform integrations.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Send(ctx context.Context, msg *bus.OutboundMessage) error
}

// IsAllowed checks if a sender is in the allow list.
// Empty allow list means everyone is allowed.
func IsAllowed(senderID string, allowList []string) bool {
	if len(allowList) == 0 {
		return true
	}
	for _, a := range allowList {
		if a == senderID {
			return true
		}
	}
	return false
}
