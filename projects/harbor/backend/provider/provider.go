// Package provider reads and runs a provider file: the TypeScript that
// connects Harbor's agent to a language model's API.
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

// Manifest is everything about a provider except its code.
type Manifest struct {
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Settings    []Setting `json:"settings"`
}

// Setting is one thing the user fills in for a provider, such as its API
// key. A setting with no default must be filled in, unless it is optional.
type Setting struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
	Default     string `json:"default,omitempty"`
}

// Provider is a provider file, read and compiled.
type Provider struct {
	Manifest
	program *goja.Program
}

var providerName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// An infinite loop at the top level of a file would otherwise hang Harbor's
// start.
const readTimeout = time.Second

// Load compiles a provider's TypeScript source, runs it once to read its
// manifest, and checks it. The error lists every problem found. The source
// can import from "harbor", and, when shared is not nil, a module in shared
// by a path such as "./shared/responses".
func Load(source string, shared fs.FS) (*Provider, error) {
	js, err := compile(source, shared)
	if err != nil {
		return nil, err
	}
	program, err := goja.Compile("provider.js", js, true)
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	timer := time.AfterFunc(readTimeout, func() { vm.Interrupt("it took longer than a second") })
	defer timer.Stop()
	if _, err := vm.RunProgram(program); err != nil {
		return nil, fmt.Errorf("reading the file failed: %v", err)
	}
	r := reader{}
	m := r.manifest(exported(vm))
	if len(r.problems) > 0 {
		return nil, errors.New(strings.Join(r.problems, "\n"))
	}
	return &Provider{Manifest: m, program: program}, nil
}

// global is the variable a compiled provider sets to its module.
const global = "__harborProvider"

func exported(vm *goja.Runtime) goja.Value {
	if module, ok := vm.Get(global).(*goja.Object); ok {
		return module.Get("default")
	}
	return nil
}

// compile strips the types and bundles the file into a script that sets
// global to the module. ES2017 turns async generators, which goja cannot
// run, into code it can.
func compile(source string, shared fs.FS) (string, error) {
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   source,
			Sourcefile: "provider.ts",
			Loader:     api.LoaderTS,
		},
		Bundle:     true,
		Format:     api.FormatIIFE,
		GlobalName: global,
		Target:     api.ES2017,
		Write:      false,
		LogLevel:   api.LogLevelSilent,
		Plugins:    []api.Plugin{modules(shared)},
	})
	if len(result.Errors) > 0 {
		problems := make([]string, len(result.Errors))
		for i, m := range result.Errors {
			problems[i] = m.Text
			if m.Location != nil {
				problems[i] = fmt.Sprintf("line %d: %s", m.Location.Line, m.Text)
			}
		}
		return "", errors.New(strings.Join(problems, "\n"))
	}
	return string(result.OutputFiles[0].Contents), nil
}

// modules resolves a provider's imports: "harbor", and a relative path to a
// TypeScript module in shared.
func modules(shared fs.FS) api.Plugin {
	return api.Plugin{
		Name: "harbor",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
				switch {
				case a.Path == "harbor":
					return api.OnResolveResult{Path: "harbor", Namespace: "harbor"}, nil
				case shared != nil && strings.HasPrefix(a.Path, "./"):
					// Relative to the importer, as in a folder of files.
					dir := "."
					if a.Namespace == "shared" {
						dir = path.Dir(a.Importer)
					}
					return api.OnResolveResult{Path: path.Join(dir, a.Path) + ".ts", Namespace: "shared"}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf(`a provider can only import from "harbor", not %q`, a.Path)
			})
			b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "harbor"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := `export function defineProvider(provider) { return provider }`
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})
			b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "shared"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
				source, err := fs.ReadFile(shared, a.Path)
				if err != nil {
					return api.OnLoadResult{}, fmt.Errorf("there is no module %s", a.Path)
				}
				contents := string(source)
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderTS}, nil
			})
		},
	}
}

// reader checks the exported object, collecting problems rather than stopping
// at the first.
type reader struct {
	problems []string
}

