package eventbus

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"alfred-ai/internal/domain"
)

type subscription struct {
	id      uint64
	handler domain.EventHandler
}

// Bus is an in-process, goroutine-safe event bus.
type Bus struct {
	mu      sync.RWMutex
	typed   map[domain.EventType][]subscription
	allSubs []subscription
	nextID  atomic.Uint64
	logger  *slog.Logger
	wg      sync.WaitGroup
	closed  atomic.Bool
}

// New creates an event bus.
func New(logger *slog.Logger) *Bus {
	return &Bus{
		typed:  make(map[domain.EventType][]subscription),
		logger: logger,
	}
}

// Publish fans out an event to matching typed subscribers and all-event subscribers.
// Each handler is invoked in its own goroutine. Panicking handlers are recovered.
func (b *Bus) Publish(ctx context.Context, event domain.Event) {
	if b.closed.Load() {
		return
	}

	b.mu.RLock()
	typed := make([]subscription, len(b.typed[event.Type]))
	copy(typed, b.typed[event.Type])
	allSubs := make([]subscription, len(b.allSubs))
	copy(allSubs, b.allSubs)
	b.mu.RUnlock()

	for _, sub := range typed {
		b.dispatch(ctx, event, sub)
	}
	for _, sub := range allSubs {
		b.dispatch(ctx, event, sub)
	}
}

func (b *Bus) dispatch(ctx context.Context, event domain.Event, sub subscription) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("event handler panicked",
					"event", string(event.Type),
					"panic", r,
				)
			}
		}()
		sub.handler(ctx, event)
	}()
}

// Subscribe registers a handler for a specific event type.
// Returns an unsubscribe function.
func (b *Bus) Subscribe(eventType domain.EventType, handler domain.EventHandler) func() {
	id := b.nextID.Add(1)
	sub := subscription{id: id, handler: handler}

	b.mu.Lock()
	b.typed[eventType] = append(b.typed[eventType], sub)
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.typed[eventType]
		for i, s := range subs {
			if s.id == id {
				b.typed[eventType] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

// SubscribeAll registers a handler that receives every event.
// Returns an unsubscribe function.
func (b *Bus) SubscribeAll(handler domain.EventHandler) func() {
	id := b.nextID.Add(1)
	sub := subscription{id: id, handler: handler}

	b.mu.Lock()
	b.allSubs = append(b.allSubs, sub)
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.allSubs {
			if s.id == id {
				b.allSubs = append(b.allSubs[:i], b.allSubs[i+1:]...)
				return
			}
		}
	}
}

// Close prevents new publishes and waits for all in-flight handlers to finish.
// Close is idempotent and safe to call multiple times.
func (b *Bus) Close() {
	if b.closed.Swap(true) {
		// Already closed — nothing to drain.
		return
	}
	b.wg.Wait()
}
