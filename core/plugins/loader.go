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
	Tools       []string          `json:"tools"`
	Parameters  map[string]string `json:"parameters"`
	Permissions []string          `json:"permissions"`
}

// Definition represents a discovered plugin.
type Definition struct {
	Manifest Manifest
	Dir      string
	Entry    string
	Tool     string
}

// InstalledPlugin describes a plugin present on disk, including status.
type InstalledPlugin struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Entry       string            `json:"entry"`
	Tools       []string          `json:"tools"`
	Parameters  map[string]string `json:"parameters"`
	Permissions []string          `json:"permissions"`
	Status      string            `json:"status"`
	Dir         string            `json:"dir"`
	SourceType  string            `json:"sourceType"`
	Repo        string            `json:"repo"`
	LatestVersion string          `json:"latestVersion,omitempty"`
	UpdateAvailable bool          `json:"updateAvailable"`
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

		tools := definition.Manifest.Tools
		if len(tools) == 0 {
			tools = []string{definition.Manifest.Name}
		}
		for _, tool := range tools {
			tool = strings.TrimSpace(tool)
			if tool == "" {
				continue
			}
			copyDefinition := definition
			copyDefinition.Tool = tool
			plugins[tool] = copyDefinition
		}
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
	if strings.TrimSpace(manifest.Version) == "" {
		return Definition{}, errors.New("plugin manifest is missing version")
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
	state, err := loadPluginState(root)
	if err != nil {
		return nil, err
	}

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
		metadata := readInstallMetadata(pluginDir)
		stateEntry := state[manifest.Name]
		version := manifest.Version
		if strings.TrimSpace(version) == "" {
			version = stateEntry.Version
		}
		repo := metadata.Repo
		if repo == "" {
			repo = stateEntry.Repo
		}
		sourceType := metadata.SourceType
		if sourceType == "" {
			sourceType = stateEntry.SourceType
		}

		result = append(result, InstalledPlugin{
			Name:        manifest.Name,
			Version:     version,
			Description: manifest.Description,
			Entry:       manifest.Entry,
			Tools:       manifest.Tools,
			Parameters:  manifest.Parameters,
			Permissions: manifest.Permissions,
			Status:      status,
			Dir:         pluginDir,
			SourceType:  sourceType,
			Repo:        repo,
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

type installMetadata struct {
	SourceType string `json:"sourceType"`
	Repo       string `json:"repo"`
}

func readInstallMetadata(pluginDir string) installMetadata {
	data, err := os.ReadFile(filepath.Join(pluginDir, ".awan-plugin.json"))
	if err != nil {
		return installMetadata{}
	}

	var metadata installMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return installMetadata{}
	}
	return metadata
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
