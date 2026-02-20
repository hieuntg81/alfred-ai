package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Agent.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", cfg.Agent.MaxIterations)
	}
	if cfg.LLM.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.LLM.DefaultProvider, "openai")
	}
	if cfg.Logger.Level != "info" {
		t.Errorf("Logger.Level = %q, want %q", cfg.Logger.Level, "info")
	}
}

func TestLoadNonExistentReturnsDefaults(t *testing.T) {
	cfg, err := Load("/tmp/nonexistent-config-12345.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.MaxIterations != 10 {
		t.Errorf("expected defaults, got MaxIterations=%d", cfg.Agent.MaxIterations)
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
agent:
  max_iterations: 20
  system_prompt: "test bot"
llm:
  default_provider: "groq"
  providers:
    - name: "groq"
      base_url: "https://api.groq.com/openai/v1"
      api_key: "test-key"
      model: "llama3-8b"
logger:
  level: "debug"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", cfg.Agent.MaxIterations)
	}
	if cfg.LLM.DefaultProvider != "groq" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.LLM.DefaultProvider, "groq")
	}
	if len(cfg.LLM.Providers) != 1 || cfg.LLM.Providers[0].APIKey != "test-key" {
		t.Errorf("Providers mismatch: %+v", cfg.LLM.Providers)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("ALFREDAI_LLM_DEFAULT_PROVIDER", "ollama")
	t.Setenv("ALFREDAI_LOGGER_LEVEL", "debug")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.LLM.DefaultProvider != "ollama" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.LLM.DefaultProvider, "ollama")
	}
	if cfg.Logger.Level != "debug" {
		t.Errorf("Logger.Level = %q, want %q", cfg.Logger.Level, "debug")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	passphrase := "test-passphrase-123"
	plaintext := "sk-abcdef123456"

	encrypted, err := EncryptValue(plaintext, passphrase)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}

	decrypted, err := DecryptValue(encrypted, passphrase)
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWrongPassphrase(t *testing.T) {
	encrypted, err := EncryptValue("secret", "correct-pass")
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptValue(encrypted, "wrong-pass")
	if err == nil {
		t.Error("expected error with wrong passphrase")
	}
}

func TestDecryptSecretsEnabled(t *testing.T) {
	passphrase := "test-config-key"
	plainAPIKey := "sk-secret123456"

	encrypted, err := EncryptValue(plainAPIKey, passphrase)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}

	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "enc:" + encrypted},
	}

	if err := decryptSecrets(cfg, passphrase); err != nil {
		t.Fatalf("decryptSecrets: %v", err)
	}

	if cfg.LLM.Providers[0].APIKey != plainAPIKey {
		t.Errorf("APIKey = %q, want %q", cfg.LLM.Providers[0].APIKey, plainAPIKey)
	}
}

func TestDecryptSecretsNoEncPrefix(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "sk-plain-key"},
	}

	if err := decryptSecrets(cfg, "any-passphrase"); err != nil {
		t.Fatalf("decryptSecrets: %v", err)
	}

	if cfg.LLM.Providers[0].APIKey != "sk-plain-key" {
		t.Errorf("APIKey should remain unchanged")
	}
}

func TestDecryptSecretsInvalidCiphertext(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "enc:notvalidhex"},
	}

	err := decryptSecrets(cfg, "passphrase")
	if err == nil {
		t.Error("expected error for invalid ciphertext")
	}
}

func TestApplyEnvOverridesTracerEnabled(t *testing.T) {
	t.Setenv("ALFREDAI_TRACER_ENABLED", "true")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if !cfg.Tracer.Enabled {
		t.Error("Tracer.Enabled should be true")
	}
}

func TestApplyEnvOverridesTracerExporter(t *testing.T) {
	t.Setenv("ALFREDAI_TRACER_EXPORTER", "stdout")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Tracer.Exporter != "stdout" {
		t.Errorf("Tracer.Exporter = %q, want %q", cfg.Tracer.Exporter, "stdout")
	}
}

func TestApplyEnvOverridesMemoryAutoCurate(t *testing.T) {
	t.Setenv("ALFREDAI_MEMORY_AUTO_CURATE", "true")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if !cfg.Memory.AutoCurate {
		t.Error("Memory.AutoCurate should be true")
	}
}

