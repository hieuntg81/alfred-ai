package tool

import (
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

func TestChannelRegistryGet(t *testing.T) {
	reg := NewChannelRegistry([]domain.Channel{
		&mockChannel{name: "telegram"},
		&mockChannel{name: "discord"},
	}, newTestLogger())

	ch, err := reg.Get("telegram")
	if err != nil {
		t.Fatalf("Get telegram: %v", err)
	}
	if ch.Name() != "telegram" {
		t.Errorf("got name %q, want %q", ch.Name(), "telegram")
	}
}

func TestChannelRegistryGetNotFound(t *testing.T) {
	reg := NewChannelRegistry([]domain.Channel{
		&mockChannel{name: "telegram"},
	}, newTestLogger())

	_, err := reg.Get("slack")
	if err == nil {
		t.Fatal("expected error for missing channel")
	}
}

func TestChannelRegistryList(t *testing.T) {
	reg := NewChannelRegistry([]domain.Channel{
		&mockChannel{name: "telegram"},
		&mockChannel{name: "discord"},
		&mockChannel{name: "slack"},
	}, newTestLogger())

	names := reg.List()
	if len(names) != 3 {
		t.Fatalf("got %d channels, want 3", len(names))
	}
	// List returns sorted names
	if names[0] != "discord" || names[1] != "slack" || names[2] != "telegram" {
		t.Errorf("got %v, want [discord slack telegram]", names)
	}
}

func TestChannelRegistryListEmpty(t *testing.T) {
	reg := NewChannelRegistry(nil, newTestLogger())

	names := reg.List()
	if len(names) != 0 {
		t.Errorf("got %d channels, want 0", len(names))
	}
}

func TestChannelRegistryAll(t *testing.T) {
	reg := NewChannelRegistry([]domain.Channel{
		&mockChannel{name: "telegram"},
		&mockChannel{name: "discord"},
	}, newTestLogger())

	chs := reg.All()
	if len(chs) != 2 {
		t.Fatalf("got %d channels, want 2", len(chs))
	}
	// All returns channels sorted by name
	if chs[0].Name() != "discord" || chs[1].Name() != "telegram" {
		t.Errorf("got [%s %s], want [discord telegram]", chs[0].Name(), chs[1].Name())
	}
}

func TestChannelRegistryConcurrentAccess(t *testing.T) {
	reg := NewChannelRegistry([]domain.Channel{
		&mockChannel{name: "telegram"},
		&mockChannel{name: "discord"},
	}, newTestLogger())

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.List()
			reg.Get("telegram")
			reg.All()
		}()
	}
	wg.Wait()
}

func TestChannelRegistryDuplicateName(t *testing.T) {
	first := &mockChannel{name: "telegram"}
	second := &mockChannel{name: "telegram"}

	reg := NewChannelRegistry([]domain.Channel{first, second}, newTestLogger())

	// Should have only one entry (last wins)
	names := reg.List()
	if len(names) != 1 {
		t.Fatalf("got %d channels, want 1", len(names))
	}

	ch, err := reg.Get("telegram")
	if err != nil {
		t.Fatalf("Get telegram: %v", err)
	}
	// The second channel should be the one stored
	if ch != second {
		t.Error("expected last registered channel to win")
	}
}
