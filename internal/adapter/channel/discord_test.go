//go:build discord

package channel

import (
	"testing"
)

func TestDiscordChannelName(t *testing.T) {
	ch := NewDiscordChannel("token", newTelegramTestLogger())
	if ch.Name() != "discord" {
		t.Errorf("Name = %q", ch.Name())
	}
}

func TestDiscordOptionGuild(t *testing.T) {
	ch := NewDiscordChannel("token", newTelegramTestLogger(), WithDiscordGuild("guild1"))
	if ch.guildID != "guild1" {
		t.Errorf("guildID = %q", ch.guildID)
	}
}

func TestDiscordOptionChannels(t *testing.T) {
	ch := NewDiscordChannel("token", newTelegramTestLogger(), WithDiscordChannels([]string{"c1", "c2"}))
	if !ch.channelIDs["c1"] || !ch.channelIDs["c2"] {
		t.Errorf("channelIDs = %v", ch.channelIDs)
	}
}

func TestDiscordOptionMentionOnly(t *testing.T) {
	ch := NewDiscordChannel("token", newTelegramTestLogger(), WithDiscordMentionOnly(true))
	if !ch.mentionOnly {
		t.Error("mentionOnly should be true")
	}
}

func TestDiscordStopBeforeStart(t *testing.T) {
	ch := NewDiscordChannel("token", newTelegramTestLogger())
	if err := ch.Stop(nil); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestDiscordNewChannel(t *testing.T) {
	ch := NewDiscordChannel("tok", newTelegramTestLogger())
	if ch.token != "tok" {
		t.Errorf("token = %q", ch.token)
	}
}

func TestDiscordMultipleOptions(t *testing.T) {
	ch := NewDiscordChannel("tok", newTelegramTestLogger(),
		WithDiscordGuild("g"),
		WithDiscordMentionOnly(true),
		WithDiscordChannels([]string{"ch1"}),
	)
	if ch.guildID != "g" || !ch.mentionOnly || !ch.channelIDs["ch1"] {
		t.Error("options not applied correctly")
	}
}
