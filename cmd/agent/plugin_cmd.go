package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/config"
	"alfred-ai/internal/plugin"
	"alfred-ai/internal/plugin/wasm"
)

func runPlugin() error {
	if len(os.Args) < 3 {
		printPluginUsage()
		return nil
	}

	switch os.Args[2] {
	case "list":
		return runPluginList()
	case "validate":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin validate <path>")
		}
		return runPluginValidate(os.Args[3])
	case "init":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin init <name>")
		}
		return runPluginInit(os.Args[3])
	case "search":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin search <query>")
		}
		return runPluginSearch(strings.Join(os.Args[3:], " "))
	case "install":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin install <name>")
		}
		return runPluginInstall(os.Args[3])
	case "update":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin update <name>")
		}
		return runPluginUpdate(os.Args[3])
	case "remove":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin remove <name>")
		}
		return runPluginRemove(os.Args[3])
	case "publish":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: alfred-ai plugin publish <path>")
		}
		return runPluginPublish(os.Args[3])
	default:
		return fmt.Errorf("unknown plugin subcommand: %s\n\nRun 'alfred-ai plugin' for usage", os.Args[2])
	}
}

func printPluginUsage() {
	fmt.Println(`alfred-ai plugin - Plugin management tools

USAGE:
    alfred-ai plugin <COMMAND>

COMMANDS:
    list               List locally installed plugins
    validate <path>    Validate a plugin at the given path
    init <name>        Scaffold a new WASM plugin project
    search <query>     Search the plugin registry
    install <name>     Install a plugin from the registry
    update <name>      Update an installed plugin
    remove <name>      Remove an installed plugin
    publish <path>     Validate and prepare a plugin for registry submission`)
}

