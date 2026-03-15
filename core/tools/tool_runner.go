package tools

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

	"github.com/awan/awan/core/filesystem"
)

const defaultToolTimeout = 30 * time.Second

// Manifest describes a tool plugin loaded from tool.json.
type Manifest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  map[string]string `json:"parameters"`
}

// Definition represents a discovered external tool.
type Definition struct {
	Manifest Manifest
	Dir      string
	Entry    string
}

// ExecuteRequest is sent to a tool over stdin.
type ExecuteRequest struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// ExecuteResponse is returned by a tool over stdout.
type ExecuteResponse struct {
	Result any `json:"result"`
}

// Runner loads, validates, and executes tool plugins.
type Runner struct {
	fs         *filesystem.AgentFS
	tools      map[string]Definition
	codeRunner *CodeRunner
}

// NewRunner loads all tool definitions from ~/.awan/tools.
func NewRunner(fs *filesystem.AgentFS) (*Runner, error) {
	toolMap, err := LoadTools(fs.Paths().Tools)
	if err != nil {
		return nil, err
	}

	codeRunner, err := NewCodeRunner(fs.Paths().Sandbox)
	if err != nil {
		return nil, err
	}

	toolMap["code.execute"] = Definition{
		Manifest: Manifest{
			Name:        "code.execute",
			Description: "Execute generated code inside the AWaN sandbox",
			Parameters: map[string]string{
				"language": "string",
				"code":     "string",
			},
		},
		Dir:   fs.Paths().Sandbox,
		Entry: "builtin:code.execute",
	}

	return &Runner{
		fs:         fs,
		tools:      toolMap,
		codeRunner: codeRunner,
	}, nil
}

// LoadTools scans tool directories and loads their manifests.
func LoadTools(root string) (map[string]Definition, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	tools := make(map[string]Definition)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		toolDir := filepath.Join(root, entry.Name())
		definition, err := loadToolDefinition(toolDir)
		if err != nil {
			return nil, err
		}

		tools[definition.Manifest.Name] = definition
	}

	return tools, nil
}

// RegisteredTools returns a copy of the loaded tool definitions.
func (r *Runner) RegisteredTools() map[string]Definition {
	result := make(map[string]Definition, len(r.tools))
	for name, definition := range r.tools {
		result[name] = definition
	}
	return result
}

// Execute runs a permitted tool as an isolated process.
func (r *Runner) Execute(ctx context.Context, allowedTools []string, toolName string, args map[string]any) (*ExecuteResponse, error) {
	definition, ok := r.tools[toolName]
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", toolName)
	}

	if !isToolAllowed(allowedTools, toolName) {
		return nil, fmt.Errorf("tool %q is not permitted for this agent", toolName)
	}

	if err := validateToolArgs(args, r.fs.Paths().Files); err != nil {
		return nil, err
	}

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultToolTimeout)
		defer cancel()
	}

	if definition.Entry == "builtin:code.execute" {
		language, _ := args["language"].(string)
		code, _ := args["code"].(string)
		result, err := r.codeRunner.Execute(ctx, CodeExecutionRequest{
			Language: language,
			Code:     code,
		})
		if err != nil {
			return nil, err
		}
		return &ExecuteResponse{
			Result: result,
		}, nil
	}

	command, err := prepareCommand(ctx, definition)
	if err != nil {
		return nil, err
	}
	command.Dir = definition.Dir
	command.Env = append(os.Environ(),
		"AWAN_FILES_ROOT="+r.fs.Paths().Files,
		"AWAN_TOOL_SANDBOX=1",
	)

	request := ExecuteRequest{
		Tool: toolName,
		Args: args,
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
		return nil, fmt.Errorf("tool %q failed: %s", toolName, message)
	}

	var response ExecuteResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("tool %q returned invalid JSON: %w", toolName, err)
	}

	return &response, nil
}

func loadToolDefinition(toolDir string) (Definition, error) {
	manifestPath := filepath.Join(toolDir, "tool.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Definition{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Definition{}, err
	}
	if manifest.Name == "" {
		return Definition{}, errors.New("tool manifest is missing name")
	}

	entry, err := resolveEntry(toolDir)
	if err != nil {
		return Definition{}, err
	}

	return Definition{
		Manifest: manifest,
		Dir:      toolDir,
		Entry:    entry,
	}, nil
}

func resolveEntry(toolDir string) (string, error) {
	for _, candidate := range []string{"main.js", "main.ts"} {
		path := filepath.Join(toolDir, candidate)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", fmt.Errorf("tool in %q is missing main.ts or main.js", toolDir)
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
	default:
		return nil, fmt.Errorf("unsupported tool entry %q", definition.Entry)
	}
}

func isToolAllowed(allowedTools []string, toolName string) bool {
	if slices.Contains(allowedTools, toolName) {
		return true
	}

	prefix, _, ok := strings.Cut(toolName, ".")
	return ok && slices.Contains(allowedTools, prefix)
}

func validateToolArgs(args map[string]any, filesRoot string) error {
	for key, value := range args {
		if !strings.EqualFold(key, "path") {
			continue
		}

		pathValue, ok := value.(string)
		if !ok {
			return errors.New("tool path arguments must be strings")
		}

		cleaned := filepath.Clean(pathValue)
		if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			return errors.New("tool path escapes the agent filesystem")
		}

		resolved := filepath.Join(filesRoot, cleaned)
		relative, err := filepath.Rel(filesRoot, resolved)
		if err != nil {
			return err
		}
		if strings.HasPrefix(relative, "..") {
			return errors.New("tool path escapes the agent filesystem")
		}
	}

	return nil
}
