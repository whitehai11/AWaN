package agent

import "time"

// Context tracks metadata for a single agent run.
type Context struct {
	AgentName string
	Prompt    string
	ModelName string
	StartedAt time.Time
}

// NewContext creates execution metadata for an agent run.
func NewContext(agentName, prompt, modelName string) *Context {
	return &Context{
		AgentName: agentName,
		Prompt:    prompt,
		ModelName: modelName,
		StartedAt: time.Now().UTC(),
	}
}
