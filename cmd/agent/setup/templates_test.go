package setup

import (
	"testing"
)

func TestGetTemplates(t *testing.T) {
	templates := GetTemplates()

	expectedCount := 8
	if len(templates) != expectedCount {
		t.Errorf("GetTemplates() returned %d templates, want %d", len(templates), expectedCount)
	}

	// Verify required templates exist
	expectedIDs := []string{
		"personal-assistant", "telegram-bot", "secure-private",
		"multi-agent", "voice-assistant", "workflow-automation", "developer-api",
		"custom",
	}
	foundIDs := make(map[string]bool)

	for _, tmpl := range templates {
		foundIDs[tmpl.ID] = true

		// Verify all fields are populated
		if tmpl.ID == "" {
			t.Error("Template has empty ID")
		}
		if tmpl.Name == "" {
			t.Errorf("Template %s has empty Name", tmpl.ID)
		}
		if tmpl.Description == "" {
			t.Errorf("Template %s has empty Description", tmpl.ID)
		}
		if tmpl.Difficulty == "" {
			t.Errorf("Template %s has empty Difficulty", tmpl.ID)
		}
		if tmpl.EstimateMin <= 0 {
			t.Errorf("Template %s has invalid EstimateMin: %d", tmpl.ID, tmpl.EstimateMin)
		}
		if tmpl.Config == nil {
			t.Errorf("Template %s has nil Config", tmpl.ID)
		}
	}

	// No duplicate IDs
	if len(foundIDs) != len(templates) {
		t.Error("Duplicate template IDs detected")
	}

	for _, expectedID := range expectedIDs {
		if !foundIDs[expectedID] {
			t.Errorf("Expected template ID %s not found", expectedID)
		}
	}
}

func TestCustomTemplateIsLast(t *testing.T) {
	templates := GetTemplates()
	last := templates[len(templates)-1]
	if last.ID != "custom" {
		t.Errorf("expected 'custom' as last template, got %q", last.ID)
	}
}

func TestPersonalAssistantTemplate(t *testing.T) {
	tmpl := personalAssistantTemplate()

	if tmpl.ID != "personal-assistant" {
		t.Errorf("ID = %s, want 'personal-assistant'", tmpl.ID)
	}
	if tmpl.Difficulty != "Beginner" {
		t.Errorf("Difficulty = %s, want 'Beginner'", tmpl.Difficulty)
	}
	if tmpl.Config == nil {
		t.Fatal("Config is nil")
	}
	if tmpl.Config.LLM.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %s, want 'openai'", tmpl.Config.LLM.DefaultProvider)
	}
	if tmpl.Config.Memory.Provider != "markdown" {
		t.Errorf("Memory.Provider = %s, want 'markdown'", tmpl.Config.Memory.Provider)
	}
	if len(tmpl.Config.Channels) != 1 || tmpl.Config.Channels[0].Type != "cli" {
		t.Error("Should have exactly one CLI channel")
	}
}

func TestTelegramBotTemplate(t *testing.T) {
	tmpl := telegramBotTemplate()

	if tmpl.ID != "telegram-bot" {
		t.Errorf("ID = %s, want 'telegram-bot'", tmpl.ID)
	}
	if tmpl.Difficulty != "Beginner" {
		t.Errorf("Difficulty = %s, want 'Beginner'", tmpl.Difficulty)
	}
	if tmpl.Config == nil {
		t.Fatal("Config is nil")
	}
	if !tmpl.Config.Memory.AutoCurate {
		t.Error("AutoCurate should be enabled for telegram bot")
	}
	if len(tmpl.Config.Channels) != 1 || tmpl.Config.Channels[0].Type != "telegram" {
		t.Error("Should have exactly one Telegram channel")
	}
}

