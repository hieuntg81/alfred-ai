package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"alfred-ai/internal/adapter/gateway"
	"alfred-ai/internal/adapter/llm"
	"alfred-ai/internal/adapter/tenant"
	"alfred-ai/internal/adapter/tool"
	"alfred-ai/internal/adapter/tui/chat"
	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/security"
	"alfred-ai/internal/usecase"
	"alfred-ai/internal/usecase/cluster"
	"alfred-ai/internal/usecase/cronjob"
	"alfred-ai/internal/usecase/multiagent"
	"alfred-ai/internal/usecase/scheduling"
	"alfred-ai/internal/usecase/workflow"
)

// redisAdapter wraps a go-redis client to implement cluster.RedisClient.
type redisAdapter struct {
	client *goredis.Client
}

func (r *redisAdapter) SetNX(ctx context.Context, key string, value string, expiration time.Duration) (bool, error) {
	return r.client.SetNX(ctx, key, value, expiration).Result()
}

func (r *redisAdapter) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

func (r *redisAdapter) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *redisAdapter) Publish(ctx context.Context, channel string, message string) error {
	return r.client.Publish(ctx, channel, message).Err()
}

func (r *redisAdapter) Subscribe(ctx context.Context, channel string) (<-chan string, error) {
	sub := r.client.Subscribe(ctx, channel)
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		msgCh := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				ch <- msg.Payload
			}
		}
	}()
	return ch, nil
}

func (r *redisAdapter) Close() error {
	return r.client.Close()
}

// RuntimeComponents holds runtime components (channels, router, scheduler, gateway, cron, tenants, cluster)
type RuntimeComponents struct {
	Router        *usecase.Router
	Channels      []domain.Channel
	Scheduler     *scheduling.Scheduler
	Gateway       *gateway.Server
	CronManager   *cronjob.Manager
	TenantManager *usecase.TenantManager           // nil in single-tenant mode
	Cluster       *cluster.ClusterCoordinator       // nil in standalone mode
}

