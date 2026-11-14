package backend

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// Chats are chats with Harbor's agent. The frontend reads them, and follows
// a turn as it runs, with live queries on the chats tables.
type Chats struct {
	agent *chat.Agent
}

const agentIntro = `You are Harbor's agent. You help the user make and fix services and dashboards in Harbor, and use them. Work with the tools; when you are done, reply in a few plain sentences.

`

func newChats(db *sql.DB, server *mcp.Server, model func() (chat.Model, error), confirm func(string, map[string]any) bool) *Chats {
	return &Chats{agent: chat.New(chat.Config{
		DB:           db,
		Tools:        server,
		Instructions: agentIntro + mcpInstructions,
		Confirm:      confirm,
		Model:        model,
	})}
}

// NewChat starts an empty chat, and returns its id.
func (c *Chats) NewChat(ctx context.Context) (string, error) { return c.agent.NewChat(ctx) }

// Send sends your message, and starts the agent's turn. lookingAt is what
// you are looking at, such as `the dashboard "Money"`, or empty.
func (c *Chats) Send(chatID, text, lookingAt string) error {
	return c.agent.Send(chatID, text, lookingAt)
}

// Answer answers the agent's request to run a tool call: allowed runs it.
func (c *Chats) Answer(chatID, callID string, allowed bool) error {
	return c.agent.Answer(chatID, callID, allowed)
}

// Stop stops the agent's turn.
func (c *Chats) Stop(chatID string) { c.agent.Stop(chatID) }

// Delete stops a chat and deletes it.
func (c *Chats) Delete(ctx context.Context, chatID string) error {
	return c.agent.DeleteChat(ctx, chatID)
}

// ServiceShutdown stops every turn when the app quits.
func (c *Chats) ServiceShutdown() error {
	c.agent.Close()
	return nil
}

// confirmer says which of Harbor's MCP tools the agent must ask before it
// calls: the ones that delete, and a service's tools that change data on
// the site. Saving a file needs no asking. Nor does browsing, as in
// Playwright MCP, though a click can change what the user's account holds:
// the instructions (mcp.md) tell the agent to submit nothing but a sign-in.
func confirmer(db *sql.DB) func(tool string, input map[string]any) bool {
	return func(tool string, input map[string]any) bool {
		switch tool {
		case "delete_service", "delete_dashboard":
			return true
		case "call_service_tool":
			return !readOnlyTool(db, input)
		case "test_service_tool":
			// With no tool, it only signs in.
			return input["tool"] != nil && input["tool"] != "" && !readOnlyTool(db, input)
		}
		return false
	}
}

func readOnlyTool(db *sql.DB, input map[string]any) bool {
	name, _ := input["service"].(string)
	toolName, _ := input["tool"].(string)
	var raw string
	if err := db.QueryRow(`SELECT manifest FROM services WHERE name = ?`, name).Scan(&raw); err != nil {
		return false
	}
	var m service.Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	for _, t := range m.Tools {
		if t.Name == toolName {
			return t.Annotations != nil && t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint
		}
	}
	return false
}