func TestApplyEnvOverridesAuditDisabled(t *testing.T) {
	t.Setenv("ALFREDAI_SECURITY_AUDIT_ENABLED", "false")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Security.Audit.Enabled {
		t.Error("Security.Audit.Enabled should be false")
	}
}

func TestApplyEnvOverridesAuditEnabled(t *testing.T) {
	t.Setenv("ALFREDAI_SECURITY_AUDIT_ENABLED", "true")

	cfg := Defaults()
	cfg.Security.Audit.Enabled = false
	ApplyEnvOverrides(cfg)

	if !cfg.Security.Audit.Enabled {
		t.Error("Security.Audit.Enabled should be true")
	}
}

func TestApplyEnvOverridesAuditPath(t *testing.T) {
	t.Setenv("ALFREDAI_SECURITY_AUDIT_PATH", "/custom/audit.jsonl")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Security.Audit.Path != "/custom/audit.jsonl" {
		t.Errorf("Audit.Path = %q", cfg.Security.Audit.Path)
	}
}

func TestApplyEnvOverridesSecurityEncryption(t *testing.T) {
	t.Setenv("ALFREDAI_SECURITY_ENCRYPTION_ENABLED", "true")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if !cfg.Security.Encryption.Enabled {
		t.Error("Security.Encryption.Enabled should be true")
	}
}

func TestApplyEnvOverridesConsentDir(t *testing.T) {
	t.Setenv("ALFREDAI_SECURITY_CONSENT_DIR", "/custom/consent")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Security.ConsentDir != "/custom/consent" {
		t.Errorf("ConsentDir = %q", cfg.Security.ConsentDir)
	}
}

func TestApplyEnvOverridesToolsSandboxRoot(t *testing.T) {
	t.Setenv("ALFREDAI_TOOLS_SANDBOX_ROOT", "/custom/sandbox")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Tools.SandboxRoot != "/custom/sandbox" {
		t.Errorf("Tools.SandboxRoot = %q", cfg.Tools.SandboxRoot)
	}
}

func TestApplyEnvOverridesMemoryProvider(t *testing.T) {
	t.Setenv("ALFREDAI_MEMORY_PROVIDER", "byterover")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Memory.Provider != "byterover" {
		t.Errorf("Memory.Provider = %q", cfg.Memory.Provider)
	}
}

func TestApplyEnvOverridesMemoryDataDir(t *testing.T) {
	t.Setenv("ALFREDAI_MEMORY_DATA_DIR", "/custom/data")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Memory.DataDir != "/custom/data" {
		t.Errorf("Memory.DataDir = %q", cfg.Memory.DataDir)
	}
}

func TestApplyEnvOverridesByteRover(t *testing.T) {
	t.Setenv("ALFREDAI_BYTEROVER_BASE_URL", "https://api.byterover.com")
	t.Setenv("ALFREDAI_BYTEROVER_API_KEY", "br-key")
	t.Setenv("ALFREDAI_BYTEROVER_PROJECT_ID", "proj-123")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Memory.ByteRover.BaseURL != "https://api.byterover.com" {
		t.Errorf("ByteRover.BaseURL = %q", cfg.Memory.ByteRover.BaseURL)
	}
	if cfg.Memory.ByteRover.APIKey != "br-key" {
		t.Errorf("ByteRover.APIKey = %q", cfg.Memory.ByteRover.APIKey)
	}
	if cfg.Memory.ByteRover.ProjectID != "proj-123" {
		t.Errorf("ByteRover.ProjectID = %q", cfg.Memory.ByteRover.ProjectID)
	}
}

func TestApplyEnvOverridesProviderAPIKey(t *testing.T) {
	t.Setenv("ALFREDAI_LLM_PROVIDER_OPENAI_API_KEY", "sk-env-override")

	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "sk-original"},
	}
	ApplyEnvOverrides(cfg)

	if cfg.LLM.Providers[0].APIKey != "sk-env-override" {
		t.Errorf("Provider APIKey = %q, want %q", cfg.LLM.Providers[0].APIKey, "sk-env-override")
	}
}

