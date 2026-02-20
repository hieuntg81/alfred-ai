package setup

import (
	"fmt"
	"io"
	"os"

	"alfred-ai/internal/infra/config"
	"gopkg.in/yaml.v3"
)

// SaveConfig validates and writes cfg to path with 0600 permissions.
func SaveConfig(cfg *config.Config, path string, w io.Writer) error {
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Fprintf(w, "Config written to %s\n", path)
	return nil
}
