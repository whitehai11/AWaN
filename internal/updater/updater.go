package updater

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Options configures the shared AWaN updater.
type Options struct {
	AppName        string
	Repo           string
	Version        string
	BinaryBaseName string
	Args           []string
	Logger         func(string)
	ConfigPath     string
}

// StartBackground launches a non-blocking update check.
func StartBackground(options Options) {
	go func() {
		if !autoUpdateEnabled(options.ConfigPath) {
			return
		}

		logf(options, "checking for updates")

		rel, err := latestRelease(options.Repo)
		if err != nil {
			logf(options, "update check failed: "+err.Error())
			return
		}

		if CompareVersions(options.Version, rel.TagName) >= 0 {
			return
		}

		logf(options, "update available "+rel.TagName)

		asset, err := matchingAsset(rel, options.BinaryBaseName)
		if err != nil {
			logf(options, "update asset lookup failed: "+err.Error())
			return
		}

		logf(options, "downloading update")
		updatePath, err := downloadAsset(asset.URL, asset.Name)
		if err != nil {
			logf(options, "update download failed: "+err.Error())
			return
		}

		executable, err := os.Executable()
		if err != nil {
			logf(options, "could not resolve executable: "+err.Error())
			return
		}

		if err := prepareReplacement(updatePath); err != nil {
			logf(options, "update verification failed: "+err.Error())
			return
		}

		logf(options, "restarting")
		if err := launchReplacement(executable, updatePath, options.Args); err != nil {
			logf(options, "restart failed: "+err.Error())
			return
		}

		os.Exit(0)
	}()
}

func downloadAsset(url, name string) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "awan-updater")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed with status %s", resp.Status)
	}

	tempDir := filepath.Join(os.TempDir(), "awan-updater")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return "", err
	}

	target := filepath.Join(tempDir, name)
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

func prepareReplacement(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("downloaded update is empty")
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, 0o755)
	}
	return nil
}

func launchReplacement(target, downloaded string, args []string) error {
	if runtime.GOOS == "windows" {
		return launchWindowsReplacement(target, downloaded, args)
	}
	return launchUnixReplacement(target, downloaded, args)
}

func launchUnixReplacement(target, downloaded string, args []string) error {
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("awan-update-%d.sh", os.Getpid()))
	argLine := quoteArgs(args)
	content := fmt.Sprintf("#!/usr/bin/env bash\nset -eu\nwhile kill -0 %d 2>/dev/null; do sleep 1; done\nmv %q %q\nchmod +x %q\nexec %q %s\n", os.Getpid(), downloaded, target, target, target, argLine)

	if err := os.WriteFile(scriptPath, []byte(content), 0o700); err != nil {
		return err
	}

	cmd := exec.Command("sh", scriptPath)
	return cmd.Start()
}

func launchWindowsReplacement(target, downloaded string, args []string) error {
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("awan-update-%d.cmd", os.Getpid()))
	argLine := quoteArgs(args)
	content := fmt.Sprintf("@echo off\r\n:wait\r\ntasklist /FI \"PID eq %d\" | findstr /I \"%d\" >nul\r\nif %%ERRORLEVEL%%==0 (\r\n  timeout /t 1 >nul\r\n  goto wait\r\n)\r\nmove /Y \"%s\" \"%s\" >nul\r\nstart \"\" \"%s\" %s\r\n", os.Getpid(), os.Getpid(), downloaded, target, target, argLine)

	if err := os.WriteFile(scriptPath, []byte(content), 0o600); err != nil {
		return err
	}

	cmd := exec.Command("cmd", "/C", scriptPath)
	return cmd.Start()
}

func autoUpdateEnabled(configPath string) bool {
	path := strings.TrimSpace(configPath)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return true
		}
		path = filepath.Join(home, ".awan", "config", "runtime.awan")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}

	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "auto_update" {
			return strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}

	return true
}

func quoteArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, strconvQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func logf(options Options, message string) {
	if options.Logger != nil {
		options.Logger(message)
	}
}
