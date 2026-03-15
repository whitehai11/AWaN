package types

import "time"

// AgentRequest represents a single agent invocation.
type AgentRequest struct {
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// AgentResponse is the result returned from a model-backed agent run.
type AgentResponse struct {
	Agent     string    `json:"agent"`
	Model     string    `json:"model"`
	Output    string    `json:"output"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
}
