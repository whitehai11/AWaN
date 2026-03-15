package models

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/whitehai11/AWaN/core/auth"
	"github.com/whitehai11/AWaN/core/config"
)

// Model is the abstraction implemented by every model provider.
type Model interface {
	Name() string
	Generate(prompt string) (string, error)
}

// NewOpenAIModel creates a built-in OpenAI client.
func NewOpenAIModel(cfg config.ProviderConfig, oauth *auth.OAuthManager) Model {
	return &openAIModel{
		modelName: cfg.Model,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:    os.Getenv("OPENAI_API_KEY"),
		oauth:     oauth,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// NewOllamaModel creates a built-in Ollama client.
func NewOllamaModel(cfg config.ProviderConfig) Model {
	return &ollamaModel{
		modelName: cfg.Model,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type openAIModel struct {
	modelName  string
	baseURL    string
	apiKey     string
	oauth      *auth.OAuthManager
	httpClient *http.Client
}

func (m *openAIModel) Name() string {
	return "openai"
}

func (m *openAIModel) Generate(prompt string) (string, error) {
	reqBody := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model: m.modelName,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	if err := m.applyAuth(req); err != nil {
		return "", err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai chat completion failed with status %s", resp.Status)
	}

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("openai returned no choices")
	}

	return strings.TrimSpace(payload.Choices[0].Message.Content), nil
}

// StreamGenerate allows future streaming-aware interfaces to reuse the model client.
func (m *openAIModel) StreamGenerate(prompt string, onChunk func(string) error) error {
	reqBody := struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model:  m.modelName,
		Stream: true,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if err := m.applyAuth(req); err != nil {
		return err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("openai stream failed with status %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return err
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := onChunk(choice.Delta.Content); err != nil {
					return err
				}
			}
		}
	}

	return scanner.Err()
}

func (m *openAIModel) applyAuth(req *http.Request) error {
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
		return nil
	}
	if m.oauth != nil {
		token, err := m.oauth.GetAccessToken()
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	return errors.New("missing OpenAI credentials: set OPENAI_API_KEY or configure OAuth")
}

type ollamaModel struct {
	modelName  string
	baseURL    string
	httpClient *http.Client
}

func (m *ollamaModel) Name() string {
	return "ollama"
}

func (m *ollamaModel) Generate(prompt string) (string, error) {
	reqBody := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		Stream bool   `json:"stream"`
	}{
		Model:  m.modelName,
		Prompt: prompt,
		Stream: false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama generate failed with status %s", resp.Status)
	}

	var payload struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	return strings.TrimSpace(payload.Response), nil
}
