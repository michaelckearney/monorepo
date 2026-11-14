// Package backend is Harbor's Go half.
package backend

import (
	"context"
	"database/sql"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/benchmark"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/chat"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// Services lists what the frontend can call. Each one appears in the frontend
// as an import from "@backend".
func Services() []desktop.Service {
	db := desktop.DB()
	var services []desktop.Service
	// The fixture sites, in a dev build, before the browsers and clients
	// that reach them.
	if desktop.Mode() == "dev" {
		sites, err := StartFixtures()
		if err != nil {
			panic("starting the fixture sites: " + err.Error())
		}
		services = append(services, desktop.NewService(&Fixtures{sites: sites}))
	}
	h := newHarbor(options{
		db:      db,
		dir:     desktop.DataDir(),
		window:  func(agent bool) engine.Window { return window{db: db, agent: agent} },
		confirm: confirmer(db),
	})
	h.icons.start()
	// One MCP server, for Harbor's agent and for agents outside Harbor.
	mcpHTTP := startMCP(h.server)
	services = append(services,
		desktop.NewService(h.documents),
		desktop.NewService(h.tools),
		desktop.NewService(mcpHTTP),
		desktop.NewService(h.providers),
		desktop.NewService(h.chats),
		desktop.NewService(&AgentTools{server: h.server, confirm: confirmer(db)}),
		desktop.NewService(&Preferences{db: db}),
		desktop.NewService(h.icons),
	)
	// The developer bar's Benchmarks page; only a dev build has its tables.
	if desktop.Mode() == "dev" {
		b, err := newBenchmarks(db, benchmarksDir(), h.providers, openTrial)
		if err != nil {
			panic("ending the benchmark runs that Harbor quit during: " + err.Error())
		}
		services = append(services, desktop.NewService(b))
	}
	return services
}

// options are what a harbor is put together from.
type options struct {
	db *sql.DB
	// dir is the data folder: both browsers keep their profiles in it.
	dir string
	// window shows a browser's live views; agent is set for the agent's
	// browser.
	window func(agent bool) engine.Window
	// confirm says which of the MCP server's tool calls the agent asks the
	// user about first.
	confirm func(tool string, input map[string]any) bool
	// model gives the agent's model for a turn. Nil is the provider set up
	// in Providers.
	model func() (chat.Model, error)
}

// harbor is Harbor's backend on one database and data folder: what
// Services serves, and what a benchmark's trial runs on.
type harbor struct {
	documents *Documents
	tools     *Tools
	providers *Providers
	chats     *Chats
	// server is Harbor's MCP server, which the agent uses.
	server *mcp.Server
	icons  *Icons
}

// newHarbor puts Harbor's backend together. Chromium starts with the first
// tool call, and the agent with the first message.
func newHarbor(o options) *harbor {
	if err := readManifests(o.db); err != nil {
		panic("reading the services' manifests: " + err.Error())
	}
	h := &harbor{icons: newIcons(o.db)}
	h.documents = &Documents{db: o.db, icons: h.icons}
	h.tools = newTools(o.db, o.dir, o.window)
	h.server = newMCPServer(h.documents, h.tools)
	h.providers = newProviders(o.db)
	model := o.model
	if model == nil {
		model = h.providers.model
	}
	h.chats = newChats(o.db, h.server, model, o.confirm)
	return h
}

// openTrial is a benchmark trial's Harbor. The user allows every tool
// call, and prompted is told the ones Harbor would have asked about.
func openTrial(db *sql.DB, dir string, window func(agent bool) engine.Window, prompted func(tool string, input map[string]any), agent chat.Model) benchmark.Harbor {
	asks := confirmer(db)
	return newHarbor(options{
		db:     db,
		dir:    dir,
		window: window,
		confirm: func(tool string, input map[string]any) bool {
			if asks(tool, input) {
				prompted(tool, input)
			}
			return false
		},
		model: func() (chat.Model, error) { return agent, nil },
	})
}

func (h *harbor) SaveService(source string) error {
	_, err := h.documents.SaveService(0, source)
	return err
}

func (h *harbor) SaveDashboard(name, source string) error {
	_, err := h.documents.SaveDashboard(0, name, source)
	return err
}

func (h *harbor) SignIn(ctx context.Context, service string) (bool, error) {
	return h.tools.SignIn(ctx, service, "")
}

func (h *harbor) NewChat(ctx context.Context) (string, error) { return h.chats.NewChat(ctx) }
func (h *harbor) Send(chatID, text string) error              { return h.chats.Send(chatID, text, "") }
func (h *harbor) Stop(chatID string)                          { h.chats.Stop(chatID) }

// Engine is the engine of the user's browser, or of the agent's.
func (h *harbor) Engine(agent bool) *engine.Engine {
	if agent {
		return h.tools.agent()
	}
	e, _ := h.tools.start()
	return e
}

// Close stops the agent's turns and both browsers.
func (h *harbor) Close() {
	h.chats.ServiceShutdown()
	h.tools.ServiceShutdown()
}

// browserDir is where a browser keeps its profile in the data folder: the
// user's where Slipway keeps the app's by default.
func browserDir(dir string, agent bool) string {
	if agent {
		return filepath.Join(dir, "agent-browser")
	}
	return filepath.Join(dir, "browser")
}
