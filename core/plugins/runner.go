package plugins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/whitehai11/AWaN/core/filesystem"
	"github.com/whitehai11/AWaN/core/utils"
)

const defaultPluginTimeout = 30 * time.Second

var pathArgumentKeys = []string{
	"path",
	"file",
	"filepath",
	"source",
	"source_path",
	"target",
	"target_path",
	"destination",
	"destination_path",
}

// ExecuteResponse is returned by a plugin over stdout.
type ExecuteResponse struct {
	Result any `json:"result"`
}

// Runner loads, validates, and executes external plugins.
type Runner struct {
	fs      *filesystem.AgentFS
	plugins map[string]Definition
	logger  *utils.Logger
}

// NewRunner loads plugins from ~/.awan/plugins.
func NewRunner(fs *filesystem.AgentFS, logger *utils.Logger) (*Runner, error) {
	pluginMap, err := LoadPlugins(fs.Paths().Plugins)
	if err != nil {
		return nil, err
	}

	return &Runner{
		fs:      fs,
		plugins: pluginMap,
		logger:  logger,
	}, nil
}

// RegisteredPlugins returns a copy of the loaded tool-to-plugin definitions.
func (r *Runner) RegisteredPlugins() map[string]Definition {
	result := make(map[string]Definition, len(r.plugins))
	for name, definition := range r.plugins {
		result[name] = definition
	}
	return result
}

// Execute runs a permitted external plugin process via APP/1.0.
func (r *Runner) Execute(ctx context.Context, allowedPlugins []string, toolName string, args map[string]any) (*ExecuteResponse, error) {
	definition, ok := r.plugins[toolName]
	if !ok {
		return nil, fmt.Errorf("plugin tool %q is not registered", toolName)
	}

	if !isPluginAllowed(allowedPlugins, toolName) && !isPluginAllowed(allowedPlugins, definition.Manifest.Name) {
		return nil, fmt.Errorf("plugin tool %q is not permitted for this agent", toolName)
	}

	if err := validatePluginPermissions(definition.Manifest, allowedPlugins); err != nil {
		return nil, err
	}

	if err := validatePluginArgs(args, r.fs.Paths().Root, r.fs.Paths().Files, r.fs.Paths().Memory); err != nil {
		return nil, err
	}

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultPluginTimeout)
		defer cancel()
	}

	command, err := prepareCommand(ctx, definition)
	if err != nil {
		return nil, err
	}
	command.Dir = definition.Dir
	command.Env = append(os.Environ(),
		"AWAN_ROOT="+r.fs.Paths().Root,
		"AWAN_FILES_ROOT="+r.fs.Paths().Files,
		"AWAN_MEMORY_ROOT="+r.fs.Paths().Memory,
		"AWAN_PLUGIN_ROOT="+definition.Dir,
		"AWAN_PLUGIN_SANDBOX=1",
	)

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}

	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return nil, err
	}

	client := newProtocolClient(definition.Manifest.Name, stdin, stdout, r.logger)
	defer func() { _ = client.Close() }()

	if err := client.Handshake(ctx, toolName); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}

	response, callErr := client.CallTool(ctx, "req-1", toolName, args)
	_ = client.Close()

	if err := command.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("plugin %q failed: %s", definition.Manifest.Name, message)
	}

	if callErr != nil {
		return nil, callErr
	}

	return response, nil
}

func prepareCommand(ctx context.Context, definition Definition) (*exec.Cmd, error) {
	switch filepath.Ext(definition.Entry) {
	case ".js":
		return exec.CommandContext(ctx, "node", definition.Entry), nil
	case ".ts":
		if _, err := exec.LookPath("tsx"); err == nil {
			return exec.CommandContext(ctx, "tsx", definition.Entry), nil
		}
		return exec.CommandContext(ctx, "node", definition.Entry), nil
	case ".py":
		return exec.CommandContext(ctx, "python", definition.Entry), nil
	case ".sh":
		return exec.CommandContext(ctx, "sh", definition.Entry), nil
	case ".bat", ".cmd":
		return exec.CommandContext(ctx, "cmd", "/C", definition.Entry), nil
	case ".exe":
		return exec.CommandContext(ctx, definition.Entry), nil
	default:
		if _, err := os.Stat(definition.Entry); err == nil {
			return exec.CommandContext(ctx, definition.Entry), nil
		}
		return nil, fmt.Errorf("unsupported plugin entry %q", definition.Entry)
	}
}

func isPluginAllowed(allowedPlugins []string, pluginName string) bool {
	if slices.Contains(allowedPlugins, pluginName) {
		return true
	}

	prefix, _, ok := strings.Cut(pluginName, ".")
	return ok && slices.Contains(allowedPlugins, prefix)
}

func validatePluginPermissions(manifest Manifest, allowedPlugins []string) error {
	required := manifest.Permissions
	if len(required) == 0 {
		required = manifest.Tools
	}

	for _, permission := range required {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		if !isPluginAllowed(allowedPlugins, permission) {
			return fmt.Errorf("plugin %q requires permission %q, which is not granted to this agent", manifest.Name, permission)
		}
	}
	return nil
}

func validatePluginArgs(args map[string]any, root, filesRoot, memoryRoot string) error {
	for key, value := range args {
		switch typed := value.(type) {
		case map[string]any:
			if err := validatePluginArgs(typed, root, filesRoot, memoryRoot); err != nil {
				return err
			}
			continue
		case []any:
			for _, item := range typed {
				nested, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if err := validatePluginArgs(nested, root, filesRoot, memoryRoot); err != nil {
					return err
				}
			}
			continue
		}

		if !isPathArgument(key) {
			continue
		}

		pathValue, ok := value.(string)
		if !ok {
			return errors.New("plugin path arguments must be strings")
		}

		if _, err := resolveAllowedPath(pathValue, root, filesRoot, memoryRoot); err != nil {
			return err
		}
	}

	return nil
}

func isPathArgument(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, candidate := range pathArgumentKeys {
		if normalized == candidate {
			return true
		}
	}
	return false
}

func resolveAllowedPath(pathValue, root, filesRoot, memoryRoot string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(pathValue))
	if cleaned == "." || cleaned == "" {
		return "", errors.New("plugin path cannot be empty")
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", errors.New("plugin path escapes the AWaN sandbox")
	}

	lowered := filepath.ToSlash(cleaned)
	targetRoot := filesRoot
	trimmed := cleaned
	switch {
	case lowered == "files":
		targetRoot = filesRoot
		trimmed = "."
	case strings.HasPrefix(lowered, "files/"):
		targetRoot = filesRoot
		trimmed = strings.TrimPrefix(lowered, "files/")
	case lowered == "memory":
		targetRoot = memoryRoot
		trimmed = "."
	case strings.HasPrefix(lowered, "memory/"):
		targetRoot = memoryRoot
		trimmed = strings.TrimPrefix(lowered, "memory/")
	}

	resolved := filepath.Join(targetRoot, filepath.Clean(trimmed))
	relative, err := filepath.Rel(targetRoot, resolved)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(relative, "..") {
		return "", errors.New("plugin path escapes the AWaN sandbox")
	}

	rootRelative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rootRelative, "..") {
		return "", errors.New("plugin path escapes the AWaN sandbox")
	}

	return resolved, nil
}
