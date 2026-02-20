package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSystemdTemplateRender(t *testing.T) {
	cfg := DaemonConfig{
		Name:       "alfred-ai",
		BinaryPath: "/usr/local/bin/alfred-ai",
		ConfigPath: "/etc/alfred-ai/config.yaml",
		WorkDir:    "/var/lib/alfred-ai",
		User:       "alfredai",
		LogPath:    "/var/log/alfred-ai",
		HomeDir:    "/home/alfredai",
	}

	content, err := RenderSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}

	checks := []string{
		"[Unit]",
		"Description=alfred-ai",
		"ExecStart=/usr/local/bin/alfred-ai --config /etc/alfred-ai/config.yaml",
		"WorkingDirectory=/var/lib/alfred-ai",
		"User=alfredai",
		"StandardOutput=append:/var/log/alfred-ai/alfred-ai.log",
		"Environment=HOME=/home/alfredai",
		"[Install]",
		"WantedBy=multi-user.target",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("systemd unit missing %q:\n%s", check, content)
		}
	}
}

func TestLaunchdTemplateRender(t *testing.T) {
	cfg := DaemonConfig{
		Name:       "alfred-ai",
		BinaryPath: "/usr/local/bin/alfred-ai",
		ConfigPath: "/Users/test/.config/alfred-ai/config.yaml",
		WorkDir:    "/Users/test/.local/share/alfred-ai",
		LogPath:    "/Users/test/.local/share/alfred-ai/logs",
		HomeDir:    "/Users/test",
	}

	content, err := RenderLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("RenderLaunchdPlist: %v", err)
	}

	checks := []string{
		"com.byterover.alfred-ai",
		"/usr/local/bin/alfred-ai",
		"--config",
		"/Users/test/.config/alfred-ai/config.yaml",
		"RunAtLoad",
		"KeepAlive",
		"alfred-ai.log",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("launchd plist missing %q:\n%s", check, content)
		}
	}
}

func TestDaemonConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Name != "alfred-ai" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if cfg.BinaryPath == "" {
		t.Error("BinaryPath should not be empty")
	}
	if cfg.User == "" {
		t.Error("User should not be empty")
	}
	if cfg.HomeDir == "" {
		t.Error("HomeDir should not be empty")
	}
}

func TestInstallUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		t.Skip("skipping on supported platform")
	}
	err := Install(DefaultConfig())
	if err == nil {
		t.Fatal("expected unsupported platform error")
	}
	if !strings.Contains(err.Error(), "unsupported platform") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDaemonConfigValidation(t *testing.T) {
	// Empty name.
	cfg := DaemonConfig{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty name")
	}

	// Empty binary path.
	cfg = DaemonConfig{Name: "test"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty binary path")
	}

	// Non-existent binary.
	cfg = DaemonConfig{Name: "test", BinaryPath: "/nonexistent/binary"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for non-existent binary")
	}

	// Valid binary (use this test binary itself).
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine executable: %v", err)
	}
	cfg = DaemonConfig{Name: "test", BinaryPath: exe}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestDaemonConfigValidateNotExecutable(t *testing.T) {
	dir := t.TempDir()
	notExec := filepath.Join(dir, "notexec")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DaemonConfig{Name: "test", BinaryPath: notExec}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for non-executable binary")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Errorf("unexpected error: %v", err)
	}
}
