package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AgentFS wraps all file operations within the AWaN runtime home.
type AgentFS struct {
	paths *Paths
}

// New creates an isolated filesystem rooted in ~/.awan or a configured path.
func New(root string) (*AgentFS, error) {
	paths, err := NewPaths(root)
	if err != nil {
		return nil, err
	}

	return &AgentFS{paths: paths}, nil
}

// Paths exposes the resolved directory layout.
func (fs *AgentFS) Paths() *Paths {
	return fs.paths
}

// WriteFile writes data under ~/.awan/files.
func (fs *AgentFS) WriteFile(path string, data []byte) error {
	target, err := fs.resolve(fs.paths.Files, path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}

	return os.WriteFile(target, data, 0o600)
}

// ReadFile reads data from ~/.awan/files.
func (fs *AgentFS) ReadFile(path string) ([]byte, error) {
	target, err := fs.resolve(fs.paths.Files, path)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(target)
}

// WriteConfigFile writes configuration data under ~/.awan/config.
func (fs *AgentFS) WriteConfigFile(path string, data []byte) error {
	target, err := fs.resolve(fs.paths.Config, path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}

	return os.WriteFile(target, data, 0o600)
}

// ListFiles returns relative file names under ~/.awan/files without file contents.
func (fs *AgentFS) ListFiles() ([]string, error) {
	files := make([]string, 0)

	err := filepath.WalkDir(fs.paths.Files, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(fs.paths.Files, path)
		if err != nil {
			return err
		}

		files = append(files, filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func (fs *AgentFS) resolve(base, relativePath string) (string, error) {
	cleaned := filepath.Clean(relativePath)
	if cleaned == "." || cleaned == "" {
		return "", errors.New("path cannot be empty")
	}
	if filepath.IsAbs(cleaned) {
		return "", errors.New("absolute paths are not allowed")
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", errors.New("path escapes the agent filesystem")
	}

	target := filepath.Join(base, cleaned)
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes the agent filesystem")
	}

	return target, nil
}
