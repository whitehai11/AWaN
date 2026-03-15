package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Definition describes a runtime agent loaded from a .awand file.
type Definition struct {
	Name        string
	Model       string
	Memory      bool
	Tools       []string
	Description string
	SourcePath  string
}

// LoadAgents scans a directory and loads all *.awand agent definitions.
func LoadAgents(dir string) (map[string]Definition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	agents := make(map[string]Definition)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".awand" {
			continue
		}

		fullPath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}

		definition, err := ParseAgentFile(data)
		if err != nil {
			return nil, err
		}

		definition.SourcePath = fullPath
		agents[definition.Name] = definition
	}

	return agents, nil
}

// ParseAgentFile parses custom .awand agent definitions.
func ParseAgentFile(data []byte) (Definition, error) {
	definition := Definition{
		Model:  "openai",
		Memory: true,
		Tools:  []string{},
	}

	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Definition{}, errors.New("invalid .awand line: " + line)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "name":
			definition.Name = value
		case "model":
			definition.Model = value
		case "memory":
			definition.Memory = strings.EqualFold(value, "true")
		case "tools":
			if value == "" {
				definition.Tools = []string{}
				continue
			}

			parts := strings.Split(value, ",")
			tools := make([]string, 0, len(parts))
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					tools = append(tools, trimmed)
				}
			}
			definition.Tools = tools
		case "description":
			definition.Description = value
		}
	}

	if definition.Name == "" {
		return Definition{}, errors.New("agent definition is missing name")
	}
	if definition.Model == "" {
		return Definition{}, errors.New("agent definition is missing model")
	}

	return definition, nil
}
