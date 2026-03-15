package agent

import (
	"errors"
	"strings"
	"time"

	"github.com/whitehai11/AWaN/core/filesystem"
	"github.com/whitehai11/AWaN/core/memory"
	"github.com/whitehai11/AWaN/core/models"
	"github.com/whitehai11/AWaN/core/tools"
	"github.com/whitehai11/AWaN/core/types"
	"github.com/whitehai11/AWaN/core/utils"
)

// Agent wraps a configured model and runtime services.
type Agent struct {
	definition Definition
	model      models.Model
	memory     *memory.Manager
	fs         *filesystem.AgentFS
	tools      *tools.Runner
	logger     *utils.Logger
}

// NewAgent creates an agent bound to a specific model implementation.
func NewAgent(definition Definition, model models.Model, memoryManager *memory.Manager, fs *filesystem.AgentFS, toolRunner *tools.Runner, logger *utils.Logger) *Agent {
	return &Agent{
		definition: definition,
		model:      model,
		memory:     memoryManager,
		fs:         fs,
		tools:      toolRunner,
		logger:     logger,
	}
}

// Run sends a prompt through the agent loop and stores the interaction in memory.
func (a *Agent) Run(prompt string) (*types.AgentResponse, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("prompt cannot be empty")
	}

	ctx := NewContext(a.definition.Name, prompt, a.model.Name())
	a.logger.Log("AGENT", "Running prompt")

	files, err := a.fs.ListFiles()
	if err != nil {
		return nil, err
	}

	memoryIDs := []string{}
	if a.definition.Memory {
		memoryIDs, err = a.memory.MemoryIDs(a.definition.Name, 12)
		if err != nil {
			return nil, err
		}
	}

	envID := GenerateEnvironmentID(CapabilityContext{
		Tools:       a.definition.Tools,
		Filesystem:  true,
		Memory:      a.definition.Memory,
		Plugins:     a.tools != nil,
		Permissions: []string{"agentfs:read", "agentfs:write", "memory:id-only"},
	})

	fullPrompt := strings.Join([]string{
		BuildSystemPrompt(envID),
		"",
		BuildStateSnapshot(a.definition.Name, files, memoryIDs),
		"",
		"USER:",
		strings.TrimSpace(prompt),
	}, "\n")

	output, err := RunLoop(a.model, fullPrompt)
	if err != nil {
		return nil, err
	}

	if a.definition.Memory {
		if err := a.memory.Store(types.MemoryRecord{
			Agent:   a.definition.Name,
			Role:    "user",
			Content: strings.TrimSpace(prompt),
		}); err != nil {
			return nil, err
		}
		if err := a.memory.Store(types.MemoryRecord{
			Agent:   a.definition.Name,
			Role:    "assistant",
			Content: output,
		}); err != nil {
			return nil, err
		}
	}

	return &types.AgentResponse{
		Agent:     a.definition.Name,
		Model:     a.model.Name(),
		Output:    output,
		StartedAt: ctx.StartedAt,
		EndedAt:   time.Now().UTC(),
	}, nil
}