func (r *reader) problem(path, format string, args ...any) {
	if path != "" {
		format = path + ": " + format
	}
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *reader) manifest(v goja.Value) Manifest {
	obj := r.object("", v, "the file must export a provider: export default defineProvider({ ... })")
	if obj == nil {
		return Manifest{}
	}
	r.only("", obj, "name", "title", "description", "settings", "generate")
	m := Manifest{
		Name:        r.str("name", obj.Get("name"), true),
		Title:       r.str("title", obj.Get("title"), true),
		Description: r.str("description", obj.Get("description"), false),
		Settings:    []Setting{},
	}
	if m.Name != "" && !providerName.MatchString(m.Name) {
		r.problem("name", "use lower-case letters, digits, and hyphens, such as my-provider")
	}
	if settings := obj.Get("settings"); !absent(settings) {
		if s := r.object("settings", settings, "must be an object, with one entry for each setting"); s != nil {
			for _, key := range s.Keys() {
				m.Settings = append(m.Settings, r.setting("settings."+key, key, s.Get(key)))
			}
		}
	}
	if _, ok := goja.AssertFunction(obj.Get("generate")); !ok {
		r.problem("generate", "must be an async generator function: async *generate(request, context) { ... }")
	}
	return m
}

func (r *reader) setting(path, key string, v goja.Value) Setting {
	s := Setting{Key: key}
	obj := r.object(path, v, "must be an object with a title")
	if obj == nil {
		return s
	}
	r.only(path, obj, "title", "description", "secret", "optional", "default")
	s.Title = r.str(path+".title", obj.Get("title"), true)
	s.Description = r.str(path+".description", obj.Get("description"), false)
	s.Default = r.str(path+".default", obj.Get("default"), false)
	s.Secret = r.flag(path+".secret", obj.Get("secret"))
	s.Optional = r.flag(path+".optional", obj.Get("optional"))
	return s
}

func (r *reader) flag(path string, v goja.Value) bool {
	if absent(v) {
		return false
	}
	b, ok := v.Export().(bool)
	if !ok {
		r.problem(path, "must be true or false")
	}
	return b
}

func (r *reader) object(path string, v goja.Value, problem string) *goja.Object {
	obj, ok := v.(*goja.Object)
	if absent(v) || !ok || obj.ClassName() == "Array" || obj.ClassName() == "Function" {
		r.problem(path, "%s", problem)
		return nil
	}
	return obj
}

func (r *reader) only(path string, obj *goja.Object, allowed ...string) {
	for _, k := range obj.Keys() {
		if !slices.Contains(allowed, k) {
			where := k
			if path != "" {
				where = path + "." + k
			}
			r.problem(where, "is not a field Harbor knows; the fields are %s", strings.Join(allowed, ", "))
		}
	}
}

func (r *reader) str(path string, v goja.Value, required bool) string {
	if absent(v) {
		if required {
			r.problem(path, "is required")
		}
		return ""
	}
	s, ok := v.Export().(string)
	if !ok {
		r.problem(path, "must be a string")
		return ""
	}
	if required && strings.TrimSpace(s) == "" {
		r.problem(path, "is required")
	}
	return s
}

func absent(v goja.Value) bool {
	return v == nil || goja.IsUndefined(v) || goja.IsNull(v)
}

// Request is what the agent asks a provider for: the next assistant message.
type Request struct {
	Instructions string    `json:"instructions"`
	Messages     []Message `json:"messages"`
	Tools        []Tool    `json:"tools"`
}

// Message is one message of a chat. A user's message has text; an
// assistant's has text and tool calls; a tool message has one tool result.
type Message struct {
	Role  string `json:"role"` // user, assistant, or tool
	Parts []Part `json:"parts"`
}

// Part is one part of a message: text, a tool call, or a tool result.
type Part struct {
	Text       string      `json:"text,omitempty"`
	ToolCall   *ToolCall   `json:"toolCall,omitempty"`
	ToolResult *ToolResult `json:"toolResult,omitempty"`
}

type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult is a tool's output, or why it has none.
type ToolResult struct {
	ID     string `json:"id"`
	Output any    `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	// Images are the pictures the tool returned, such as a screenshot,
	// for a model that can look at them.
	Images []Image `json:"images,omitempty"`
}

// Image is a picture in a tool's result.
type Image struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"` // base64
}

// Tool is a tool the model can call, as MCP describes it.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Output is one thing generate yields: some text of the reply, a whole tool
// call, or the end, with what the provider says the turn used.
type Output struct {
	Text     string          `json:"text,omitempty"`
	ToolCall *ToolCall       `json:"toolCall,omitempty"`
	Done     bool            `json:"done,omitempty"`
	Usage    json.RawMessage `json:"usage,omitempty"`
}

// Model is a provider with its settings: what the agent runs.
type Model struct {
	Provider *Provider
	Settings map[string]string
	// Client makes the provider's requests; nil is http.DefaultClient.
	Client *http.Client
}
