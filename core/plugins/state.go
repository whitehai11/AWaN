package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const pluginStateFilename = "plugins.json"

type PluginStateEntry struct {
	Version     string    `json:"version"`
	Repo        string    `json:"repo,omitempty"`
	SourceType  string    `json:"sourceType,omitempty"`
	InstalledAt time.Time `json:"installedAt,omitempty"`
}

type PluginState map[string]PluginStateEntry

func loadPluginState(root string) (PluginState, error) {
	path := filepath.Join(root, pluginStateFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PluginState{}, nil
		}
		return nil, err
	}

	var state PluginState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state == nil {
		state = PluginState{}
	}
	return state, nil
}

func savePluginState(root string, state PluginState) error {
	if state == nil {
		state = PluginState{}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, pluginStateFilename), data, 0o600)
}
