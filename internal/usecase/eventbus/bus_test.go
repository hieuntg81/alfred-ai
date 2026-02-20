package eventbus

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

func newTestBus() *Bus {
	return New(slog.Default())
}

func newEvent(t domain.EventType) domain.Event {
	return domain.Event{Type: t, Timestamp: time.Now()}
}

func TestPublishSubscribe(t *testing.T) {
	bus := newTestBus()

	var got atomic.Int32
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, e domain.Event) {
		if e.Type == domain.EventMessageReceived {
			got.Add(1)
		}
	})

	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	bus.Close() // drain
	if got.Load() != 1 {
		t.Fatalf("expected 1, got %d", got.Load())
	}
}

func TestSubscribeAll(t *testing.T) {
	bus := newTestBus()

	var got atomic.Int32
	bus.SubscribeAll(func(_ context.Context, _ domain.Event) {
		got.Add(1)
	})

	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	bus.Publish(context.Background(), newEvent(domain.EventToolCallStarted))
	bus.Close()

	if got.Load() != 2 {
		t.Fatalf("expected 2, got %d", got.Load())
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := newTestBus()

	var got atomic.Int32
	unsub := bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		got.Add(1)
	})

	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	bus.Close()
	if got.Load() != 1 {
		t.Fatalf("expected 1 before unsub, got %d", got.Load())
	}

	// Re-create bus since Close was called
	bus = newTestBus()
	unsub2 := bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		got.Add(1)
	})
	_ = unsub // original unsub for old bus

	unsub2()
	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	bus.Close()

	if got.Load() != 1 {
		t.Fatalf("expected still 1 after unsub, got %d", got.Load())
	}
}

func TestConcurrentPublish(t *testing.T) {
	bus := newTestBus()

	var got atomic.Int32
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		got.Add(1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
		}()
	}
	wg.Wait()
	bus.Close()

	if got.Load() != 100 {
		t.Fatalf("expected 100, got %d", got.Load())
	}
}

func TestPanicRecovery(t *testing.T) {
	bus := newTestBus()

	var got atomic.Int32
	// First subscriber panics
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		panic("boom")
	})
	// Second subscriber should still fire
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		got.Add(1)
	})

	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	bus.Close()

	if got.Load() != 1 {
		t.Fatalf("expected 1 (second handler), got %d", got.Load())
	}
}

func TestCloseDrainsAndRejectsNew(t *testing.T) {
	bus := newTestBus()

	var got atomic.Int32
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		time.Sleep(50 * time.Millisecond)
		got.Add(1)
	})

	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	bus.Close() // should block until the handler finishes

	if got.Load() != 1 {
		t.Fatalf("expected handler to have run, got %d", got.Load())
	}

	// After close, new publishes should be no-ops
	bus.Publish(context.Background(), newEvent(domain.EventMessageReceived))
	// Wait a bit to see if spurious delivery happens
	time.Sleep(20 * time.Millisecond)
	if got.Load() != 1 {
		t.Fatalf("expected no delivery after close, got %d", got.Load())
	}
}
