package main

import (
	"context"
	"log/slog"

	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/plugin"
	"alfred-ai/internal/usecase"
	"alfred-ai/internal/usecase/node"
)

// FeatureComponents holds optional feature components
type FeatureComponents struct {
	PluginManager  *plugin.Manager
	NodeManager    *node.Manager
	NodeAuth       *node.Auth
	PrivacyManager *usecase.PrivacyManager
	Curator        *usecase.Curator
}

// initFeatures initializes optional features (plugins, nodes, privacy, curator)
// Returns the components and any error
func initFeatures(
	ctx context.Context,
	cfg *config.Config,
	llmProvider domain.LLMProvider,
	mem domain.MemoryProvider,
	toolRegistry *tool.Registry,
	security *SecurityComponents,
	bus domain.EventBus,
	log *slog.Logger,
) (*FeatureComponents, error) {
	comp := &FeatureComponents{}

	// 1. Init plugin system (if enabled, excluded from edge builds)
	if cfg.Plugins.Enabled && !edgeBuild {
		pluginMgr := plugin.NewManager(log, bus, cfg.Plugins.Dirs,
			cfg.Plugins.AllowPermissions, cfg.Plugins.DenyPermissions)
		manifests, err := pluginMgr.Discover()
		if err != nil {
			log.Warn("plugin discovery failed", "error", err)
		} else {
			log.Info("plugins discovered", "count", len(manifests))
		}
		comp.PluginManager = pluginMgr
	}

	// 2. Init node system (if enabled, excluded from edge builds)
	if cfg.Nodes.Enabled && !edgeBuild {
		nodeAuth := node.NewAuth()
		nodeMgr := node.NewManager(
			buildNodeInvoker(cfg.Nodes.InvokeTimeout, log),
			buildNodeDiscoverer(log),
			nodeAuth, bus, security.AuditLogger,
			node.ManagerConfig{
				HeartbeatInterval: cfg.Nodes.HeartbeatInterval,
				InvokeTimeout:     cfg.Nodes.InvokeTimeout,
				AllowedNodes:      cfg.Nodes.AllowedNodes,
			}, log,
		)
		toolRegistry.Register(tool.NewNodeInvokeTool(nodeMgr))
		toolRegistry.Register(tool.NewNodeListTool(nodeMgr))
		nodeMgr.StartHeartbeatChecker(ctx)
		log.Info("node system enabled",
			"heartbeat_interval", cfg.Nodes.HeartbeatInterval,
			"invoke_timeout", cfg.Nodes.InvokeTimeout,
		)

		// Register camera tool (requires nodes)
		if cfg.Tools.CameraEnabled {
			cameraCfg := tool.CameraConfig{
				MaxPayloadSize:  cfg.Tools.CameraMaxPayloadSize,
				MaxClipDuration: cfg.Tools.CameraMaxClipDuration,
				Timeout:         cfg.Tools.CameraTimeout,
			}
			cameraBackend := tool.NewNodeCameraBackend(nodeMgr, cfg.Tools.CameraMaxPayloadSize)
			toolRegistry.Register(tool.NewCameraTool(cameraBackend, cameraCfg, log))
			log.Info("camera tool enabled",
				"max_payload_size", cfg.Tools.CameraMaxPayloadSize,
				"max_clip_duration", cfg.Tools.CameraMaxClipDuration,
			)
		}

		// Register location tool (requires nodes)
		if cfg.Tools.LocationEnabled {
			locationCfg := tool.LocationConfig{
				Timeout:         cfg.Tools.LocationTimeout,
				DefaultAccuracy: cfg.Tools.LocationDefaultAccuracy,
			}
			locationBackend := tool.NewNodeLocationBackend(nodeMgr)
			toolRegistry.Register(tool.NewLocationTool(locationBackend, locationCfg, log))
			log.Info("location tool enabled",
				"timeout", cfg.Tools.LocationTimeout,
				"default_accuracy", cfg.Tools.LocationDefaultAccuracy,
			)
		}

		comp.NodeManager = nodeMgr
		comp.NodeAuth = nodeAuth
	}

	// 3. Init privacy manager
	dataFlow := domain.DataFlowInfo{
		Flows: []domain.DataFlow{
			{Source: "conversation", Destination: "local markdown files", Purpose: "long-term memory", Encrypted: security.Encryptor != nil},
			{Source: "conversation", Destination: "LLM provider", Purpose: "response generation", Encrypted: true},
			{Source: "audit events", Destination: cfg.Security.Audit.Path, Purpose: "audit trail", Encrypted: false},
		},
	}
	comp.PrivacyManager = usecase.NewPrivacyManager(cfg.Security.ConsentDir, mem, security.AuditLogger, dataFlow)

	// 4. Init curator (if auto-curate enabled)
	if cfg.Memory.AutoCurate {
		comp.Curator = usecase.NewCurator(mem, llmProvider, log)
		log.Info("auto-curate enabled")
	}

	return comp, nil
}
