package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest describes a plugin loaded from plugin.json.
type Manifest struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Entry       string            `json:"entry"`
	Parameters  map[string]string `json:"parameters"`
}

// Definition represents a discovered plugin.
type Definition struct {
	Manifest Manifest
	Dir      string
	Entry    string
}

// LoadPlugins scans plugin directories and loads their manifests.
func LoadPlugins(root string) (map[string]Definition, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	plugins := make(map[string]Definition)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(root, entry.Name())
		definition, err := loadPluginDefinition(pluginDir)
		if err != nil {
			return nil, err
		}

		plugins[definition.Manifest.Name] = definition
	}

	return plugins, nil
}

func loadPluginDefinition(pluginDir string) (Definition, error) {
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Definition{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Definition{}, err
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return Definition{}, errors.New("plugin manifest is missing name")
	}

	entry, err := resolveEntry(pluginDir, manifest.Entry)
	if err != nil {
		return Definition{}, err
	}

	return Definition{
		Manifest: manifest,
		Dir:      pluginDir,
		Entry:    entry,
	}, nil
}

func resolveEntry(pluginDir, declaredEntry string) (string, error) {
	candidates := []string{}
	if strings.TrimSpace(declaredEntry) != "" {
		candidates = append(candidates, declaredEntry)
	}
	candidates = append(candidates, "runner.js", "runner.ts", "runner.py", "runner.sh", "runner", "main.js", "main.ts")

	for _, candidate := range candidates {
		cleaned := filepath.Clean(strings.TrimSpace(candidate))
		if cleaned == "" || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			continue
		}

		path := filepath.Join(pluginDir, cleaned)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", fmt.Errorf("plugin in %q is missing a valid entry file", pluginDir)
}
