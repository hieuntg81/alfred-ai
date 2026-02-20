package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
)

// DaemonConfig holds parameters for daemon installation.
type DaemonConfig struct {
	Name       string
	BinaryPath string
	ConfigPath string
	WorkDir    string
	User       string
	LogPath    string
	HomeDir    string
}

// DaemonStatus holds the status of an installed daemon.
type DaemonStatus struct {
	Running bool
	PID     int
	Uptime  string
}

// DefaultConfig returns a DaemonConfig with auto-detected defaults.
func DefaultConfig() DaemonConfig {
	name := "alfred-ai"
	binary, _ := os.Executable()
	if binary == "" {
		binary = "/usr/local/bin/alfred-ai"
	}

	u, _ := user.Current()
	username := "root"
	homeDir := "/root"
	if u != nil {
		username = u.Username
		homeDir = u.HomeDir
	}

	return DaemonConfig{
		Name:       name,
		BinaryPath: binary,
		ConfigPath: filepath.Join(homeDir, ".config", name, "config.yaml"),
		WorkDir:    filepath.Join(homeDir, ".local", "share", name),
		User:       username,
		LogPath:    filepath.Join(homeDir, ".local", "share", name, "logs"),
		HomeDir:    homeDir,
	}
}

// Validate checks the DaemonConfig for correctness.
func (c *DaemonConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("daemon name is required")
	}
	if c.BinaryPath == "" {
		return fmt.Errorf("binary path is required")
	}
	info, err := os.Stat(c.BinaryPath)
	if err != nil {
		return fmt.Errorf("binary %q: %w", c.BinaryPath, err)
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("binary %q is not executable", c.BinaryPath)
	}
	return nil
}

// Install installs the daemon on the current platform.
func Install(cfg DaemonConfig) error {
	switch runtime.GOOS {
	case "linux":
		return installSystemd(cfg)
	case "darwin":
		return installLaunchd(cfg)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Uninstall removes the daemon on the current platform.
func Uninstall(name string) error {
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd(name)
	case "darwin":
		return uninstallLaunchd(name)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Status returns the daemon status on the current platform.
func Status(name string) (*DaemonStatus, error) {
	switch runtime.GOOS {
	case "linux":
		return statusSystemd(name)
	case "darwin":
		return statusLaunchd(name)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// --- systemd ---

const systemdTemplate = `[Unit]
Description={{.Name}} AI assistant
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} --config {{.ConfigPath}}
WorkingDirectory={{.WorkDir}}
User={{.User}}
Restart=on-failure
RestartSec=5
StandardOutput=append:{{.LogPath}}/{{.Name}}.log
StandardError=append:{{.LogPath}}/{{.Name}}.log
Environment=HOME={{.HomeDir}}

[Install]
WantedBy=multi-user.target
`

// RenderSystemdUnit renders the systemd service file content.
func RenderSystemdUnit(cfg DaemonConfig) (string, error) {
	tmpl, err := template.New("systemd").Parse(systemdTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func installSystemd(cfg DaemonConfig) error {
	content, err := RenderSystemdUnit(cfg)
	if err != nil {
		return err
	}

	// Create log directory.
	if err := os.MkdirAll(cfg.LogPath, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Create work directory.
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	unitPath := filepath.Join("/etc/systemd/system", cfg.Name+".service")
	if err := os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", cfg.Name},
		{"systemctl", "start", cfg.Name},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s: %w", strings.Join(args, " "), out, err)
		}
	}

	return nil
}

func uninstallSystemd(name string) error {
	cmds := [][]string{
		{"systemctl", "stop", name},
		{"systemctl", "disable", name},
	}
	for _, args := range cmds {
		exec.Command(args[0], args[1:]...).Run() // best effort
	}

	unitPath := filepath.Join("/etc/systemd/system", name+".service")
	os.Remove(unitPath)
	exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

func statusSystemd(name string) (*DaemonStatus, error) {
	out, err := exec.Command("systemctl", "is-active", name).Output()
	running := strings.TrimSpace(string(out)) == "active"
	if err != nil && !running {
		return &DaemonStatus{Running: false}, nil
	}

	status := &DaemonStatus{Running: running}

	// Try to get PID.
	if pidOut, err := exec.Command("systemctl", "show", "--property=MainPID", name).Output(); err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(pidOut)), "=", 2)
		if len(parts) == 2 {
			status.PID, _ = strconv.Atoi(parts[1])
		}
	}

	return status, nil
}

// --- launchd ---

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.byterover.{{.Name}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>--config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>WorkingDirectory</key>
    <string>{{.WorkDir}}</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}/{{.Name}}.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}/{{.Name}}.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>{{.HomeDir}}</string>
    </dict>
</dict>
</plist>
`

// RenderLaunchdPlist renders the launchd plist content.
func RenderLaunchdPlist(cfg DaemonConfig) (string, error) {
	tmpl, err := template.New("launchd").Parse(launchdTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func installLaunchd(cfg DaemonConfig) error {
	content, err := RenderLaunchdPlist(cfg)
	if err != nil {
		return err
	}

	// Create log directory.
	if err := os.MkdirAll(cfg.LogPath, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Create work directory.
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	label := "com.byterover." + cfg.Name
	plistPath := filepath.Join(cfg.HomeDir, "Library", "LaunchAgents", label+".plist")

	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %s: %w", out, err)
	}

	return nil
}

func uninstallLaunchd(name string) error {
	home, _ := os.UserHomeDir()
	label := "com.byterover." + name
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")

	exec.Command("launchctl", "unload", plistPath).Run() // best effort
	os.Remove(plistPath)
	return nil
}

func statusLaunchd(name string) (*DaemonStatus, error) {
	label := "com.byterover." + name
	out, err := exec.Command("launchctl", "list", label).CombinedOutput()
	if err != nil {
		return &DaemonStatus{Running: false}, nil
	}

	status := &DaemonStatus{Running: true}
	// Parse PID from launchctl list output.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "PID") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				status.PID, _ = strconv.Atoi(parts[len(parts)-1])
			}
		}
	}

	return status, nil
}
