package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
)

// AgentTools lets a developer try the tools Harbor's agent has. It reaches
// the agent's own MCP server, the way the agent does, so a call returns what
// the agent would be given.
type AgentTools struct {
	server  *mcp.Server
	confirm func(tool string, input map[string]any) bool

	once    sync.Once
	session *mcp.ClientSession
	err     error
}

// AgentTool is one of the agent's tools, as the agent sees it.
type AgentTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// InputSchema is a JSON schema, as JSON text.
	InputSchema string `json:"inputSchema"`
}

func (t *AgentTools) connect() (*mcp.ClientSession, error) {
	t.once.Do(func() {
		client, server := mcp.NewInMemoryTransports()
		if _, t.err = t.server.Connect(context.Background(), server, nil); t.err != nil {
			return
		}
		t.session, t.err = mcp.NewClient(&mcp.Implementation{Name: "harbor-developer"}, nil).Connect(context.Background(), client, nil)
	})
	if t.err != nil {
		return nil, fmt.Errorf("could not connect to Harbor's tools: %w", t.err)
	}
	return t.session, nil
}

// List returns the agent's tools.
func (t *AgentTools) List(ctx context.Context) ([]AgentTool, error) {
	session, err := t.connect()
	if err != nil {
		return nil, err
	}
	tools := []AgentTool{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		tools = append(tools, AgentTool{Name: tool.Name, Description: tool.Description, InputSchema: string(schema)})
	}
	return tools, nil
}

// Asks reports whether the agent would ask before it made this call. input
// is a JSON object.
func (t *AgentTools) Asks(name, input string) (bool, error) {
	var in map[string]any
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return false, fmt.Errorf("the input is not a JSON object: %w", err)
	}
	return t.confirm(name, in), nil
}

// Call runs one of the agent's tools with input, a JSON object, and returns
// the result as the model is given it: {"output": ...} or {"error": ...}.
func (t *AgentTools) Call(ctx context.Context, name, input string) (string, error) {
	var in map[string]any
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("the input is not a JSON object: %w", err)
	}
	session, err := t.connect()
	if err != nil {
		return "", err
	}
	result := map[string]any{}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
	if err != nil {
		// The agent gives the model a failed call's error, too.
		result["error"] = err.Error()
	} else if r := chat.ToolResult("", res); r.Error != "" {
		result["error"] = r.Error
	} else {
		result["output"] = r.Output
	}
	out, err := json.Marshal(result)
	return string(out), err
}
