package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/awan/awan/core/agent"
	"github.com/awan/awan/core/auth"
	"github.com/awan/awan/core/config"
	"github.com/awan/awan/core/filesystem"
	"github.com/awan/awan/core/memory"
	"github.com/awan/awan/core/models"
	"github.com/awan/awan/core/tools"
	"github.com/awan/awan/core/types"
	"github.com/awan/awan/core/utils"
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
	tools    *tools.Runner
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

	toolRunner, err := tools.NewRunner(agentFS)
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
		tools:    toolRunner,
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

// RegisteredTools returns the loaded tool definitions.
func (r *Runtime) RegisteredTools() map[string]tools.Definition {
	if r.tools == nil {
		return map[string]tools.Definition{}
	}
	return r.tools.RegisteredTools()
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
	return agent.NewAgent(definition, model, r.memory, r.fs, r.tools, r.logger), nil
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

// ExecuteTool runs a permitted tool for the requested agent.
func (r *Runtime) ExecuteTool(agentName, toolName string, args map[string]any) (*tools.ExecuteResponse, error) {
	definition, ok := r.agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %q is not registered", agentName)
	}
	if r.tools == nil {
		return nil, errors.New("tool runner is not available")
	}

	return r.tools.Execute(nil, definition.Tools, toolName, args)
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}
