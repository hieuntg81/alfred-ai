//go:build discord

package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/adapter/channel"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func buildDiscordChannel(cc config.ChannelConfig, log *slog.Logger) (domain.Channel, error) {
	if cc.Discord == nil || cc.Discord.Token == "" {
		return nil, fmt.Errorf("discord.token is required")
	}
	var opts []channel.DiscordOption
	if cc.Discord.GuildID != "" {
		opts = append(opts, channel.WithDiscordGuild(cc.Discord.GuildID))
	}
	if len(cc.ChannelIDs) > 0 {
		opts = append(opts, channel.WithDiscordChannels(cc.ChannelIDs))
	}
	if cc.MentionOnly {
		opts = append(opts, channel.WithDiscordMentionOnly(true))
	}
	return channel.NewDiscordChannel(cc.Discord.Token, log, opts...), nil
}