func runPluginList() error {
	cfgPath := configPath()
	cfg, err := loadConfigOrDefault(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	dirs := cfg.Plugins.Dirs
	if len(dirs) == 0 {
		dirs = []string{"./plugins"}
	}

	manifests, err := plugin.ScanDirectories(dirs)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	if len(manifests) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tTYPES\tWASM\tPERMISSIONS")
	for _, m := range manifests {
		types := make([]string, len(m.Types))
		for i, t := range m.Types {
			types[i] = string(t)
		}
		isWASM := "no"
		if m.WASMConfig != nil && m.WASMConfig.Binary != "" {
			isWASM = "yes"
		}
		perms := "-"
		if len(m.Permissions) > 0 {
			perms = strings.Join(m.Permissions, ", ")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			m.Name, m.Version, strings.Join(types, ","), isWASM, perms)
	}
	return w.Flush()
}

func runPluginValidate(path string) error {
	manifestPath := filepath.Join(path, "plugin.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var m domain.PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		fmt.Printf("FAIL: malformed plugin.yaml: %v\n", err)
		return fmt.Errorf("validation failed")
	}

	var issues []string

	if m.Name == "" {
		issues = append(issues, "name is required")
	}
	if m.Version == "" {
		issues = append(issues, "version is required")
	}
	if len(m.Types) == 0 {
		issues = append(issues, "at least one type is required")
	}

	// WASM-specific validation.
	if m.WASMConfig != nil && m.WASMConfig.Binary != "" {
		wasmPath := filepath.Join(path, m.WASMConfig.Binary)
		if _, err := os.Stat(wasmPath); err != nil {
			issues = append(issues, fmt.Sprintf("wasm binary not found: %s", wasmPath))
		} else {
			// Attempt to compile the WASM binary to validate format.
			ctx := context.Background()
			rt, rtErr := wasm.NewRuntime(ctx, wasm.DefaultRuntimeConfig(), slogDiscard())
			if rtErr != nil {
				issues = append(issues, fmt.Sprintf("wasm runtime init failed: %v", rtErr))
			} else {
				wasmBytes, readErr := os.ReadFile(wasmPath)
				if readErr != nil {
					issues = append(issues, fmt.Sprintf("read wasm binary: %v", readErr))
				} else {
					if _, compileErr := rt.Inner().CompileModule(ctx, wasmBytes); compileErr != nil {
						issues = append(issues, fmt.Sprintf("wasm compile failed: %v", compileErr))
					} else {
						fmt.Println("PASS: WASM binary compiles successfully")
					}
				}
				_ = rt.Close(ctx)
			}
		}

		// Validate capabilities.
		if err := wasm.ValidateCapabilities(m.WASMConfig.Capabilities); err != nil {
			issues = append(issues, err.Error())
		} else if len(m.WASMConfig.Capabilities) > 0 {
			fmt.Printf("PASS: capabilities valid: %v\n", m.WASMConfig.Capabilities)
		}
	}

	if len(issues) > 0 {
		fmt.Println("Validation results:")
		for _, issue := range issues {
			fmt.Printf("  FAIL: %s\n", issue)
		}
		return fmt.Errorf("validation failed with %d issues", len(issues))
	}

	fmt.Printf("PASS: plugin %q v%s is valid\n", m.Name, m.Version)
	return nil
}

func runPluginInit(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\. ") {
		return fmt.Errorf("invalid plugin name %q: must be a simple identifier", name)
	}

	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	if err := os.MkdirAll(name, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	files := map[string]string{
		"plugin.yaml": pluginYAMLTemplate(name),
		"main.go":     pluginMainGoTemplate(name),
		"Makefile":    pluginMakefileTemplate(),
		"README.md":   pluginReadmeTemplate(name),
	}

	for filename, content := range files {
		p := filepath.Join(name, filename)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
	}

	fmt.Printf("Plugin %q scaffolded successfully!\n\n", name)
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", name)
	fmt.Println("  # Edit main.go with your plugin logic")
	fmt.Println("  make build")
	fmt.Println("  alfred-ai plugin validate .")
	return nil
}

func pluginYAMLTemplate(name string) string {
	return `name: ` + name + `
version: "0.1.0"
description: "A WASM plugin for alfred-ai"
author: ""
types:
  - tool
  - wasm
permissions: []
wasm:
  binary: plugin.wasm
  max_memory_mb: 64
  exec_timeout: 30s
  capabilities:
    - log
    - config
    - tool
`
}

func pluginMainGoTemplate(name string) string {
	return `//go:build tinygo

package main

import "unsafe"

// Host function imports from alfred_v1.

//go:wasmimport alfred_v1 log
func hostLog(level int32, ptr uintptr, size uint32)

//go:wasmimport alfred_v1 tool_result
func hostToolResult(ptr uintptr, size uint32)

// Memory management exports for the host.

//export malloc
func wasmMalloc(size uint32) uintptr {
	buf := make([]byte, size)
	return uintptr(unsafe.Pointer(&buf[0]))
}

//export free
func wasmFree(ptr uintptr, size uint32) {
	// No-op: TinyGo GC handles deallocation.
}

// Plugin lifecycle exports.

//export _init
func pluginInit() {
	logMsg(1, "` + name + ` plugin initialized")
}

//export _close
func pluginClose() {
	logMsg(1, "` + name + ` plugin closed")
}

// Tool execution export.

//export tool_execute
func toolExecute(ptr uintptr, size uint32) {
	// Read input parameters from WASM memory.
	_ = ptrToString(ptr, size)

	// Process the input and produce a result.
	result := ` + "`" + `{"content":"Hello from ` + name + `!","is_error":false}` + "`" + `

	resultBytes := []byte(result)
	hostToolResult(uintptr(unsafe.Pointer(&resultBytes[0])), uint32(len(resultBytes)))
}

func logMsg(level int32, msg string) {
	ptr := uintptr(unsafe.Pointer(unsafe.StringData(msg)))
	hostLog(level, ptr, uint32(len(msg)))
}

func ptrToString(ptr uintptr, size uint32) string {
	return unsafe.String((*byte)(unsafe.Pointer(ptr)), size)
}

func main() {}
`
}

func pluginMakefileTemplate() string {
	return `.PHONY: build clean validate

build:
	tinygo build -o plugin.wasm -target wasi -no-debug main.go

clean:
	rm -f plugin.wasm

validate:
	alfred-ai plugin validate .
`
}

func pluginReadmeTemplate(name string) string {
	return `# ` + name + `

An alfred-ai WASM plugin.

## Build

` + "```" + `bash
make build
` + "```" + `

## Validate

` + "```" + `bash
alfred-ai plugin validate .
` + "```" + `

## Configuration

Add the plugin directory to your config.yaml:

` + "```" + `yaml
plugins:
  enabled: true
  wasm_enabled: true
  dirs:
    - ./plugins
` + "```" + `
`
}

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func registryAndInstaller(cfg *config.Config) (*plugin.Registry, *plugin.Installer) {
	cacheDir := filepath.Join(os.TempDir(), "alfredai-plugin-cache")
	regURL := cfg.Plugins.RegistryURL
	if regURL == "" {
		regURL = "https://raw.githubusercontent.com/alfredai/plugins/main/plugins.json"
	}

	pluginDir := "./plugins"
	if len(cfg.Plugins.Dirs) > 0 {
		pluginDir = cfg.Plugins.Dirs[0]
	}

	logger := slogDiscard()
	reg := plugin.NewRegistry(regURL, cacheDir, logger)
	inst := plugin.NewInstaller(pluginDir, reg, logger)
	return reg, inst
}

func runPluginSearch(query string) error {
	cfg, err := loadConfigOrDefault(configPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	reg, _ := registryAndInstaller(cfg)
	results, err := reg.Search(context.Background(), query)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		fmt.Printf("No plugins found for %q.\n", query)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION\tVERIFIED")
	for _, e := range results {
		verified := " "
		if e.Verified {
			verified = "yes"
		}
		desc := e.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.Version, desc, verified)
	}
	return w.Flush()
}

func runPluginInstall(name string) error {
	cfg, err := loadConfigOrDefault(configPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	_, inst := registryAndInstaller(cfg)
	if err := inst.Install(context.Background(), name); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	fmt.Printf("Plugin %q installed successfully.\n", name)
	return nil
}

func runPluginUpdate(name string) error {
	cfg, err := loadConfigOrDefault(configPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	_, inst := registryAndInstaller(cfg)
	if err := inst.Update(context.Background(), name); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	fmt.Printf("Plugin %q updated successfully.\n", name)
	return nil
}

func runPluginRemove(name string) error {
	cfg, err := loadConfigOrDefault(configPath())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	_, inst := registryAndInstaller(cfg)
	if err := inst.Remove(name); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	fmt.Printf("Plugin %q removed successfully.\n", name)
	return nil
}

func runPluginPublish(path string) error {
	// Validate the plugin first (reuse existing validation logic).
	manifestPath := filepath.Join(path, "plugin.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w (does %s contain a plugin.yaml?)", err, path)
	}

	var m domain.PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("malformed plugin.yaml: %w", err)
	}

	var issues []string
	if m.Name == "" {
		issues = append(issues, "name is required")
	}
	if m.Version == "" {
		issues = append(issues, "version is required")
	}
	if len(m.Types) == 0 {
		issues = append(issues, "at least one type is required")
	}
	if m.Description == "" {
		issues = append(issues, "description is required for publishing")
	}
	if m.Author == "" {
		issues = append(issues, "author is required for publishing")
	}

	if len(issues) > 0 {
		fmt.Println("Plugin is not ready to publish:")
		for _, issue := range issues {
			fmt.Printf("  FAIL: %s\n", issue)
		}
		return fmt.Errorf("publish validation failed with %d issues", len(issues))
	}

	// Generate the registry entry for the PR.
	entry, err := json.MarshalIndent(map[string]any{
		"name":        m.Name,
		"version":     m.Version,
		"description": m.Description,
		"author":      m.Author,
		"types":       m.Types,
	}, "  ", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry entry: %w", err)
	}

	fmt.Printf("Plugin %q v%s is valid and ready to publish.\n\n", m.Name, m.Version)
	fmt.Println("Registry entry (add this to plugins.json in your PR):")
	fmt.Println()
	fmt.Printf("  %s\n\n", string(entry))
	fmt.Println("To submit your plugin to the registry:")
	fmt.Println("  1. Fork https://github.com/alfredai/plugins")
	fmt.Println("  2. Add the registry entry above to plugins.json")
	fmt.Println("  3. Open a pull request with your plugin files")
	fmt.Println("  4. A maintainer will review and merge your submission")
	return nil
}

func loadConfigOrDefault(path string) (*config.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return config.Defaults(), nil
	}
	return config.Load(path)
}
