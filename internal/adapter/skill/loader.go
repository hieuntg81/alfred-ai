package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"alfred-ai/internal/domain"
)

// FileSkillProvider loads skills from markdown files in a directory.
type FileSkillProvider struct {
	dir    string
	mu     sync.RWMutex
	skills map[string]domain.Skill
}

// NewFileSkillProvider creates a skill provider that reads from the given directory.
func NewFileSkillProvider(dir string) *FileSkillProvider {
	return &FileSkillProvider{
		dir:    dir,
		skills: make(map[string]domain.Skill),
	}
}

// Load reads skill files from the skill directory and parses them.
// It supports two layouts:
//   - Flat: skills/*.md (one file per skill)
//   - Subdirectory: skills/<name>/SKILL.md (one directory per skill)
func (p *FileSkillProvider) Load(_ context.Context) ([]domain.Skill, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, fmt.Errorf("read skill dir %s: %w", p.dir, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var skills []domain.Skill
	for _, entry := range entries {
		var path string
		if entry.IsDir() {
			// Subdirectory layout: look for SKILL.md inside
			candidate := filepath.Join(p.dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(candidate); err != nil {
				continue // no SKILL.md inside, skip
			}
			path = candidate
		} else if strings.HasSuffix(entry.Name(), ".md") {
			path = filepath.Join(p.dir, entry.Name())
		} else {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat skill file %s: %w", path, err)
		}
		if info.Size() > maxSkillFileSize {
			return nil, fmt.Errorf("skill file %s too large (%d bytes, max %d)", path, info.Size(), maxSkillFileSize)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read skill file %s: %w", path, err)
		}

		skill, err := parseSkillFile(string(data))
		if err != nil {
			return nil, fmt.Errorf("parse skill file %s: %w", path, err)
		}

		if _, exists := p.skills[skill.Name]; exists {
			return nil, fmt.Errorf("duplicate skill name %q in %s", skill.Name, path)
		}
		p.skills[skill.Name] = skill
		skills = append(skills, skill)
	}

	return skills, nil
}

// Get returns a skill by name.
func (p *FileSkillProvider) Get(name string) (*domain.Skill, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	s, ok := p.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}
	return &s, nil
}

// List returns all loaded skills.
func (p *FileSkillProvider) List() []domain.Skill {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]domain.Skill, 0, len(p.skills))
	for _, s := range p.skills {
		result = append(result, s)
	}
	return result
}

// maxSkillFileSize is the maximum allowed skill file size (1 MiB).
const maxSkillFileSize = 1 << 20

// validTriggers are the allowed values for the Trigger field.
var validTriggers = map[string]bool{
	"tool":   true,
	"prompt": true,
	"both":   true,
}

// parseSkillFile parses a markdown file with YAML frontmatter (--- delimited).
func parseSkillFile(content string) (domain.Skill, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return domain.Skill{}, fmt.Errorf("missing frontmatter delimiter")
	}

	// Split on --- to get frontmatter and body
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) != 2 {
		return domain.Skill{}, fmt.Errorf("missing closing frontmatter delimiter")
	}

	frontmatter := strings.TrimSpace(parts[0])
	body := strings.TrimSpace(parts[1])

	skill := domain.Skill{
		Metadata: make(map[string]string),
		Template: body,
	}

	// Parse simple YAML frontmatter (key: value format)
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])

		switch key {
		case "name":
			skill.Name = value
		case "description":
			skill.Description = value
		case "trigger":
			skill.Trigger = value
		case "tags":
			skill.Tags = parseTags(value)
		case "tools":
			skill.Tools = parseTags(value)
		case "model_preference":
			skill.ModelPreference = value
		default:
			skill.Metadata[key] = value
		}
	}

	if skill.Name == "" {
		return domain.Skill{}, fmt.Errorf("skill missing name in frontmatter")
	}
	if skill.Trigger == "" {
		skill.Trigger = "tool"
	}
	if !validTriggers[skill.Trigger] {
		return domain.Skill{}, fmt.Errorf("invalid trigger %q: must be one of tool, prompt, both", skill.Trigger)
	}

	return skill, nil
}

// parseTags parses [tag1, tag2] or tag1, tag2 format.
func parseTags(s string) []string {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	var tags []string
	for _, p := range parts {
		tag := strings.TrimSpace(p)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