// initRuntime initializes runtime components (router, channels, scheduler, gateway)
// Returns components, cleanup function, and any error
func initRuntime(
	ctx context.Context,
	cfg *config.Config,
	llmRegistry *llm.Registry,
	llmProvider domain.LLMProvider,
	agentComp *AgentComponents,
	features *FeatureComponents,
	sec *SecurityComponents,
	mem domain.MemoryProvider,
	bus domain.EventBus,
	log *slog.Logger,
) (*RuntimeComponents, func(context.Context) error, error) {
	comp := &RuntimeComponents{}

	// 1. Init router (single-agent or multi-agent)
	var registry *multiagent.Registry
	if cfg.Agents != nil && len(cfg.Agents.Instances) > 0 {
		comp.Router, registry = initMultiAgent(cfg, llmRegistry, agentComp.ToolRegistry,
			mem, bus, nil, agentComp.Compressor, agentComp.Approver, log)
	} else {
		comp.Router = usecase.NewRouter(agentComp.Agent, agentComp.SessionManager, bus, log)
	}

	// Set curator if available
	if features.Curator != nil {
		comp.Router.SetCurator(features.Curator)
	}

	// Set secret scanner if available
	if sec.SecretScanner != nil {
		comp.Router.SetScanner(&scannerAdapter{inner: sec.SecretScanner})
	}

	// Set service-layer RBAC if available
	if sec.Authorizer != nil {
		comp.Router.SetAuthorizer(sec.Authorizer)
	}

	// Set offline manager if configured
	if cfg.Offline != nil && cfg.Offline.Enabled {
		// Resolve a local LLM provider (Ollama) for offline mode.
		localLLM, err := llmRegistry.Get("ollama")
		if err != nil {
			log.Warn("offline mode requires a local LLM provider (ollama), skipping",
				"error", err)
		} else {
			checkURL := cfg.Offline.CheckURL
			if checkURL == "" {
				checkURL = "https://1.1.1.1"
			}
			checkPeriod := 30 * time.Second
			if cfg.Offline.CheckPeriod != "" {
				if d, err := time.ParseDuration(cfg.Offline.CheckPeriod); err == nil {
					checkPeriod = d
				}
			}
			queueDir := cfg.Offline.QueueDir
			if queueDir == "" {
				queueDir = "data/offline-queue"
			}
			offlineMgr := usecase.NewOfflineManager(localLLM, queueDir, checkURL, checkPeriod, log)
			offlineMgr.StartMonitor(ctx)
			comp.Router.SetOffline(offlineMgr)
			log.Info("offline mode enabled", "check_url", checkURL, "check_period", checkPeriod)
		}
	}

	// Start key rotator in background if configured
	if sec.KeyRotator != nil {
		go sec.KeyRotator.Start(ctx)
	}

	// 2. Build channels
	channels, cliCh, err := buildChannels(cfg, log, features.PrivacyManager)
	if err != nil {
		return nil, nil, fmt.Errorf("channels: %w", err)
	}
	comp.Channels = channels

	// Wire /clear command to actually delete the CLI session
	if cliCh != nil {
		cliCh.SetOnClear(func() {
			agentComp.SessionManager.Delete("cli:" + chat.DefaultSessionID)
		})
		cliCh.SetEventBus(bus)
	}

	// 3. Init scheduler (if enabled)
	if cfg.Scheduler.Enabled {
		scheduler := scheduling.NewScheduler(log)

		// Register standard actions
		scheduler.RegisterAction(scheduling.ActionMemorySync, func(ctx context.Context) error {
			return mem.Sync(ctx)
		})
		if features.Curator != nil {
			scheduler.RegisterAction(scheduling.ActionMemoryCurate, func(ctx context.Context) error {
				return nil
			})
		}
		scheduler.RegisterAction(scheduling.ActionSessionReap, func(ctx context.Context) error {
			reaped := agentComp.SessionManager.ReapStaleSessions(24 * time.Hour)
			if reaped > 0 {
				log.Info("reaped stale sessions", "count", reaped)
			}
			return nil
		})
		if sec.FileAuditLogger != nil {
			scheduler.RegisterAction(scheduling.ActionAuditRetention, func(ctx context.Context) error {
				removed, err := sec.FileAuditLogger.EnforceRetention(ctx)
				if err != nil {
					return err
				}
				if removed > 0 {
					log.Info("audit retention enforced", "removed", removed)
				}
				return nil
			})

			// Schedule daily audit retention automatically.
			if err := scheduler.AddTask(scheduling.ScheduledTask{
				Name:     "audit_retention",
				Schedule: "0 3 * * *", // 3 AM daily
				Action:   scheduling.ActionAuditRetention,
			}); err != nil {
				log.Warn("scheduler: failed to add audit retention task", "error", err)
			}
		}

		// Add configured tasks
		for _, tc := range cfg.Scheduler.Tasks {
			if err := scheduler.AddTask(scheduling.ScheduledTask{
				Name:     tc.Name,
				Schedule: tc.Schedule,
				Action:   scheduling.ScheduledAction(tc.Action),
				AgentID:  tc.AgentID,
				Channel:  tc.Channel,
				Message:  tc.Message,
				OneShot:  tc.OneShot,
			}); err != nil {
				log.Warn("scheduler: failed to add task", "name", tc.Name, "error", err)
			}
		}
		log.Info("scheduler enabled", "tasks", len(cfg.Scheduler.Tasks))
		comp.Scheduler = scheduler
	}

	// 3b. Init cron tool (if enabled)
	if cfg.Tools.CronEnabled {
		// Ensure scheduler exists for cron tool.
		if comp.Scheduler == nil {
			comp.Scheduler = scheduling.NewScheduler(log)
		}

		cronStore, err := cronjob.NewFileStore(cfg.Tools.CronDataDir)
		if err != nil {
			return nil, nil, fmt.Errorf("cron store: %w", err)
		}

		cronMgr := cronjob.NewManager(cronStore, comp.Scheduler, bus, log)
		cronMgr.SetHandler(comp.Router)

		if err := cronMgr.LoadAndSchedule(ctx); err != nil {
			log.Warn("failed to load persisted cron jobs", "error", err)
		}

		agentComp.ToolRegistry.Register(tool.NewCronTool(cronMgr, log))
		comp.CronManager = cronMgr
		log.Info("cron tool enabled", "data_dir", cfg.Tools.CronDataDir)
	}

	// 3c. Init message tool (if enabled)
	if cfg.Tools.MessageEnabled {
		if len(comp.Channels) == 0 {
			log.Warn("message tool enabled but no channels configured")
		}
		channelReg := tool.NewChannelRegistry(comp.Channels, log)
		agentComp.ToolRegistry.Register(tool.NewMessageTool(channelReg, log))
		log.Info("message tool enabled", "channels", len(comp.Channels))
	}

	// 3d. Init workflow tool (if enabled)
	if cfg.Tools.WorkflowEnabled {
		workflowStore, err := workflow.NewFileStore(cfg.Tools.WorkflowDataDir)
		if err != nil {
			return nil, nil, fmt.Errorf("workflow store: %w", err)
		}

		workflowMgr := workflow.NewManager(
			workflowStore,
			workflow.ManagerConfig{
				PipelineDir:             cfg.Tools.WorkflowDir,
				Timeout:                 cfg.Tools.WorkflowTimeout,
				MaxOutput:               cfg.Tools.WorkflowMaxOutput,
				MaxRunning:              cfg.Tools.WorkflowMaxRunning,
				AllowedCommands:         cfg.Tools.AllowedCommands,
				WorkflowAllowedCommands: cfg.Tools.WorkflowAllowedCommands,
			},
			sec.Sandbox,
			createShellBackend(cfg),
			&http.Client{
				Transport: security.NewSSRFSafeTransport(),
				Timeout:   cfg.Tools.WorkflowTimeout,
			},
			bus,
			log,
			agentComp.ToolRegistry,
		)

		if err := workflowMgr.LoadPipelines(); err != nil {
			log.Warn("failed to load workflow pipelines", "error", err)
		}

		agentComp.ToolRegistry.Register(tool.NewWorkflowTool(workflowMgr, log))
		log.Info("workflow tool enabled", "pipeline_dir", cfg.Tools.WorkflowDir)
	}

	// 3f. Init voice call tool (if enabled)
	var voiceCallTool *tool.VoiceCallTool
	if cfg.Tools.VoiceCall.Enabled {
		vc := cfg.Tools.VoiceCall

		// 1. Create JSONL persistence store.
		callFileStore, err := tool.NewFileCallStore(vc.DataDir)
		if err != nil {
			return nil, nil, fmt.Errorf("voice call store: %w", err)
		}

		// 2. Create in-memory call store (loads from JSONL).
		callStore := tool.NewCallStore(vc.MaxConcurrent, callFileStore)

		// 3. Create telephony backend.
		var voiceBackend tool.VoiceCallBackend
		switch vc.Provider {
		case "twilio":
			voiceBackend = tool.NewTwilioBackend(tool.TwilioBackendConfig{
				AccountSID: vc.TwilioAccountSID,
				AuthToken:  vc.TwilioAuthToken,
				FromNumber: vc.FromNumber,
			}, log)
		case "mock":
			voiceBackend = tool.NewMockVoiceCallBackend()
		default:
			return nil, nil, fmt.Errorf("unknown voice call provider: %s", vc.Provider)
		}

		// 4. Resolve OpenAI API key (prefer LLM registry, fallback to dedicated config).
		openaiKey := vc.OpenAIAPIKey
		if openaiKey == "" {
			for _, p := range cfg.LLM.Providers {
				if p.Type == "openai" || p.Name == "openai" {
					openaiKey = p.APIKey
					break
				}
			}
		}

		// 5. Create TTS/STT providers.
		ttsProvider := tool.NewOpenAITTSProvider(tool.OpenAITTSConfig{
			APIKey: openaiKey,
			Model:  vc.TTSModel,
			Voice:  vc.TTSVoice,
		}, log)

		sttProvider := tool.NewOpenAISTTProvider(tool.OpenAISTTConfig{
			APIKey:            openaiKey,
			Model:             vc.STTModel,
			SilenceDurationMs: vc.SilenceDurationMs,
		}, log)

		// 6. Create webhook server.
		webhookServer := tool.NewVoiceCallWebhookServer(
			tool.VoiceCallWebhookConfig{
				Addr:        vc.WebhookAddr,
				WebhookPath: vc.WebhookPath,
				StreamPath:  vc.StreamPath,
				PublicURL:   vc.WebhookPublicURL,
				SkipVerify:  vc.WebhookSkipVerify,
			},
			voiceBackend, callStore, sttProvider, ttsProvider, log,
		)
		if err := webhookServer.Start(ctx); err != nil {
			return nil, nil, fmt.Errorf("voice call webhook server: %w", err)
		}

		// 7. Register tool.
		voiceCallTool = tool.NewVoiceCallTool(voiceBackend, callStore, tool.VoiceCallToolConfig{
			FromNumber:        vc.FromNumber,
			DefaultTo:         vc.DefaultTo,
			DefaultMode:       vc.DefaultMode,
			MaxConcurrent:     vc.MaxConcurrent,
			MaxDuration:       vc.MaxDuration,
			TranscriptTimeout: vc.TranscriptTimeout,
			Timeout:           vc.Timeout,
			AllowedNumbers:    vc.AllowedNumbers,
			WebhookPublicURL:  vc.WebhookPublicURL,
			WebhookPath:       vc.WebhookPath,
			StreamPath:        vc.StreamPath,
		}, log)
		agentComp.ToolRegistry.Register(voiceCallTool)

		if vc.WebhookSkipVerify {
			log.Warn("voice call webhook signature verification disabled (dev-only)")
		}
		log.Info("voice call tool enabled",
			"provider", vc.Provider,
			"from", vc.FromNumber,
			"max_concurrent", vc.MaxConcurrent,
			"webhook_addr", vc.WebhookAddr,
		)
	}

	// 3e. Init llm_task tool (if enabled)
	if cfg.Tools.LLMTaskEnabled {
		llmTaskCfg := tool.LLMTaskConfig{
			AllowedModels: cfg.Tools.LLMTaskAllowedModels,
			DefaultModel:  cfg.Tools.LLMTaskDefaultModel,
			MaxTokens:     cfg.Tools.LLMTaskMaxTokens,
			Timeout:       cfg.Tools.LLMTaskTimeout,
			MaxPromptSize: cfg.Tools.LLMTaskMaxPromptSize,
			MaxInputSize:  cfg.Tools.LLMTaskMaxInputSize,
		}
		if llmTaskCfg.DefaultModel == "" {
			for _, p := range cfg.LLM.Providers {
				if p.Name == cfg.LLM.DefaultProvider {
					llmTaskCfg.DefaultModel = p.Model
					break
				}
			}
		}
		agentComp.ToolRegistry.Register(tool.NewLLMTaskTool(
			llmProvider, llmRegistry, llmTaskCfg, log,
		))
		log.Info("llm_task tool enabled",
			"timeout", cfg.Tools.LLMTaskTimeout,
			"max_tokens", cfg.Tools.LLMTaskMaxTokens,
		)
	}

	// 4. Init tenant manager (if enabled)
	if cfg.Tenants != nil && cfg.Tenants.Enabled {
		tenantStore, err := tenant.NewSQLiteTenantStore(
			cfg.Tenants.DataDir + "/tenants.db",
		)
		if err != nil {
			return nil, nil, fmt.Errorf("init tenant store: %w", err)
		}
		comp.TenantManager = usecase.NewTenantManager(tenantStore, cfg.Tenants.DataDir, log)
		if sec.Authorizer != nil {
			comp.TenantManager.SetAuthorizer(sec.Authorizer)
		}
		log.Info("multi-tenant enabled", "data_dir", cfg.Tenants.DataDir)
	}

	// 4b. Init cluster coordinator (if enabled)
	if cfg.Cluster != nil && cfg.Cluster.Enabled {
		nodeID := cfg.Cluster.NodeID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node-%d", time.Now().UnixNano()%100000)
		}
		lockTTL := 30 * time.Second
		if cfg.Cluster.LockTTL != "" {
			if d, err := time.ParseDuration(cfg.Cluster.LockTTL); err == nil {
				lockTTL = d
			}
		}

		redisOpts, err := goredis.ParseURL(cfg.Cluster.RedisURL)
		if err != nil {
			return nil, nil, fmt.Errorf("parse cluster redis URL: %w", err)
		}
		rdb := goredis.NewClient(redisOpts)
		if err := rdb.Ping(ctx).Err(); err != nil {
			rdb.Close()
			return nil, nil, fmt.Errorf("cluster redis ping: %w", err)
		}

		comp.Cluster = cluster.NewClusterCoordinator(
			&redisAdapter{client: rdb},
			cluster.CoordinatorConfig{NodeID: nodeID, LockTTL: lockTTL},
			log,
		)
		log.Info("cluster mode enabled",
			"node_id", nodeID, "redis_url", cfg.Cluster.RedisURL)
	}

	// 4c. Create GDPR handler (if audit + memory are available)
	if sec.AuditLogger != nil && mem != nil {
		sec.GDPRHandler = security.NewGDPRHandler(mem, sec.AuditLogger)
		log.Info("GDPR handler enabled")
	}

	// 5. Init gateway (if enabled)
	if cfg.Gateway.Enabled {
		var entries []struct {
			Token, Name string
			Roles       []string
		}
		for _, t := range cfg.Gateway.Auth.Tokens {
			entries = append(entries, struct {
				Token, Name string
				Roles       []string
			}{Token: t.Token, Name: t.Name, Roles: t.Roles})
		}
		auth := gateway.NewStaticTokenAuth(entries)
		gwServer := gateway.NewServer(bus, auth, cfg.Gateway.Addr, log)
		gwDeps := gateway.HandlerDeps{
			Router:         comp.Router,
			Sessions:       agentComp.SessionManager,
			Tools:          agentComp.ToolRegistry,
			Memory:         mem,
			Bus:            bus,
			Logger:         log,
			Registry:       registry,
			ActiveRequests: &sync.Map{},
			Authorizer:     sec.Authorizer,
			AuditLogger:    sec.AuditLogger,
		}
		if features.NodeManager != nil {
			gwDeps.NodeManager = features.NodeManager
		}
		if features.NodeAuth != nil {
			gwDeps.NodeTokens = features.NodeAuth
		}
		if comp.CronManager != nil {
			gwDeps.CronManager = comp.CronManager
		}
		if agentComp.ProcessManager != nil {
			gwDeps.ProcessManager = agentComp.ProcessManager
		}
		if comp.TenantManager != nil {
			gwDeps.TenantManager = comp.TenantManager
		}
		if sec.GDPRHandler != nil {
			gwDeps.GDPRHandler = sec.GDPRHandler
		}
		gateway.RegisterDefaultHandlers(gwServer, gwDeps)

		// Register REST endpoints (status + metrics).
		channelNames := make([]string, len(comp.Channels))
		for i, ch := range comp.Channels {
			channelNames[i] = ch.Name()
		}
		gateway.RegisterRESTHandlers(gwServer, gwDeps, channelNames)

		comp.Gateway = gwServer
		log.Info("gateway enabled", "addr", cfg.Gateway.Addr)
	}

	// Cleanup function
	cleanup := func(ctx context.Context) error {
		// Stop voice calls FIRST (hangup active calls, persist state).
		if voiceCallTool != nil {
			voiceCallTool.HangupActiveCalls(ctx)
		}

		// Stop gateway to drain in-flight requests before stopping process manager.
		if comp.Gateway != nil {
			comp.Gateway.Stop(ctx)
		}

		// Then stop process manager (kill all running processes).
		if agentComp.ProcessManager != nil {
			agentComp.ProcessManager.Stop(ctx)
		}

		// Stop scheduler
		if comp.Scheduler != nil {
			comp.Scheduler.Stop()
		}

		// Stop cluster coordinator
		if comp.Cluster != nil {
			if err := comp.Cluster.Stop(); err != nil {
				log.Warn("cluster coordinator stop error", "error", err)
			}
		}

		// Wait for router
		comp.Router.Wait()

		// Stop channels
		for _, ch := range comp.Channels {
			if err := ch.Stop(ctx); err != nil {
				log.Warn("channel stop error", "channel", ch.Name(), "error", err)
			}
		}

		return nil
	}

	return comp, cleanup, nil
}
