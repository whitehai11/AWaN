package plugins

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// CustomPluginRequest installs a plugin directly from a GitHub repository URL.
type CustomPluginRequest struct {
	Repo string `json:"repo"`
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
	Type    string `json:"type"`
}

// RemoveResult summarizes a removed plugin.
type RemoveResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ToggleResult summarizes a plugin enable/disable operation.
type ToggleResult struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
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

// InstalledPlugins lists plugins currently installed under ~/.awan/plugins.
func (r *Registry) InstalledPlugins() ([]RegistryPlugin, error) {
	definitions, err := r.InstalledPluginDetails()
	if err != nil {
		return nil, err
	}

	result := make([]RegistryPlugin, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, RegistryPlugin{
			Name:        definition.Name,
			Description: definition.Description,
			Version:     definition.Version,
			Repo:        definition.Repo,
		})
	}

	return result, nil
}

// InstalledPluginDetails returns installed plugins including enabled/disabled status.
func (r *Registry) InstalledPluginDetails() ([]InstalledPlugin, error) {
	return ListInstalledPlugins(r.fs.Paths().Plugins)
}

// RemovePlugin deletes an installed plugin directory from ~/.awan/plugins.
func (r *Registry) RemovePlugin(name string) (*RemoveResult, error) {
	definitions, err := ListInstalledPlugins(r.fs.Paths().Plugins)
	if err != nil {
		return nil, err
	}

	needle := strings.TrimSpace(name)
	for _, definition := range definitions {
		if !strings.EqualFold(definition.Name, needle) &&
			!strings.EqualFold(pluginDirName(definition.Name), needle) {
			continue
		}

		if err := os.RemoveAll(definition.Dir); err != nil {
			return nil, err
		}

		return &RemoveResult{
			Name: definition.Name,
			Path: definition.Dir,
		}, nil
	}

	targetDir := filepath.Join(r.fs.Paths().Plugins, pluginDirName(needle))
	if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		if err := os.RemoveAll(targetDir); err != nil {
			return nil, err
		}
		return &RemoveResult{Name: needle, Path: targetDir}, nil
	}

	return nil, fmt.Errorf("plugin %q is not installed", name)
}

// EnablePlugin marks a plugin as enabled and makes it loadable by the runtime.
func (r *Registry) EnablePlugin(name string) (*ToggleResult, error) {
	return r.togglePlugin(name, "enabled")
}

// DisablePlugin marks a plugin as disabled without deleting it.
func (r *Registry) DisablePlugin(name string) (*ToggleResult, error) {
	return r.togglePlugin(name, "disabled")
}

func (r *Registry) togglePlugin(name, targetStatus string) (*ToggleResult, error) {
	installed, err := ListInstalledPlugins(r.fs.Paths().Plugins)
	if err != nil {
		return nil, err
	}

	needle := strings.TrimSpace(name)
	for _, plugin := range installed {
		if !strings.EqualFold(plugin.Name, needle) &&
			!strings.EqualFold(pluginDirName(plugin.Name), needle) {
			continue
		}

		if plugin.Status == targetStatus {
			return &ToggleResult{Name: plugin.Name, Path: plugin.Dir, Status: plugin.Status}, nil
		}

		from := filepath.Join(plugin.Dir, manifestFileForStatus(plugin.Status))
		to := filepath.Join(plugin.Dir, manifestFileForStatus(targetStatus))
		if err := os.Rename(from, to); err != nil {
			return nil, err
		}

		return &ToggleResult{Name: plugin.Name, Path: plugin.Dir, Status: targetStatus}, nil
	}

	return nil, fmt.Errorf("plugin %q is not installed", name)
}

func manifestFileForStatus(status string) string {
	if status == "disabled" {
		return "plugin.disabled.json"
	}
	return "plugin.json"
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

	if err := writeInstallMetadata(targetDir, installMetadata{
		SourceType: "official",
		Repo:       strings.TrimSpace(entry.Repo),
	}); err != nil {
		return nil, err
	}

	return &InstallResult{
		Name:    entry.Name,
		Version: entry.Version,
		Path:    targetDir,
		Source:  sourceURL,
		Type:    "official",
	}, nil
}

// InstallCustomPlugin downloads and installs a plugin directly from a GitHub URL.
func (r *Registry) InstallCustomPlugin(ctx context.Context, repoURL string) (*InstallResult, error) {
	repoURL = normalizeGitHubRepo(repoURL)
	if repoURL == "" {
		return nil, errors.New("custom plugin repository URL is required")
	}

	tempDir, err := os.MkdirTemp("", "awan-plugin-custom-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	var archivePath string
	var sourceURL string
	for _, candidate := range archiveCandidates(RegistryPlugin{Repo: repoURL}) {
		archivePath, err = r.downloadFile(ctx, candidate, tempDir)
		if err == nil {
			sourceURL = candidate
			break
		}
	}
	if archivePath == "" {
		return nil, fmt.Errorf("failed to download plugin from %s", repoURL)
	}

	extractRoot := filepath.Join(tempDir, "extract")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		return nil, err
	}
	if err := extractArchive(archivePath, extractRoot); err != nil {
		return nil, err
	}

	pluginSourceDir, manifest, err := findAnyPluginDirectory(extractRoot)
	if err != nil {
		return nil, err
	}

	targetDir := filepath.Join(r.fs.Paths().Plugins, pluginDirName(manifest.Name))
	_ = os.RemoveAll(targetDir)
	if err := copyDir(pluginSourceDir, targetDir); err != nil {
		return nil, err
	}
	if err := writeInstallMetadata(targetDir, installMetadata{
		SourceType: "custom",
		Repo:       repoURL,
	}); err != nil {
		return nil, err
	}

	return &InstallResult{
		Name:    manifest.Name,
		Version: manifest.Version,
		Path:    targetDir,
		Source:  sourceURL,
		Type:    "custom",
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

func findAnyPluginDirectory(root string) (string, Manifest, error) {
	var selectedPath string
	var selectedManifest Manifest

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
		if strings.TrimSpace(manifest.Name) == "" {
			return nil
		}

		selectedPath = filepath.Dir(path)
		selectedManifest = manifest
		return io.EOF
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", Manifest{}, err
	}
	if selectedPath == "" {
		return "", Manifest{}, errors.New("no plugin.json found in custom repository")
	}
	return selectedPath, selectedManifest, nil
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

func writeInstallMetadata(pluginDir string, metadata installMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginDir, ".awan-plugin.json"), data, 0o600)
}

func normalizeGitHubRepo(repo string) string {
	repo = strings.TrimSpace(strings.TrimRight(repo, "/"))
	if repo == "" {
		return ""
	}
	if strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		return strings.TrimSuffix(repo, ".git")
	}
	if strings.Count(repo, "/") == 1 {
		return "https://github.com/" + strings.TrimSuffix(repo, ".git")
	}
	return repo
}
