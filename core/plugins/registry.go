package plugins

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whitehai11/AWaN/core/filesystem"
)

const DefaultRegistryURL = "https://registry.awan.dev/plugins.json"

// RegistryPlugin describes a plugin entry from the public registry.
type RegistryPlugin struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Repo        string `json:"repo"`
	Version     string `json:"version"`
}

// RegistryIndex is the public registry payload.
type RegistryIndex struct {
	Plugins []RegistryPlugin `json:"plugins"`
}

// InstallResult summarizes an installed plugin.
type InstallResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Source  string `json:"source"`
}

// Registry fetches and installs plugins from the public registry.
type Registry struct {
	fs         *filesystem.AgentFS
	client     *http.Client
	registryURL string
}

// NewRegistry creates a plugin registry client.
func NewRegistry(fs *filesystem.AgentFS, registryURL string) *Registry {
	if strings.TrimSpace(registryURL) == "" {
		registryURL = DefaultRegistryURL
	}

	return &Registry{
		fs:          fs,
		client:      &http.Client{Timeout: 45 * time.Second},
		registryURL: registryURL,
	}
}

// FetchRegistry downloads the registry index JSON.
func (r *Registry) FetchRegistry(ctx context.Context) (*RegistryIndex, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.registryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "awan-plugin-registry")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry fetch failed with status %s", resp.Status)
	}

	var index RegistryIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, err
	}

	return &index, nil
}

// ListPlugins returns every plugin from the registry.
func (r *Registry) ListPlugins(ctx context.Context) ([]RegistryPlugin, error) {
	index, err := r.FetchRegistry(ctx)
	if err != nil {
		return nil, err
	}
	return index.Plugins, nil
}

// SearchPlugins returns registry entries that match the query.
func (r *Registry) SearchPlugins(ctx context.Context, query string) ([]RegistryPlugin, error) {
	plugins, err := r.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return plugins, nil
	}

	matches := make([]RegistryPlugin, 0)
	for _, plugin := range plugins {
		haystack := strings.ToLower(strings.Join([]string{
			plugin.Name,
			plugin.Description,
			plugin.Repo,
			plugin.Version,
		}, " "))
		if strings.Contains(haystack, needle) {
			matches = append(matches, plugin)
		}
	}

	return matches, nil
}

// InstallPlugin downloads a plugin from the registry into ~/.awan/plugins.
func (r *Registry) InstallPlugin(ctx context.Context, name string) (*InstallResult, error) {
	matches, err := r.SearchPlugins(ctx, name)
	if err != nil {
		return nil, err
	}

	var selected *RegistryPlugin
	for _, candidate := range matches {
		if strings.EqualFold(candidate.Name, strings.TrimSpace(name)) {
			copyCandidate := candidate
			selected = &copyCandidate
			break
		}
	}
	if selected == nil && len(matches) > 0 {
		copyCandidate := matches[0]
		selected = &copyCandidate
	}
	if selected == nil {
		return nil, fmt.Errorf("plugin %q not found in registry", name)
	}

	return r.installEntry(ctx, *selected)
}

func (r *Registry) installEntry(ctx context.Context, entry RegistryPlugin) (*InstallResult, error) {
	archiveURLCandidates := archiveCandidates(entry)
	if len(archiveURLCandidates) == 0 {
		return nil, fmt.Errorf("plugin %q has no install source", entry.Name)
	}

	tempDir, err := os.MkdirTemp("", "awan-plugin-install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	var archivePath string
	var sourceURL string
	for _, candidate := range archiveURLCandidates {
		archivePath, err = r.downloadFile(ctx, candidate, tempDir)
		if err == nil {
			sourceURL = candidate
			break
		}
	}
	if archivePath == "" {
		return nil, fmt.Errorf("failed to download plugin %q from %s", entry.Name, entry.Repo)
	}

	extractRoot := filepath.Join(tempDir, "extract")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		return nil, err
	}

	if err := extractArchive(archivePath, extractRoot); err != nil {
		return nil, err
	}

	pluginSourceDir, err := findPluginDirectory(extractRoot, entry.Name)
	if err != nil {
		return nil, err
	}

	targetDir := filepath.Join(r.fs.Paths().Plugins, pluginDirName(entry.Name))
	_ = os.RemoveAll(targetDir)
	if err := copyDir(pluginSourceDir, targetDir); err != nil {
		return nil, err
	}

	return &InstallResult{
		Name:    entry.Name,
		Version: entry.Version,
		Path:    targetDir,
		Source:  sourceURL,
	}, nil
}