func TestSecurePrivateTemplate(t *testing.T) {
	tmpl := securePrivateTemplate()

	if tmpl.ID != "secure-private" {
		t.Errorf("ID = %s, want 'secure-private'", tmpl.ID)
	}
	if tmpl.Difficulty != "Intermediate" {
		t.Errorf("Difficulty = %s, want 'Intermediate'", tmpl.Difficulty)
	}
	if tmpl.Config == nil {
		t.Fatal("Config is nil")
	}
	if !tmpl.Config.Security.Encryption.Enabled {
		t.Error("Encryption should be enabled")
	}
	if !tmpl.Config.Security.Audit.Enabled {
		t.Error("Audit logging should be enabled")
	}
	if tmpl.Config.Tools.SandboxRoot == "" {
		t.Error("SandboxRoot should be set")
	}
}

func TestMultiAgentTemplate(t *testing.T) {
	tmpl := multiAgentTemplate()

	if tmpl.ID != "multi-agent" {
		t.Errorf("ID = %s, want 'multi-agent'", tmpl.ID)
	}
	if tmpl.Difficulty != "Advanced" {
		t.Errorf("Difficulty = %s, want 'Advanced'", tmpl.Difficulty)
	}

	cfg := tmpl.Config
	if cfg == nil {
		t.Fatal("Config is nil")
	}
	if cfg.Agents == nil {
		t.Fatal("multi-agent template should have Agents config")
	}
	if len(cfg.Agents.Instances) < 2 {
		t.Errorf("expected at least 2 agent instances, got %d", len(cfg.Agents.Instances))
	}
	if cfg.Agents.Default == "" {
		t.Error("should have a default agent")
	}
	if !cfg.Gateway.Enabled {
		t.Error("should have Gateway enabled")
	}
	if !cfg.Memory.AutoCurate {
		t.Error("should have auto-curation enabled")
	}

	// Verify agent IDs are unique.
	agentIDs := make(map[string]bool)
	for _, inst := range cfg.Agents.Instances {
		if agentIDs[inst.ID] {
			t.Errorf("duplicate agent ID: %s", inst.ID)
		}
		agentIDs[inst.ID] = true

		if inst.Name == "" {
			t.Errorf("agent %s has empty Name", inst.ID)
		}
		if inst.SystemPrompt == "" {
			t.Errorf("agent %s has empty SystemPrompt", inst.ID)
		}
	}
}

func TestVoiceAssistantTemplate(t *testing.T) {
	tmpl := voiceAssistantTemplate()

	if tmpl.ID != "voice-assistant" {
		t.Errorf("ID = %s, want 'voice-assistant'", tmpl.ID)
	}
	if tmpl.Difficulty != "Intermediate" {
		t.Errorf("Difficulty = %s, want 'Intermediate'", tmpl.Difficulty)
	}

	cfg := tmpl.Config
	if cfg == nil {
		t.Fatal("Config is nil")
	}
	if !cfg.Tools.VoiceCall.Enabled {
		t.Error("VoiceCall should be enabled")
	}
	if cfg.Tools.VoiceCall.Provider != "twilio" {
		t.Errorf("expected twilio provider, got %q", cfg.Tools.VoiceCall.Provider)
	}
	if cfg.Tools.VoiceCall.DefaultMode != "conversation" {
		t.Errorf("expected conversation mode, got %q", cfg.Tools.VoiceCall.DefaultMode)
	}
	if cfg.Tools.VoiceCall.MaxConcurrent <= 0 {
		t.Error("MaxConcurrent should be positive")
	}
	if cfg.Tools.VoiceCall.MaxDuration <= 0 {
		t.Error("MaxDuration should be positive")
	}
	if cfg.Tools.VoiceCall.TTSVoice == "" {
		t.Error("TTSVoice should be set")
	}
	if cfg.Tools.VoiceCall.STTModel == "" {
		t.Error("STTModel should be set")
	}
}

func TestWorkflowAutomationTemplate(t *testing.T) {
	tmpl := workflowAutomationTemplate()

	if tmpl.ID != "workflow-automation" {
		t.Errorf("ID = %s, want 'workflow-automation'", tmpl.ID)
	}
	if tmpl.Difficulty != "Intermediate" {
		t.Errorf("Difficulty = %s, want 'Intermediate'", tmpl.Difficulty)
	}

	cfg := tmpl.Config
	if cfg == nil {
		t.Fatal("Config is nil")
	}
	if !cfg.Tools.WorkflowEnabled {
		t.Error("WorkflowEnabled should be true")
	}
	if !cfg.Tools.CronEnabled {
		t.Error("CronEnabled should be true")
	}
	if !cfg.Tools.ProcessEnabled {
		t.Error("ProcessEnabled should be true")
	}
	if !cfg.Gateway.Enabled {
		t.Error("Gateway should be enabled")
	}
	if cfg.Tools.WorkflowDir == "" {
		t.Error("WorkflowDir should be set")
	}
	if cfg.Tools.CronDataDir == "" {
		t.Error("CronDataDir should be set")
	}
}

