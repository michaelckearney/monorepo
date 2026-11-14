package backend

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP serves Harbor's MCP server over HTTP, on this computer only, for
// agents outside Harbor, such as Claude Code. A request needs the token.
type MCP struct {
	path string
	port int
	err  error
	http *http.Server

	mu    sync.Mutex
	token string
}

// MCPConnection is how an agent outside Harbor connects.
type MCPConnection struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// mcpFile keeps the port and the token in the data folder, so an agent set
// up once keeps working after Harbor restarts.
const mcpFile = "mcp.json"

func startMCP(server *mcp.Server) *MCP {
	m, err := serveMCP(server, filepath.Join(desktop.DataDir(), mcpFile))
	if err != nil {
		return &MCP{err: err}
	}
	return m
}

func serveMCP(server *mcp.Server, path string) (*MCP, error) {
	var saved struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	m := &MCP{path: path, token: saved.Token}
	if m.token == "" {
		m.token = newMCPToken()
	}
	// The port from last time, unless another program has it now: two
	// copies of Harbor, such as a built one and one under development,
	// each get one.
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", saved.Port))
	if err != nil {
		l, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return nil, err
	}
	m.port = l.Addr().(*net.TCPAddr).Port
	if err := m.save(); err != nil {
		l.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		want := []byte("Bearer " + m.token)
		m.mu.Unlock()
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "Harbor's MCP server needs its token: Authorization: Bearer <token>", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	m.http = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go m.http.Serve(l)
	return m, nil
}

func newMCPToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// save writes the port and the token to the data folder. The caller holds
// mu, or no other goroutine has m yet.
func (m *MCP) save() error {
	data, _ := json.Marshal(struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}{m.port, m.token})
	return os.WriteFile(m.path, data, 0o600)
}

// Connection is how to connect an agent outside Harbor to its MCP server.
func (m *MCP) Connection() (MCPConnection, error) {
	if m.err != nil {
		return MCPConnection{}, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return MCPConnection{URL: fmt.Sprintf("http://127.0.0.1:%d/mcp", m.port), Token: m.token}, nil
}

// NewToken replaces the token, so an agent given the old one can no longer
// connect.
func (m *MCP) NewToken() (MCPConnection, error) {
	if m.err != nil {
		return MCPConnection{}, m.err
	}
	m.mu.Lock()
	old := m.token
	m.token = newMCPToken()
	if err := m.save(); err != nil {
		m.token = old
		m.mu.Unlock()
		return MCPConnection{}, err
	}
	m.mu.Unlock()
	return m.Connection()
}

// ServiceShutdown stops the server when the app quits.
func (m *MCP) ServiceShutdown() error {
	if m.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.http.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
