package backend

import (
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework"
)

func TestAgentToolsReturnWhatTheAgentGets(t *testing.T) {
	db := desktop.TestDB(t)
	docs := &Documents{db: db, icons: newIcons(db)}
	tools := &AgentTools{server: newMCPServer(docs, &Tools{db: db}), confirm: confirmer(db)}

	list, err := tools.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list {
		names = append(names, tool.Name)
		if tool.Name == "read_dashboard" && !strings.Contains(tool.InputSchema, `"required":["name"]`) {
			t.Errorf("read_dashboard's schema: %s", tool.InputSchema)
		}
	}
	if len(list) != 29 || !strings.Contains(strings.Join(names, " "), "write_dashboard") {
		t.Errorf("tools: %v", names)
	}

	// The marks the Tools page badges with, as MCP reads the annotations:
	// a tool that is not read-only destroys unless it says otherwise.
	for name, want := range map[string]AgentToolAnnotations{
		"list_dashboards":   {ReadOnlyHint: true, DestructiveHint: true},
		"write_dashboard":   {DestructiveHint: false},
		"delete_dashboard":  {DestructiveHint: true},
		"call_service_tool": {DestructiveHint: true},
		"browser_click":     {DestructiveHint: false},
	} {
		for _, tool := range list {
			if tool.Name == name && tool.Annotations != want {
				t.Errorf("%s's annotations: %+v, want %+v", name, tool.Annotations, want)
			}
		}
	}

	for _, tc := range []struct{ tool, input, want string }{
		{"write_dashboard", `{"name":"Inbox","source":"<template><p>hi</p></template>"}`, `{"output":{"replaced":false,"saved":"dashboard Inbox"}}`},
		{"list_dashboards", `{}`, `{"output":{"dashboards":["Inbox"]}}`},
		{"read_dashboard", `{"name":"Nope"}`, `{"error":"there is no dashboard named Nope"}`},
		{"no_such_tool", `{}`, `{"error":"calling \"tools/call\": unknown tool \"no_such_tool\""}`},
	} {
		got, err := tools.Call(t.Context(), tc.tool, tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%s: %s, %v\nwant %s", tc.tool, got, err, tc.want)
		}
	}
	if _, err := tools.Call(t.Context(), "list_dashboards", `[]`); err == nil {
		t.Error("an input that is not an object was sent")
	}

	for tool, want := range map[string]bool{"delete_dashboard": true, "write_dashboard": false, "list_dashboards": false} {
		if asks, err := tools.Asks(tool, `{}`); err != nil || asks != want {
			t.Errorf("asks before %s: %v, %v", tool, asks, err)
		}
	}
}
