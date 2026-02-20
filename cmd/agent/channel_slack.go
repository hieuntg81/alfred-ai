//go:build slack

package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/adapter/channel"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func buildSlackChannel(cc config.ChannelConfig, log *slog.Logger) (domain.Channel, error) {
	if cc.Slack == nil || cc.Slack.BotToken == "" {
		return nil, fmt.Errorf("slack.bot_token is required")
	}
	var opts []channel.SlackOption
	if len(cc.ChannelIDs) > 0 {
		opts = append(opts, channel.WithSlackChannels(cc.ChannelIDs))
	}
	if cc.MentionOnly {
		opts = append(opts, channel.WithSlackMentionOnly(true))
	}
	return channel.NewSlackChannel(cc.Slack.BotToken, cc.Slack.AppToken, log, opts...), nil
}
