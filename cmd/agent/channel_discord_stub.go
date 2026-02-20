//go:build !discord

package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func buildDiscordChannel(_ config.ChannelConfig, _ *slog.Logger) (domain.Channel, error) {
	return nil, fmt.Errorf("discord channel requires build with -tags discord")
}
