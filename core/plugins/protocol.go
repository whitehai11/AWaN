package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/whitehai11/AWaN/core/utils"
)

const ProtocolVersion = "APP/1.0"

type protocolMessage struct {
	Type     string         `json:"type"`
	Protocol string         `json:"protocol,omitempty"`
	ID       string         `json:"id,omitempty"`
	Tool     string         `json:"tool,omitempty"`
	Args     map[string]any `json:"args,omitempty"`
	Tools    []string       `json:"tools,omitempty"`
	Result   any            `json:"result,omitempty"`
	Message  string         `json:"message,omitempty"`
}

type protocolClient struct {
	pluginName    string
	logger        *utils.Logger
	encoder       *json.Encoder
	decoder       *json.Decoder
	stdin         io.Closer
	messages      chan protocolMessage
	readErr       chan error
	mu            sync.Mutex
	ready         bool
	capabilities  []string
}

func newProtocolClient(pluginName string, stdin io.WriteCloser, stdout io.Reader, logger *utils.Logger) *protocolClient {
	client := &protocolClient{
		pluginName: pluginName,
		logger:     logger,
		encoder:    json.NewEncoder(stdin),
		decoder:    json.NewDecoder(stdout),
		stdin:      stdin,
		messages:   make(chan protocolMessage),
		readErr:    make(chan error, 1),
	}

	go client.readLoop()

	return client
}

func (c *protocolClient) Handshake(ctx context.Context, toolName string) error {
	if err := c.send(protocolMessage{
		Type:     "init",
		Protocol: ProtocolVersion,
	}); err != nil {
		return err
	}

	readySeen := false
	capabilitiesSeen := false

	for !(readySeen && capabilitiesSeen) {
		msg, err := c.nextMessage(ctx)
		if err != nil {
			return err
		}

		switch msg.Type {
		case "ready":
			readySeen = true
			c.mu.Lock()
			c.ready = true
			c.mu.Unlock()
		case "capabilities":
			capabilitiesSeen = true
			c.mu.Lock()
			c.capabilities = append([]string(nil), msg.Tools...)
			c.mu.Unlock()
		case "log":
			c.log(msg.Message)
		case "error":
			return fmt.Errorf("plugin %q failed during init: %s", c.pluginName, msg.Message)
		default:
			return fmt.Errorf("plugin %q returned unexpected APP message %q during init", c.pluginName, msg.Type)
		}
	}

	if !containsTool(c.capabilitiesList(), toolName) {
		return fmt.Errorf("plugin %q does not declare capability %q", c.pluginName, toolName)
	}

	return nil
}

func (c *protocolClient) CallTool(ctx context.Context, requestID, toolName string, args map[string]any) (*ExecuteResponse, error) {
	if !c.isReady() {
		return nil, errors.New("plugin protocol handshake has not completed")
	}

	if err := c.send(protocolMessage{
		Type: "call",
		ID:   requestID,
		Tool: toolName,
		Args: args,
	}); err != nil {
		return nil, err
	}

	for {
		msg, err := c.nextMessage(ctx)
		if err != nil {
			return nil, err
		}

		switch msg.Type {
		case "log":
			c.log(msg.Message)
		case "result":
			if msg.ID != requestID {
				continue
			}
			return &ExecuteResponse{Result: msg.Result}, nil
		case "error":
			if msg.ID != "" && msg.ID != requestID {
				continue
			}
			if strings.TrimSpace(msg.Message) == "" {
				msg.Message = "unknown plugin error"
			}
			return nil, fmt.Errorf("plugin %q failed: %s", c.pluginName, msg.Message)
		default:
			return nil, fmt.Errorf("plugin %q returned unexpected APP message %q", c.pluginName, msg.Type)
		}
	}
}

func (c *protocolClient) Close() error {
	if c.stdin == nil {
		return nil
	}
	return c.stdin.Close()
}

func (c *protocolClient) capabilitiesList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.capabilities...)
}

func (c *protocolClient) isReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

func (c *protocolClient) send(message protocolMessage) error {
	return c.encoder.Encode(message)
}

func (c *protocolClient) nextMessage(ctx context.Context) (protocolMessage, error) {
	select {
	case <-ctx.Done():
		return protocolMessage{}, ctx.Err()
	case err := <-c.readErr:
		if err == nil {
			return protocolMessage{}, io.EOF
		}
		return protocolMessage{}, err
	case message := <-c.messages:
		return message, nil
	}
}

func (c *protocolClient) readLoop() {
	defer close(c.messages)

	for {
		var message protocolMessage
		if err := c.decoder.Decode(&message); err != nil {
			if errors.Is(err, io.EOF) {
				c.readErr <- nil
				return
			}
			c.readErr <- err
			return
		}
		c.messages <- message
	}
}

func (c *protocolClient) log(message string) {
	if c.logger == nil || strings.TrimSpace(message) == "" {
		return
	}
	c.logger.Log("PLUGIN", fmt.Sprintf("%s: %s", c.pluginName, strings.TrimSpace(message)))
}

func containsTool(tools []string, toolName string) bool {
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool), strings.TrimSpace(toolName)) {
			return true
		}
	}
	return false
}
