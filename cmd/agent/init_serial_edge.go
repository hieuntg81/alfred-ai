//go:build edge

package main

import (
	"log/slog"

	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/infra/config"
)

// registerSerialTool registers the serial tool when running an edge build.
func registerSerialTool(cfg *config.Config, reg *tool.Registry, log *slog.Logger) {
	if !cfg.Tools.SerialEnabled {
		return
	}
	backend := tool.NewMockSerialBackend() // TODO: replace with real serial backend (e.g. go.bug.st/serial)
	reg.Register(tool.NewSerialTool(backend, log))
	log.Info("serial tool enabled (edge build)")
}