func TestDeveloperAPITemplate(t *testing.T) {
	tmpl := developerAPITemplate()

	if tmpl.ID != "developer-api" {
		t.Errorf("ID = %s, want 'developer-api'", tmpl.ID)
	}
	if tmpl.Difficulty != "Advanced" {
		t.Errorf("Difficulty = %s, want 'Advanced'", tmpl.Difficulty)
	}

	cfg := tmpl.Config
	if cfg == nil {
		t.Fatal("Config is nil")
	}
	if !cfg.Gateway.Enabled {
		t.Error("Gateway should be enabled")
	}
	if !cfg.Tools.CanvasEnabled {
		t.Error("CanvasEnabled should be true")
	}
	if !cfg.Tools.CronEnabled {
		t.Error("CronEnabled should be true")
	}
	if !cfg.Tools.ProcessEnabled {
		t.Error("ProcessEnabled should be true")
	}
	if !cfg.Tools.WorkflowEnabled {
		t.Error("WorkflowEnabled should be true")
	}
	if !cfg.Tools.MessageEnabled {
		t.Error("MessageEnabled should be true")
	}
	if !cfg.Tools.LLMTaskEnabled {
		t.Error("LLMTaskEnabled should be true")
	}
	if !cfg.Memory.AutoCurate {
		t.Error("AutoCurate should be enabled")
	}
}

func TestAdvancedCustomTemplate(t *testing.T) {
	tmpl := advancedCustomTemplate()

	if tmpl.ID != "custom" {
		t.Errorf("ID = %s, want 'custom'", tmpl.ID)
	}
	if tmpl.Difficulty != "Advanced" {
		t.Errorf("Difficulty = %s, want 'Advanced'", tmpl.Difficulty)
	}
	if tmpl.Config == nil {
		t.Error("Config should not be nil")
	}
}

func TestGetTemplateByID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantNil bool
	}{
		{"personal-assistant", "personal-assistant", false},
		{"telegram-bot", "telegram-bot", false},
		{"secure-private", "secure-private", false},
		{"multi-agent", "multi-agent", false},
		{"voice-assistant", "voice-assistant", false},
		{"workflow-automation", "workflow-automation", false},
		{"developer-api", "developer-api", false},
		{"custom", "custom", false},
		{"nonexistent", "nonexistent", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetTemplateByID(tt.id)
			if tt.wantNil && got != nil {
				t.Errorf("GetTemplateByID(%s) should return nil", tt.id)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("GetTemplateByID(%s) should not return nil", tt.id)
			}
			if !tt.wantNil && got != nil && got.ID != tt.id {
				t.Errorf("GetTemplateByID(%s) returned template with ID %s", tt.id, got.ID)
			}
		})
	}
}

func TestTemplateConfigValidity(t *testing.T) {
	templates := GetTemplates()

	for _, tmpl := range templates {
		t.Run(tmpl.Name, func(t *testing.T) {
			cfg := tmpl.Config

			// All templates except custom should have LLM configured.
			if tmpl.ID != "custom" && len(cfg.LLM.Providers) == 0 {
				t.Error("Template should have at least one LLM provider")
			}

			// All templates except custom should have channels.
			if tmpl.ID != "custom" && len(cfg.Channels) == 0 {
				t.Error("Template should have at least one channel")
			}

			// Estimates should be reasonable.
			if tmpl.EstimateMin < 1 || tmpl.EstimateMin > 30 {
				t.Errorf("EstimateMin %d is outside reasonable range (1-30)", tmpl.EstimateMin)
			}
		})
	}
}
