//go:build slack

package channel

import (
	"testing"
)

func TestSlackChannelName(t *testing.T) {
	ch := NewSlackChannel("bot-token", "app-token", newTelegramTestLogger())
	if ch.Name() != "slack" {
		t.Errorf("Name = %q", ch.Name())
	}
}

func TestSlackOptionChannels(t *testing.T) {
	ch := NewSlackChannel("bot", "app", newTelegramTestLogger(), WithSlackChannels([]string{"c1", "c2"}))
	if !ch.channelIDs["c1"] || !ch.channelIDs["c2"] {
		t.Errorf("channelIDs = %v", ch.channelIDs)
	}
}

func TestSlackOptionMentionOnly(t *testing.T) {
	ch := NewSlackChannel("bot", "app", newTelegramTestLogger(), WithSlackMentionOnly(true))
	if !ch.mentionOnly {
		t.Error("mentionOnly should be true")
	}
}

func TestSlackStopBeforeStart(t *testing.T) {
	ch := NewSlackChannel("bot", "app", newTelegramTestLogger())
	if err := ch.Stop(nil); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestSlackNewChannel(t *testing.T) {
	ch := NewSlackChannel("bot", "app", newTelegramTestLogger())
	if ch.botToken != "bot" || ch.appToken != "app" {
		t.Error("tokens not set")
	}
}

func TestSlackMultipleOptions(t *testing.T) {
	ch := NewSlackChannel("bot", "app", newTelegramTestLogger(),
		WithSlackMentionOnly(true),
		WithSlackChannels([]string{"ch1"}),
	)
	if !ch.mentionOnly || !ch.channelIDs["ch1"] {
		t.Error("options not applied correctly")
	}
}
