//go:build !edge

package main

import (
	"log/slog"

	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/infra/config"
)

// registerBLETool is a no-op in non-edge builds.
func registerBLETool(_ *config.Config, _ *tool.Registry, _ *slog.Logger) {}
