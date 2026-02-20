//go:build !slack

package main

import (
	"fmt"
	"log/slog"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
)

func buildSlackChannel(_ config.ChannelConfig, _ *slog.Logger) (domain.Channel, error) {
	return nil, fmt.Errorf("slack channel requires build with -tags slack")
}
