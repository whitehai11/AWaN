package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultConfigFile = "awan.config.json"

// Config is the root configuration consumed by the runtime.
type Config struct {
	DefaultModel string         `json:"defaultModel"`
	DefaultAgent string         `json:"defaultAgent"`
	API          APIConfig      `json:"api"`
	Storage      StorageConfig  `json:"storage"`
	OpenAI       ProviderConfig `json:"openai"`
	Ollama       ProviderConfig `json:"ollama"`
	Auth         AuthConfig     `json:"auth"`
}

// APIConfig configures the local runtime server.
type APIConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// StorageConfig configures the isolated runtime home directory.
type StorageConfig struct {
	RootPath string `json:"rootPath"`
}

// ProviderConfig contains model provider settings.
type ProviderConfig struct {
	Model   string `json:"model"`
	BaseURL string `json:"baseURL"`
}

// AuthConfig configures token storage.
type AuthConfig struct {
	StoragePath string `json:"storagePath"`
}

// AgentProfile represents a parsed .awan file.
type AgentProfile struct {
	AgentName string
	Model     string
	Memory    bool
}

// Load reads configuration from awan.config.json or falls back to defaults.
func Load() (*Config, error) {
	return LoadFromPath(defaultConfigFile)
}

// LoadFromPath reads configuration from a specific file path.
func LoadFromPath(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	cfg := defaultConfig()

	data, err := os.ReadFile(absPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	cfg.applyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate ensures the runtime has the minimum required configuration.
func (c *Config) Validate() error {
	if c.DefaultModel == "" {
		return errors.New("defaultModel is required")
	}
	if c.DefaultAgent == "" {
		return errors.New("defaultAgent is required")
	}
	if c.API.Host == "" {
		return errors.New("api.host is required")
	}
	if c.API.Port <= 0 {
		return errors.New("api.port must be greater than zero")
	}
	return nil
}

// Address returns the configured local listen address.
func (c *Config) Address() string {
	return c.API.Host + ":" + strconv.Itoa(c.API.Port)
}

func defaultConfig() *Config {
	return &Config{
		DefaultModel: "openai",
		DefaultAgent: "default",
		API: APIConfig{
			Host: "localhost",
			Port: 7452,
		},
		OpenAI: ProviderConfig{
			Model:   "gpt-4o-mini",
			BaseURL: "https://api.openai.com",
		},
		Ollama: ProviderConfig{
			Model:   "llama3",
			BaseURL: "http://localhost:11434",
		},
	}
}

func (c *Config) applyEnvOverrides() {
	if value := os.Getenv("AWAN_DEFAULT_MODEL"); value != "" {
		c.DefaultModel = value
	}
	if value := os.Getenv("AWAN_DEFAULT_AGENT"); value != "" {
		c.DefaultAgent = value
	}
	if value := os.Getenv("AWAN_HOST"); value != "" {
		c.API.Host = value
	}
	if value := os.Getenv("AWAN_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			c.API.Port = port
		}
	}
	if value := os.Getenv("AWAN_HOME"); value != "" {
		c.Storage.RootPath = value
	}
	if value := os.Getenv("OPENAI_MODEL"); value != "" {
		c.OpenAI.Model = value
	}
	if value := os.Getenv("OPENAI_BASE_URL"); value != "" {
		c.OpenAI.BaseURL = value
	}
	if value := os.Getenv("OLLAMA_MODEL"); value != "" {
		c.Ollama.Model = value
	}
	if value := os.Getenv("OLLAMA_BASE_URL"); value != "" {
		c.Ollama.BaseURL = value
	}
}

// LoadAgentProfile reads and parses a .awan agent profile.
func LoadAgentProfile(path string) (*AgentProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAgentProfile(data)
}

// ParseAgentProfile parses simple key=value .awan content.
func ParseAgentProfile(data []byte) (*AgentProfile, error) {
	profile := &AgentProfile{
		AgentName: "default",
		Model:     "openai",
		Memory:    true,
	}

	lines := strings.Split(string(data), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, errors.New("invalid .awan config line: " + line)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "agent_name":
			profile.AgentName = value
		case "model":
			profile.Model = value
		case "memory":
			profile.Memory = strings.EqualFold(value, "enabled") || strings.EqualFold(value, "true")
		}
	}

	return profile, nil
}
