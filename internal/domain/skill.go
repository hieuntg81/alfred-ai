package domain

import "context"

// Skill represents a reusable prompt/tool template loaded from markdown files.
type Skill struct {
	Name            string
	Description     string
	Tags            []string
	Trigger         string // "tool", "prompt", or "both"
	Tools           []string
	ModelPreference string // e.g. "fast", "default", "powerful"
	Metadata        map[string]string
	Template        string
}

// SkillProvider loads and manages skills.
type SkillProvider interface {
	Load(ctx context.Context) ([]Skill, error)
	Get(name string) (*Skill, error)
	List() []Skill
}

// ModelRouter routes a model preference label (e.g. "fast", "powerful") to a concrete LLM provider.
type ModelRouter interface {
	Route(preference string) (LLMProvider, error)
}
