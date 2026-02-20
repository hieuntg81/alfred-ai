//go:build edge

package main

import (
	"log/slog"

	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/infra/config"
)

// registerBLETool registers the BLE tool when running an edge build.
func registerBLETool(cfg *config.Config, reg *tool.Registry, log *slog.Logger) {
	if !cfg.Tools.BLEEnabled {
		return
	}
	backend := tool.NewMockBLEBackend() // TODO: replace with real BLE backend when a suitable Go BLE library is evaluated
	reg.Register(tool.NewBLETool(backend, log))
	log.Info("ble tool enabled (edge build)")
}
