//go:build !edge

package main

import (
	"log/slog"

	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/infra/config"
)

// registerGPIOTool is a no-op in non-edge builds.
func registerGPIOTool(_ *config.Config, _ *tool.Registry, _ *slog.Logger) {}