func TestApplyEnvOverridesTelegramToken(t *testing.T) {
	t.Setenv("ALFREDAI_TELEGRAM_TOKEN", "tg-token-123")

	cfg := Defaults()
	cfg.Channels = []ChannelConfig{
		{Type: "telegram", Telegram: &TelegramChannelConfig{}},
	}
	ApplyEnvOverrides(cfg)

	if cfg.Channels[0].Telegram.Token != "tg-token-123" {
		t.Errorf("Telegram.Token = %q, want %q", cfg.Channels[0].Telegram.Token, "tg-token-123")
	}
}

func TestApplyEnvOverridesTelegramTokenNilConfig(t *testing.T) {
	t.Setenv("ALFREDAI_TELEGRAM_TOKEN", "tg-token-123")

	cfg := Defaults()
	cfg.Channels = []ChannelConfig{
		{Type: "telegram"},
	}
	ApplyEnvOverrides(cfg)

	if cfg.Channels[0].Telegram == nil || cfg.Channels[0].Telegram.Token != "tg-token-123" {
		t.Errorf("expected Telegram config to be auto-initialized with token")
	}
}

func TestApplyEnvOverridesTelegramTokenSkipsNonEmpty(t *testing.T) {
	t.Setenv("ALFREDAI_TELEGRAM_TOKEN", "tg-token-new")

	cfg := Defaults()
	cfg.Channels = []ChannelConfig{
		{Type: "telegram", Telegram: &TelegramChannelConfig{Token: "existing-token"}},
	}
	ApplyEnvOverrides(cfg)

	// Should not override non-empty token
	if cfg.Channels[0].Telegram.Token != "existing-token" {
		t.Errorf("Telegram.Token = %q, should not override existing", cfg.Channels[0].Telegram.Token)
	}
}

func TestDecryptValueInvalidFormat(t *testing.T) {
	_, err := DecryptValue("nocolon", "passphrase")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestDecryptValueInvalidSalt(t *testing.T) {
	_, err := DecryptValue("notvalidhex:aabbcc", "passphrase")
	if err == nil {
		t.Error("expected error for invalid salt hex")
	}
}

func TestDecryptValueInvalidCiphertext(t *testing.T) {
	// Valid salt hex but invalid ciphertext hex
	_, err := DecryptValue("aabbccddee112233aabbccddee112233:notvalidhex", "passphrase")
	if err == nil {
		t.Error("expected error for invalid ciphertext hex")
	}
}

func TestDecryptValueTooShort(t *testing.T) {
	// Valid hex but too short for nonce+ciphertext
	_, err := DecryptValue("aabbccddee112233aabbccddee112233:aabb", "passphrase")
	if err == nil {
		t.Error("expected error for ciphertext too short")
	}
}

func TestLoadInsecurePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "insecure.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  max_iterations: 5\n"), 0666); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for insecure permissions")
	}
}

func TestLoadWithConfigKey(t *testing.T) {
	passphrase := "test-load-key"
	plainKey := "sk-loadtest"

	encrypted, err := EncryptValue(plainKey, passphrase)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
llm:
  providers:
    - name: "openai"
      api_key: "enc:` + encrypted + `"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ALFREDAI_CONFIG_KEY", passphrase)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.LLM.Providers[0].APIKey != plainKey {
		t.Errorf("APIKey = %q, want %q", cfg.LLM.Providers[0].APIKey, plainKey)
	}
}

func TestEncryptDecryptValueRoundTrip(t *testing.T) {
	passphrase := "test-pass"
	encrypted, err := EncryptValue("my-secret", passphrase)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}

	decrypted, err := DecryptValue(encrypted, passphrase)
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}
	if decrypted != "my-secret" {
		t.Errorf("decrypted = %q, want %q", decrypted, "my-secret")
	}
}

func TestDecryptSecretsWithEncryptedKey(t *testing.T) {
	passphrase := "config-pass"
	encAPIKey, err := EncryptValue("sk-real-key", passphrase)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "enc:" + encAPIKey},
	}

	err = decryptSecrets(cfg, passphrase)
	if err != nil {
		t.Fatalf("decryptSecrets: %v", err)
	}
	if cfg.LLM.Providers[0].APIKey != "sk-real-key" {
		t.Errorf("APIKey = %q, want %q", cfg.LLM.Providers[0].APIKey, "sk-real-key")
	}
}

