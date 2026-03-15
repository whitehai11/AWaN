package agent

import "strings"

// BuildSystemPrompt returns the shared token-efficient runtime instruction block.
func BuildSystemPrompt(envID string) string {
	return strings.Join([]string{
		"You are an AI agent running inside AWaN.",
		"",
		"Environment ID: " + envID,
		"",
		"To use tools respond with JSON.",
	}, "\n")
}

// BuildStateSnapshot returns the minimal per-request environment snapshot.
func BuildStateSnapshot(agentName string, files []string, memoryIDs []string) string {
	return strings.Join([]string{
		"AGENT: " + agentName,
		"",
		"FILES:",
		strings.Join(files, ","),
		"",
		"MEM:",
		strings.Join(memoryIDs, ","),
	}, "\n")
}
