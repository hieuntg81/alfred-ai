package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const maxIncludeDepth = 10

// processIncludes merges config files referenced by cfg.Includes into cfg.
// basePath is the directory of the config file that contains the includes.
// visited tracks absolute paths to detect circular includes.
func processIncludes(cfg *Config, basePath string, visited map[string]bool, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("config includes: max depth %d exceeded", maxIncludeDepth)
	}

	if visited == nil {
		visited = make(map[string]bool)
	}

	for _, pattern := range cfg.Includes {
		paths, err := resolveIncludePaths(pattern, basePath)
		if err != nil {
			return err
		}
		for _, p := range paths {
			abs, err := filepath.Abs(p)
			if err != nil {
				return fmt.Errorf("config includes: abs path %q: %w", p, err)
			}

			if visited[abs] {
				return fmt.Errorf("config includes: circular include detected for %q", abs)
			}
			visited[abs] = true

			if err := mergeFile(cfg, abs, visited, depth+1); err != nil {
				return err
			}
		}
	}

	// Clear includes so they don't re-process on second unmarshal pass.
	cfg.Includes = nil
	return nil
}

// resolveIncludePaths resolves a pattern (which may contain globs) relative to baseDir.
// It validates that the resolved path does not escape baseDir.
func resolveIncludePaths(pattern, baseDir string) ([]string, error) {
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(baseDir, pattern)
	}

	pattern = filepath.Clean(pattern)

	// Check for path traversal: resolved path must be under baseDir or absolute.
	rel, err := filepath.Rel(baseDir, pattern)
	if err == nil && len(rel) >= 2 && rel[:2] == ".." {
		return nil, fmt.Errorf("config includes: path %q escapes config directory", pattern)
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("config includes: glob %q: %w", pattern, err)
	}

	if len(matches) == 0 {
		// Non-glob pattern: treat as literal path, let mergeFile report file-not-found.
		if !hasMeta(pattern) {
			return []string{pattern}, nil
		}
		// Glob matched nothing — not an error.
		return nil, nil
	}

	return matches, nil
}

// hasMeta reports whether the pattern contains any glob metacharacters.
func hasMeta(pattern string) bool {
	for _, c := range pattern {
		switch c {
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// mergeFile reads a YAML file and unmarshals it onto cfg (overlaying existing values).
// It also processes any includes in the included file (supporting nested includes).
func mergeFile(cfg *Config, path string, visited map[string]bool, depth int) error {
	if err := validatePermissions(path); err != nil {
		return fmt.Errorf("config includes: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config includes: read %q: %w", path, err)
	}

	if len(data) == 0 {
		return nil
	}

	// Clear includes before unmarshaling so only this file's includes are detected.
	cfg.Includes = nil

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config includes: parse %q: %w", path, err)
	}

	// Check for nested includes from this file.
	if len(cfg.Includes) > 0 {
		baseDir := filepath.Dir(path)
		if err := processIncludes(cfg, baseDir, visited, depth); err != nil {
			return err
		}
	}

	return nil
}
