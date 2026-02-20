//go:build edge

package main

import (
	"log/slog"

	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/infra/config"
)

// registerGPIOTool registers the GPIO tool when running an edge build.
func registerGPIOTool(cfg *config.Config, reg *tool.Registry, log *slog.Logger) {
	if !cfg.Tools.GPIOEnabled {
		return
	}

	// Try real periph.io backend first; fall back to mock if hardware init fails.
	backend, err := tool.NewPeriphGPIOBackend()
	if err != nil {
		log.Warn("periph.io GPIO init failed, using mock backend", "error", err)
		backend = nil
	}

	if backend != nil {
		reg.Register(tool.NewGPIOTool(backend, log))
		log.Info("gpio tool enabled (periph.io hardware backend)")
	} else {
		reg.Register(tool.NewGPIOTool(tool.NewMockGPIOBackend(), log))
		log.Info("gpio tool enabled (mock backend — no hardware detected)")
	}
}
