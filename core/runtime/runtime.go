package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/whitehai11/AWaN/core/agent"
	"github.com/whitehai11/AWaN/core/auth"
	"github.com/whitehai11/AWaN/core/config"
	"github.com/whitehai11/AWaN/core/filesystem"
	"github.com/whitehai11/AWaN/core/memory"
	"github.com/whitehai11/AWaN/core/models"
	"github.com/whitehai11/AWaN/core/plugins"
	"github.com/whitehai11/AWaN/core/types"
	"github.com/whitehai11/AWaN/core/utils"
)

// Runtime owns configuration, model registration, storage, and agent execution.
type Runtime struct {
	config   *config.Config
	logger   *utils.Logger
	auth     *auth.OAuthManager
	registry *models.Registry
	memory   *memory.Manager
	fs       *filesystem.AgentFS
	agents   map[string]agent.Definition
	plugins  *plugins.Runner
	pluginRegistry *plugins.Registry
}

// New creates a runtime using the provided configuration.
func New(cfg *config.Config) (*Runtime, error) {
	logger := utils.NewLogger()

	agentFS, err := filesystem.New(cfg.Storage.RootPath)
	if err != nil {
		return nil, err
	}

	memoryManager, err := memory.NewManager(agentFS)
	if err != nil {
		return nil, err
	}

	pluginRunner, err := plugins.NewRunner(agentFS)
	if err != nil {
		return nil, err
	}

	oauthManager := auth.NewOpenAIManagerFromEnv(cfg.Auth.StoragePath)

	loadedAgents, err := agent.LoadAgents(agentFS.Paths().Agents)
	if err != nil {
		return nil, err
	}
	if _, ok := loadedAgents[cfg.DefaultAgent]; !ok {
		loadedAgents[cfg.DefaultAgent] = agent.Definition{
			Name:        cfg.DefaultAgent,
			Model:       cfg.DefaultModel,
			Memory:      true,
			Tools:       []string{"filesystem", "memory"},
			Description: "Default AWaN agent",
		}
	}

	return &Runtime{
		config:   cfg,
		logger:   logger,
		auth:     oauthManager,
		registry: models.NewDefaultRegistry(cfg, oauthManager),
		memory:   memoryManager,
		fs:       agentFS,
		agents:   loadedAgents,
		plugins:  pluginRunner,
		pluginRegistry: plugins.NewRegistry(agentFS, ""),
	}, nil
}

// Config returns the loaded runtime configuration.
func (r *Runtime) Config() *config.Config {
	return r.config
}

// Logger returns the runtime logger.
func (r *Runtime) Logger() *utils.Logger {
	return r.logger
}

// Filesystem returns the isolated agent filesystem.
func (r *Runtime) Filesystem() *filesystem.AgentFS {
	return r.fs
}

// OAuthManager returns the runtime OAuth manager.
func (r *Runtime) OAuthManager() *auth.OAuthManager {
	return r.auth
}

// RegisteredAgents returns the loaded agent definitions.
func (r *Runtime) RegisteredAgents() map[string]agent.Definition {
	agents := make(map[string]agent.Definition, len(r.agents))
	for name, definition := range r.agents {
		agents[name] = definition
	}
	return agents
}

// RegisteredPlugins returns the loaded plugin definitions.
func (r *Runtime) RegisteredPlugins() map[string]plugins.Definition {
	if r.plugins == nil {
		return map[string]plugins.Definition{}
	}
	return r.plugins.RegisteredPlugins()
}

// PluginRegistry returns the registry client.
func (r *Runtime) PluginRegistry() *plugins.Registry {
	return r.pluginRegistry
}

// ListFiles returns the lightweight runtime file list.
func (r *Runtime) ListFiles() ([]string, error) {
	return r.fs.ListFiles()
}

// Agent creates a configured agent instance for the requested model.
func (r *Runtime) Agent(agentName, modelName string) (*agent.Agent, error) {
	if strings.TrimSpace(agentName) == "" {
		agentName = r.config.DefaultAgent
	}

	definition, ok := r.agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %q is not registered", agentName)
	}
	if strings.TrimSpace(modelName) == "" {
		modelName = definition.Model
	}

	model, err := r.registry.GetModel(modelName)
	if err != nil {
		return nil, err
	}

	r.logger.Log("MODEL", fmt.Sprintf("Using %s", model.Name()))
	definition.Model = modelName
	return agent.NewAgent(definition, model, r.memory, r.fs, r.plugins, r.logger), nil
}

