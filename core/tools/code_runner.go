package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultCodeTimeout  = 5 * time.Second
	defaultMemoryLimit  = 96
	defaultOutputCutoff = 64 * 1024
)

// CodeExecutionRequest contains the code-execution payload.
type CodeExecutionRequest struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// CodeExecutionResult is returned from the sandbox runner.
type CodeExecutionResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// CodeRunner executes generated code inside the AWaN sandbox directory.
type CodeRunner struct {
	root string
}

// NewCodeRunner creates a sandboxed code runner rooted in ~/.awan/sandbox.
func NewCodeRunner(root string) (*CodeRunner, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}

	return &CodeRunner{root: root}, nil
}

// Execute runs code in an isolated working directory with time and output limits.
func (r *CodeRunner) Execute(ctx context.Context, request CodeExecutionRequest) (*CodeExecutionResult, error) {
	language := strings.ToLower(strings.TrimSpace(request.Language))
	if language == "" {
		return nil, errors.New("language is required")
	}
	if strings.TrimSpace(request.Code) == "" {
		return nil, errors.New("code is required")
	}

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultCodeTimeout)
		defer cancel()
	}

	runDir := filepath.Join(r.root, time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}

	filename, commandName, commandArgs, err := r.prepareExecution(language, runDir, request.Code)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, filename), []byte(request.Code), 0o600); err != nil {
		return nil, err
	}

	command := exec.CommandContext(ctx, commandName, commandArgs...)
	command.Dir = runDir
	command.Env = sandboxEnv(runDir, language)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &limitedBuffer{Buffer: &stdout, limit: defaultOutputCutoff}
	command.Stderr = &limitedBuffer{Buffer: &stderr, limit: defaultOutputCutoff}

	err = command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return &CodeExecutionResult{
			Stdout: stdout.String(),
			Stderr: "execution timed out",
		}, nil
	}

	result := &CodeExecutionResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
	if err != nil && result.Stderr == "" {
		result.Stderr = err.Error()
	}

	return result, nil
}

func (r *CodeRunner) prepareExecution(language, runDir, code string) (string, string, []string, error) {
	switch language {
	case "python", "py":
		filename := "main.py"
		if runtime.GOOS == "windows" {
			return filename, "python", []string{"-I", filename}, nil
		}
		wrapper := "import resource, runpy\nresource.setrlimit(resource.RLIMIT_AS, (100663296, 100663296))\nrunpy.run_path('main.py', run_name='__main__')\n"
		if err := os.WriteFile(filepath.Join(runDir, "_runner.py"), []byte(wrapper), 0o600); err != nil {
			return "", "", nil, err
		}
		return filename, "python", []string{"-I", "_runner.py"}, nil
	case "javascript", "js", "node":
		return "main.js", "node", []string{"--disable-proto=throw", "--no-warnings", "main.js"}, nil
	case "go", "golang":
		return "main.go", "go", []string{"run", "main.go"}, nil
	default:
		return "", "", nil, fmt.Errorf("unsupported language %q", language)
	}
}

func sandboxEnv(runDir, language string) []string {
	keys := []string{"PATH", "SystemRoot", "ComSpec", "PATHEXT", "TMP", "TEMP"}
	env := make([]string, 0, len(keys)+6)

	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}

	env = append(env,
		"HOME="+runDir,
		"USERPROFILE="+runDir,
		"AWAN_SANDBOX_ROOT="+runDir,
		"AWAN_DISABLE_NETWORK=1",
	)

	switch language {
	case "python", "py":
		env = append(env,
			"PYTHONNOUSERSITE=1",
			"PYTHONDONTWRITEBYTECODE=1",
		)
	case "javascript", "js", "node":
		env = append(env,
			fmt.Sprintf("NODE_OPTIONS=--max-old-space-size=%d", defaultMemoryLimit),
			"NODE_PATH=",
		)
	case "go", "golang":
		env = append(env,
			fmt.Sprintf("GOMEMLIMIT=%dMiB", defaultMemoryLimit),
			"GOPROXY=off",
			"GOSUMDB=off",
			"GONOSUMDB=*",
			"GOPATH="+filepath.Join(runDir, "gopath"),
			"GOCACHE="+filepath.Join(runDir, "gocache"),
		)
	}

	return env
}

type limitedBuffer struct {
	Buffer *bytes.Buffer
	limit  int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.Buffer.Len() >= l.limit {
		return len(p), nil
	}

	remaining := l.limit - l.Buffer.Len()
	if len(p) > remaining {
		p = p[:remaining]
	}

	return l.Buffer.Write(p)
}
