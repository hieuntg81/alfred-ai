package tool

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"alfred-ai/internal/domain"
)

// ChannelRegistry provides name-based lookup for connected channels.
type ChannelRegistry struct {
	mu       sync.RWMutex
	channels map[string]domain.Channel
}

// NewChannelRegistry builds a registry from the given channel slice.
func NewChannelRegistry(channels []domain.Channel, logger *slog.Logger) *ChannelRegistry {
	m := make(map[string]domain.Channel, len(channels))
	for _, ch := range channels {
		if _, exists := m[ch.Name()]; exists {
			logger.Warn("duplicate channel name", "name", ch.Name())
		}
		m[ch.Name()] = ch
	}
	return &ChannelRegistry{channels: m}
}

// Get retrieves a channel by name. Returns an error if not found.
func (r *ChannelRegistry) Get(name string) (domain.Channel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ch, ok := r.channels[name]
	if !ok {
		return nil, fmt.Errorf("channel %q not found", name)
	}
	return ch, nil
}

// List returns all channel names sorted alphabetically.
func (r *ChannelRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.channels))
	for name := range r.channels {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// All returns all registered channels sorted by name.
func (r *ChannelRegistry) All() []domain.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	chs := make([]domain.Channel, 0, len(r.channels))
	for _, ch := range r.channels {
		chs = append(chs, ch)
	}
	slices.SortFunc(chs, func(a, b domain.Channel) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})
	return chs
}
