package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/whitehai11/AWaN/core/filesystem"
	"github.com/whitehai11/AWaN/core/tools"
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

// ExecuteRequest is sent to a plugin over stdin.
type ExecuteRequest struct {
	Plugin string         `json:"plugin"`
	Args   map[string]any `json:"args"`
}

// ExecuteResponse is returned by a plugin over stdout.
type ExecuteResponse struct {
	Result any `json:"result"`
}

// Runner loads, validates, and executes external plugins.
type Runner struct {
	fs         *filesystem.AgentFS
	plugins    map[string]Definition
	codeRunner *tools.CodeRunner
}

// NewRunner loads plugins from ~/.awan/plugins and registers built-in helpers.
func NewRunner(fs *filesystem.AgentFS) (*Runner, error) {
	pluginMap, err := LoadPlugins(fs.Paths().Plugins)
	if err != nil {
		return nil, err
	}

	codeRunner, err := tools.NewCodeRunner(fs.Paths().Sandbox)
	if err != nil {
		return nil, err
	}

	pluginMap["code.execute"] = Definition{
		Manifest: Manifest{
			Name:        "code.execute",
			Version:     "builtin",
			Description: "Execute generated code inside the AWaN sandbox",
			Parameters: map[string]string{
				"language": "string",
				"code":     "string",
			},
			Permissions: []string{"code.execute"},
			Entry: "builtin:code.execute",
		},
		Dir:   fs.Paths().Sandbox,
		Entry: "builtin:code.execute",
	}

	return &Runner{
		fs:         fs,
		plugins:    pluginMap,
		codeRunner: codeRunner,
	}, nil
}

// RegisteredPlugins returns a copy of the loaded plugin definitions.
func (r *Runner) RegisteredPlugins() map[string]Definition {
	result := make(map[string]Definition, len(r.plugins))
	for name, definition := range r.plugins {
		result[name] = definition
	}
	return result
}

// Execute runs a permitted plugin as an isolated process.
func (r *Runner) Execute(ctx context.Context, allowedPlugins []string, pluginName string, args map[string]any) (*ExecuteResponse, error) {
	definition, ok := r.plugins[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin %q is not registered", pluginName)
	}

	if !isPluginAllowed(allowedPlugins, pluginName) {
		return nil, fmt.Errorf("plugin %q is not permitted for this agent", pluginName)
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

	if definition.Entry == "builtin:code.execute" {
		language, _ := args["language"].(string)
		code, _ := args["code"].(string)
		result, err := r.codeRunner.Execute(ctx, tools.CodeExecutionRequest{
			Language: language,
			Code:     code,
		})
		if err != nil {
			return nil, err
		}
		return &ExecuteResponse{Result: result}, nil
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

	request := ExecuteRequest{
		Plugin: pluginName,
		Args:   args,
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return nil, err
	}

	if _, err := io.Copy(stdin, bytes.NewReader(data)); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	_ = stdin.Close()

	if err := command.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("plugin %q failed: %s", pluginName, message)
	}

	var response ExecuteResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("plugin %q returned invalid JSON: %w", pluginName, err)
	}

	return &response, nil
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
	for _, permission := range manifest.Permissions {
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