// Run executes a single agent request.
func (r *Runtime) Run(request types.AgentRequest) (*types.AgentResponse, error) {
	agentInstance, err := r.Agent(request.Agent, request.Model)
	if err != nil {
		return nil, err
	}

	return agentInstance.Run(request.Prompt)
}

// Chat currently shares the same execution path as Run while preserving a dedicated API endpoint.
func (r *Runtime) Chat(request types.AgentRequest) (*types.AgentResponse, error) {
	return r.Run(request)
}

// MemorySnapshot returns the current memory state for an agent.
func (r *Runtime) MemorySnapshot(agentName string) (*types.MemorySnapshot, error) {
	if strings.TrimSpace(agentName) == "" {
		agentName = r.config.DefaultAgent
	}

	return r.memory.Snapshot(agentName)
}

// StoreMemory persists a record outside the normal run loop.
func (r *Runtime) StoreMemory(request types.MemoryStoreRequest) (*types.MemoryRecord, error) {
	if strings.TrimSpace(request.Content) == "" {
		return nil, errors.New("memory content cannot be empty")
	}

	record := types.MemoryRecord{
		ID:        time.Now().UTC().Format("20060102150405.000000000"),
		Agent:     fallback(request.Agent, r.config.DefaultAgent),
		Role:      fallback(request.Role, "note"),
		Content:   strings.TrimSpace(request.Content),
		CreatedAt: time.Now().UTC(),
	}

	if err := r.memory.Store(record); err != nil {
		return nil, err
	}

	return &record, nil
}

// ExecuteTool runs a permitted plugin for the requested agent.
func (r *Runtime) ExecuteTool(agentName, toolName string, args map[string]any) (*plugins.ExecuteResponse, error) {
	definition, ok := r.agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %q is not registered", agentName)
	}
	if r.plugins == nil {
		return nil, errors.New("plugin runner is not available")
	}

	return r.plugins.Execute(nil, definition.Tools, toolName, args)
}

// ListRegistryPlugins fetches the public registry contents.
func (r *Runtime) ListRegistryPlugins() ([]plugins.RegistryPlugin, error) {
	if r.pluginRegistry == nil {
		return nil, errors.New("plugin registry is not available")
	}
	return r.pluginRegistry.ListPlugins(nil)
}

// SearchRegistryPlugins searches the public plugin registry.
func (r *Runtime) SearchRegistryPlugins(query string) ([]plugins.RegistryPlugin, error) {
	if r.pluginRegistry == nil {
		return nil, errors.New("plugin registry is not available")
	}
	return r.pluginRegistry.SearchPlugins(nil, query)
}

// InstallRegistryPlugin downloads and installs a plugin by name.
func (r *Runtime) InstallRegistryPlugin(name string) (*plugins.InstallResult, error) {
	if r.pluginRegistry == nil {
		return nil, errors.New("plugin registry is not available")
	}
	result, err := r.pluginRegistry.InstallPlugin(nil, name)
	if err != nil {
		return nil, err
	}

	reloaded, err := plugins.NewRunner(r.fs)
	if err != nil {
		return nil, err
	}
	r.plugins = reloaded
	return result, nil
}

// InstalledPlugins lists plugins present in ~/.awan/plugins.
func (r *Runtime) InstalledPlugins() ([]plugins.RegistryPlugin, error) {
	if r.pluginRegistry == nil {
		return nil, errors.New("plugin registry is not available")
	}
	return r.pluginRegistry.InstalledPlugins()
}

// RemoveInstalledPlugin deletes an installed plugin and reloads the runtime runner.
func (r *Runtime) RemoveInstalledPlugin(name string) (*plugins.RemoveResult, error) {
	if r.pluginRegistry == nil {
		return nil, errors.New("plugin registry is not available")
	}

	result, err := r.pluginRegistry.RemovePlugin(name)
	if err != nil {
		return nil, err
	}

	reloaded, err := plugins.NewRunner(r.fs)
	if err != nil {
		return nil, err
	}
	r.plugins = reloaded
	return result, nil
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}
