package models

import (
	"fmt"
	"sync"

	"github.com/whitehai11/AWaN/core/auth"
	"github.com/whitehai11/AWaN/core/config"
)

// Factory creates a configured model instance at runtime.
type Factory func() (Model, error)

// Registry stores named model factories for the runtime.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: map[string]Factory{},
	}
}

// RegisterModel registers a model factory under a stable provider name.
func (r *Registry) RegisterModel(name string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// GetModel instantiates a configured model by name.
func (r *Registry) GetModel(name string) (Model, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("model %q is not registered", name)
	}

	return factory()
}

// RegisteredModels returns the currently available provider names.
func (r *Registry) RegisteredModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}

	return names
}

// NewDefaultRegistry wires the built-in model providers for the runtime.
func NewDefaultRegistry(cfg *config.Config, oauth *auth.OAuthManager) *Registry {
	registry := NewRegistry()
	registry.RegisterModel("openai", func() (Model, error) {
		return NewOpenAIModel(cfg.OpenAI, oauth), nil
	})
	registry.RegisterModel("ollama", func() (Model, error) {
		return NewOllamaModel(cfg.Ollama), nil
	})
	return registry
}