func TestDecryptSecretsNonEncryptedKey(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.Providers = []ProviderConfig{
		{Name: "openai", APIKey: "sk-plain-key"},
	}

	err := decryptSecrets(cfg, "any-pass")
	if err != nil {
		t.Fatalf("decryptSecrets: %v", err)
	}
	if cfg.LLM.Providers[0].APIKey != "sk-plain-key" {
		t.Errorf("APIKey should remain unchanged")
	}
}

func TestApplyEnvOverridesSearchDecayHalfLife(t *testing.T) {
	t.Setenv("ALFREDAI_MEMORY_SEARCH_DECAY_HALF_LIFE", "48h")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Memory.Search.DecayHalfLife != 48*time.Hour {
		t.Errorf("Search.DecayHalfLife = %v, want 48h", cfg.Memory.Search.DecayHalfLife)
	}
}

func TestApplyEnvOverridesSearchMMRDiversity(t *testing.T) {
	t.Setenv("ALFREDAI_MEMORY_SEARCH_MMR_DIVERSITY", "0.3")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Memory.Search.MMRDiversity != 0.3 {
		t.Errorf("Search.MMRDiversity = %v, want 0.3", cfg.Memory.Search.MMRDiversity)
	}
}

func TestApplyEnvOverridesSearchEmbeddingCacheSize(t *testing.T) {
	t.Setenv("ALFREDAI_MEMORY_SEARCH_EMBEDDING_CACHE_SIZE", "512")

	cfg := Defaults()
	ApplyEnvOverrides(cfg)

	if cfg.Memory.Search.EmbeddingCacheSize != 512 {
		t.Errorf("Search.EmbeddingCacheSize = %d, want 512", cfg.Memory.Search.EmbeddingCacheSize)
	}
}

func TestApplyEnvOverridesSecurityAuditFalse(t *testing.T) {
	t.Setenv("ALFREDAI_SECURITY_AUDIT_ENABLED", "false")
	cfg := Defaults()
	cfg.Security.Audit.Enabled = true
	ApplyEnvOverrides(cfg)
	if cfg.Security.Audit.Enabled {
		t.Error("Audit.Enabled should be false")
	}
}

func TestValidatePermissionsOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte("test"), 0600)
	if err := validatePermissions(path); err != nil {
		t.Errorf("validatePermissions: %v", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("invalid: [yaml: bad"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestValidatePermissions(t *testing.T) {
	dir := t.TempDir()

	// 0600 should pass
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validatePermissions(good); err != nil {
		t.Errorf("0600 should pass: %v", err)
	}

	// 0644 should pass
	readable := filepath.Join(dir, "readable.yaml")
	if err := os.WriteFile(readable, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validatePermissions(readable); err != nil {
		t.Errorf("0644 should pass: %v", err)
	}

	// 0666 should fail (world-writable)
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("test"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := validatePermissions(bad); err == nil {
		t.Error("0666 should fail")
	}
}

func TestValidatePermissionsStatError(t *testing.T) {
	// Call validatePermissions on a non-existent file to trigger the os.Stat error path.
	err := validatePermissions("/tmp/nonexistent-file-for-stat-test-xyz.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadReadError(t *testing.T) {
	// Create a file that exists but cannot be read (no read permissions).
	// This triggers the "read config" error path (not IsNotExist).
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  max_iterations: 5\n"), 0000); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for unreadable file")
	}
}

func TestLoadDecryptSecretsError(t *testing.T) {
	// Create a config with an encrypted key that uses an invalid format,
	// then set ALFREDAI_CONFIG_KEY to trigger decryptSecrets with a failing decrypt.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
llm:
  providers:
    - name: "openai"
      api_key: "enc:invalid-not-hex"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ALFREDAI_CONFIG_KEY", "some-passphrase")
	_, err := Load(path)
	if err == nil {
		t.Error("expected error from decrypt secrets")
	}
}