func archiveCandidates(entry RegistryPlugin) []string {
	repo := strings.TrimSpace(entry.Repo)
	if repo == "" {
		return nil
	}

	if strings.HasSuffix(repo, ".zip") || strings.HasSuffix(repo, ".tar.gz") || strings.HasSuffix(repo, ".tgz") {
		return []string{repo}
	}

	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimRight(repo, "/")

	if !strings.Contains(repo, "github.com/") {
		return []string{repo}
	}

	candidates := make([]string, 0, 4)
	if entry.Version != "" {
		candidates = append(candidates,
			repo+"/archive/refs/tags/v"+entry.Version+".zip",
			repo+"/archive/refs/tags/"+entry.Version+".zip",
		)
	}
	candidates = append(candidates,
		repo+"/archive/refs/heads/main.zip",
		repo+"/archive/refs/heads/master.zip",
	)
	return candidates
}

func (r *Registry) downloadFile(ctx context.Context, url, destinationDir string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "awan-plugin-registry")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed with status %s", resp.Status)
	}

	filename := filepath.Base(strings.Split(url, "?")[0])
	if filename == "" || filename == "." || filename == "/" {
		filename = "plugin.zip"
	}

	target := filepath.Join(destinationDir, filename)
	file, err := os.Create(target)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}

	return target, nil
}

func extractArchive(archivePath, destination string) error {
	switch {
	case strings.HasSuffix(strings.ToLower(archivePath), ".zip"):
		return extractZip(archivePath, destination)
	case strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz"), strings.HasSuffix(strings.ToLower(archivePath), ".tgz"):
		return extractTarGz(archivePath, destination)
	default:
		return fmt.Errorf("unsupported plugin archive %q", archivePath)
	}
}

func extractZip(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		targetPath, err := safeJoin(destination, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return err
		}

		source, err := file.Open()
		if err != nil {
			return err
		}

		target, err := os.Create(targetPath)
		if err != nil {
			source.Close()
			return err
		}

		if _, err := io.Copy(target, source); err != nil {
			target.Close()
			source.Close()
			return err
		}

		target.Close()
		source.Close()
	}

	return nil
}

func extractTarGz(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath, err := safeJoin(destination, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return err
			}

			target, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(target, tarReader); err != nil {
				target.Close()
				return err
			}
			target.Close()
		}
	}

	return nil
}

func findPluginDirectory(root, expectedName string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(entry.Name(), "plugin.json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}

		if strings.EqualFold(manifest.Name, expectedName) || strings.HasPrefix(strings.ToLower(manifest.Name), strings.ToLower(expectedName)+".") {
			matches = append(matches, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", errors.New("no plugin.json found for requested plugin")
	}

	return matches[0], nil
}

func copyDir(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)

		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o600)
	})
}

func safeJoin(root, name string) (string, error) {
	cleanRoot := filepath.Clean(root)
	target := filepath.Join(cleanRoot, filepath.Clean(name))
	relative, err := filepath.Rel(cleanRoot, target)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", fmt.Errorf("archive entry %q escapes install directory", name)
	}
	return target, nil
}

func pluginDirName(name string) string {
	prefix, _, ok := strings.Cut(strings.TrimSpace(name), ".")
	if ok && prefix != "" {
		return prefix
	}
	return strings.TrimSpace(name)
}
