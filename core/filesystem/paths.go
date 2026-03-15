package filesystem

import (
	"os"
	"path/filepath"
)

const defaultRootDir = ".awan"

// Paths contains the isolated directory layout for the runtime.
type Paths struct {
	Root   string
	Agents string
	Memory string
	Files  string
	Config string
	Tools  string
	Sandbox string
}

// NewPaths resolves the AWaN home directory and ensures the layout exists.
func NewPaths(root string) (*Paths, error) {
	if root == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(homeDir, defaultRootDir)
	}

	paths := &Paths{
		Root:   root,
		Agents: filepath.Join(root, "agents"),
		Memory: filepath.Join(root, "memory"),
		Files:  filepath.Join(root, "files"),
		Config: filepath.Join(root, "config"),
		Tools:  filepath.Join(root, "tools"),
		Sandbox: filepath.Join(root, "sandbox"),
	}

	for _, dir := range []string{paths.Root, paths.Agents, paths.Memory, paths.Files, paths.Config, paths.Tools, paths.Sandbox} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}

	return paths, nil
}
