package eventbus

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"alfred-ai/internal/domain"
)

// BenchmarkEventBusPublish benchmarks the hot path: publishing events to subscribers
func BenchmarkEventBusPublish(b *testing.B) {
	bus := New(slog.Default())
	ctx := context.Background()
	event := domain.Event{
		Type:      domain.EventMessageReceived,
		Timestamp: time.Now(),
		SessionID: "bench-session",
	}

	// Subscribe a no-op handler
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		// Fast no-op handler
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		bus.Publish(ctx, event)
	}

	bus.Close() // Wait for all dispatched goroutines
}

// BenchmarkEventBusPublishMultipleSubscribers benchmarks with multiple subscribers
func BenchmarkEventBusPublishMultipleSubscribers(b *testing.B) {
	bus := New(slog.Default())
	ctx := context.Background()
	event := domain.Event{
		Type:      domain.EventMessageReceived,
		Timestamp: time.Now(),
		SessionID: "bench-session",
	}

	// Subscribe 10 handlers
	for i := 0; i < 10; i++ {
		bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
			// Fast no-op handler
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		bus.Publish(ctx, event)
	}

	bus.Close()
}

// BenchmarkEventBusPublishAllSubscribers benchmarks SubscribeAll
func BenchmarkEventBusPublishAllSubscribers(b *testing.B) {
	bus := New(slog.Default())
	ctx := context.Background()
	event := domain.Event{
		Type:      domain.EventMessageReceived,
		Timestamp: time.Now(),
	}

	// Subscribe all-events handler
	bus.SubscribeAll(func(_ context.Context, _ domain.Event) {
		// Fast no-op handler
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		bus.Publish(ctx, event)
	}

	bus.Close()
}

// BenchmarkEventBusSubscribe benchmarks subscription operation
func BenchmarkEventBusSubscribe(b *testing.B) {
	bus := New(slog.Default())
	handler := func(_ context.Context, _ domain.Event) {}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		unsub := bus.Subscribe(domain.EventMessageReceived, handler)
		_ = unsub
		// Note: not calling unsub to avoid contention, measuring subscribe only
	}
}

// BenchmarkEventBusUnsubscribe benchmarks unsubscription operation
func BenchmarkEventBusUnsubscribe(b *testing.B) {
	bus := New(slog.Default())
	handler := func(_ context.Context, _ domain.Event) {}

	// Pre-create unsubscribe functions
	unsubs := make([]func(), b.N)
	for i := 0; i < b.N; i++ {
		unsubs[i] = bus.Subscribe(domain.EventMessageReceived, handler)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		unsubs[i]()
	}
}

// BenchmarkEventBusPublishParallel benchmarks concurrent publishing
func BenchmarkEventBusPublishParallel(b *testing.B) {
	bus := New(slog.Default())
	event := domain.Event{
		Type:      domain.EventMessageReceived,
		Timestamp: time.Now(),
	}

	// Subscribe a handler
	bus.Subscribe(domain.EventMessageReceived, func(_ context.Context, _ domain.Event) {
		// Fast no-op handler
	})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			bus.Publish(ctx, event)
		}
	})

	bus.Close()
}

// BenchmarkEventBusPublishNoSubscribers benchmarks publishing with no subscribers (best case)
func BenchmarkEventBusPublishNoSubscribers(b *testing.B) {
	bus := New(slog.Default())
	ctx := context.Background()
	event := domain.Event{
		Type:      domain.EventMessageReceived,
		Timestamp: time.Now(),
	}

	// No subscribers - measures overhead of Publish itself

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		bus.Publish(ctx, event)
	}

	bus.Close()
}
