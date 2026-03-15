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

// InstalledPlugin describes a plugin present on disk, including status.
type InstalledPlugin struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Entry       string            `json:"entry"`
	Parameters  map[string]string `json:"parameters"`
	Status      string            `json:"status"`
	Dir         string            `json:"dir"`
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

// ListInstalledPlugins returns enabled and disabled plugins present on disk.
func ListInstalledPlugins(root string) ([]InstalledPlugin, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	result := make([]InstalledPlugin, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(root, entry.Name())
		manifest, status, err := readInstalledManifest(pluginDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		result = append(result, InstalledPlugin{
			Name:        manifest.Name,
			Version:     manifest.Version,
			Description: manifest.Description,
			Entry:       manifest.Entry,
			Parameters:  manifest.Parameters,
			Status:      status,
			Dir:         pluginDir,
		})
	}

	return result, nil
}

func readInstalledManifest(pluginDir string) (Manifest, string, error) {
	type candidate struct {
		file   string
		status string
	}
	for _, item := range []candidate{
		{file: "plugin.json", status: "enabled"},
		{file: "plugin.disabled.json", status: "disabled"},
	} {
		data, err := os.ReadFile(filepath.Join(pluginDir, item.file))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Manifest{}, "", err
		}

		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return Manifest{}, "", err
		}
		return manifest, item.status, nil
	}

	return Manifest{}, "", os.ErrNotExist
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
