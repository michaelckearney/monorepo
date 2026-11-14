// Package service reads a service file: the TypeScript that connects Harbor
// to one website.
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

// Manifest is what Harbor reads from a service file: everything about the
// service except its code. It is stored beside the source, so listing and
// describing services never runs the file.
//
// A tool's fields are MCP's tool definition, so a manifest's tools can be
// served over MCP as they are.
type Manifest struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Login       *Login `json:"login,omitempty"`
	Tools       []Tool `json:"tools"`
}

// Login says where to sign in: to the service, or, for a tool, again before
// it runs, as a bank asks for your password again before a transfer. The
// file also has a check function, which runs in the page, and may have a
// run function, which signs in on its own (Run is set).
type Login struct {
	URL string `json:"url"`
	Run bool   `json:"run,omitempty"`
}

// Tool is one MCP tool, without its run function.
type Tool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description"`
	Annotations  *Annotations    `json:"annotations,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	// Login is what the tool needs beyond the service's sign-in. It is
	// Harbor's own field, not MCP's.
	Login *Login `json:"login,omitempty"`
	// Frame, Harbor's own too, is the frame the tool runs in, when what it
	// reads is in a frame from another site, which the page cannot read.
	Frame *Frame `json:"frame,omitempty"`
}

// Frame is a frame from another site in one of the service's pages. The
// tool runs in the frame from Site, such as
// https://gradebook.localhost:8443, on the page at Page, a path on the
// service's site. The file may give the page as a function of the tool's
// input instead (PageFromInput is set).
type Frame struct {
	Site          string `json:"site"`
	Page          string `json:"page,omitempty"`
	PageFromInput bool   `json:"pageFromInput,omitempty"`
}

// Annotations are MCP's hints about what a tool does. A tool without
// readOnlyHint is taken to change data, and to be destructive unless
// destructiveHint is false.
type Annotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

var (
	serviceName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	toolName    = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
)

// An infinite loop at the top level of a file would otherwise hang the save.
// A variable so the test of that waits a moment, not a second.
var readTimeout = time.Second

// Read reads the manifest from a service's TypeScript source. It compiles
// the file, runs it with no page and no network, and checks the object it
// exports. The error lists every problem found.
func Read(source string) (Manifest, error) {
	js, err := Compile(source)
	if err != nil {
		return Manifest{}, err
	}
	vm := goja.New()
	timer := time.AfterFunc(readTimeout, func() { vm.Interrupt("it took longer than a second") })
	defer timer.Stop()
	if _, err := vm.RunString(js); err != nil {
		return Manifest{}, runError(err)
	}
	// A file with no exports leaves __service unset.
	var exported goja.Value
	if module, ok := vm.Get(Global).(*goja.Object); ok {
		exported = module.Get("default")
	}
	r := reader{vm: vm}
	m := r.manifest(exported)
	if len(r.problems) > 0 {
		return Manifest{}, errors.New(strings.Join(r.problems, "\n"))
	}
	return m, nil
}

// Global is the variable a compiled service sets to its module. The
// service's object is its default export: Global.default.
const Global = "__harborService"

// Compile strips the types and bundles the file into a script that sets
// Global to the module. The only import allowed is "harbor".
func Compile(source string) (string, error) {
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   source,
			Sourcefile: "service.ts",
			Loader:     api.LoaderTS,
		},
		Bundle:     true,
		Format:     api.FormatIIFE,
		GlobalName: Global,
		Target:     api.ES2017,
		Write:      false,
		LogLevel:   api.LogLevelSilent,
		Plugins:    []api.Plugin{harborModule},
	})
	if len(result.Errors) > 0 {
		return "", errors.New(strings.Join(messages(result.Errors), "\n"))
	}
	return string(result.OutputFiles[0].Contents), nil
}

var harborModule = api.Plugin{
	Name: "harbor",
	Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			if a.Path != "harbor" {
				return api.OnResolveResult{}, fmt.Errorf(`a service can only import from "harbor", not %q`, a.Path)
			}
			return api.OnResolveResult{Path: "harbor", Namespace: "harbor"}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "harbor"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
			contents := harborJS
			return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
		})
	},
}

// harborJS is the "harbor" module a service imports. fill asks Harbor to
// type a saved username, password, or code into a field (engine/saved.go),
// so the value is never in the file; it works only in login.run.
const harborJS = `
export function defineService(service) { return service }

export function fill(field, what) {
  if (typeof HTMLElement === "undefined" || !(field instanceof HTMLElement)) {
    return Promise.reject(new Error("fill takes a field on the page, such as document.querySelector('input[name=password]')"))
  }
  if (["username", "password", "code"].indexOf(what) < 0) {
    return Promise.reject(new Error('fill fills in "username", "password", or "code", not ' + JSON.stringify(what)))
  }
  field.focus()
  if (typeof field.select === "function") field.select()
  return harborFill({ what }).then(() => {})
}
`

func messages(ms []api.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Text
		if m.Location != nil {
			out[i] = fmt.Sprintf("line %d: %s", m.Location.Line, m.Text)
		}
	}
	return out
}

// runError explains an error thrown while the file was read. The usual cause
// is code outside a function using the page, which is not there yet.
func runError(err error) error {
	var exc *goja.Exception
	if errors.As(err, &exc) && strings.Contains(exc.Value().String(), "ReferenceError") {
		return fmt.Errorf("%s. Code outside a function runs when Harbor reads the file, where there is no page: move it into a function", exc.Value())
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return fmt.Errorf("reading the file stopped: %v", interrupted.Value())
	}
	return fmt.Errorf("reading the file failed: %v", err)
}

// reader checks the exported object, collecting problems rather than stopping
// at the first.
type reader struct {
	vm       *goja.Runtime
	problems []string
}

func (r *reader) problem(path, format string, args ...any) {
	if path == "" {
		r.problems = append(r.problems, fmt.Sprintf(format, args...))
		return
	}
	r.problems = append(r.problems, path+": "+fmt.Sprintf(format, args...))
}

func (r *reader) manifest(v goja.Value) Manifest {
	obj := r.object("", v, "the file must export a service: export default defineService({ ... })")
	if obj == nil {
		return Manifest{}
	}
	r.only("", obj, "name", "title", "description", "url", "login", "tools")
	m := Manifest{
		Name:        r.str("name", obj.Get("name"), true),
		Title:       r.str("title", obj.Get("title"), true),
		Description: r.str("description", obj.Get("description"), false),
		URL:         r.str("url", obj.Get("url"), true),
	}
	if m.Name != "" && !serviceName.MatchString(m.Name) {
		r.problem("name", "use lower-case letters, digits, and hyphens, such as my-service")
	}
	if m.URL != "" {
		if u, err := url.Parse(m.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			r.problem("url", "must be a full http or https address, such as https://example.com")
		}
	}
	if login := obj.Get("login"); !absent(login) {
		m.Login = r.login("login", login)
	}
	m.Tools = r.tools(obj.Get("tools"))
	return m
}

func (r *reader) login(path string, v goja.Value) *Login {
	obj := r.object(path, v, "must be an object with url and check")
	if obj == nil {
		return nil
	}
	r.only(path, obj, "url", "check", "run")
	l := &Login{URL: r.str(path+".url", obj.Get("url"), true)}
	if l.URL != "" && !strings.HasPrefix(l.URL, "/") {
		r.problem(path+".url", "must be a path on the service's site, starting with /")
	}
	r.function(path+".check", obj.Get("check"))
	if run := obj.Get("run"); !absent(run) {
		r.function(path+".run", run)
		l.Run = true
	}
	return l
}

func (r *reader) frame(path string, v goja.Value) *Frame {
	obj := r.object(path, v, "must be an object with site and page")
	if obj == nil {
		return nil
	}
	r.only(path, obj, "site", "page")
	f := &Frame{Site: r.str(path+".site", obj.Get("site"), true)}
	if f.Site != "" {
		if u, err := url.Parse(f.Site); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			r.problem(path+".site", "must be a site's address, with no path, such as https://example.com")
		}
	}
	page := obj.Get("page")
	if _, ok := goja.AssertFunction(page); ok {
		f.PageFromInput = true
	} else if f.Page = r.str(path+".page", page, true); f.Page != "" && !strings.HasPrefix(f.Page, "/") {
		r.problem(path+".page", "must be a path on the service's site, starting with /, or a function of the input that returns one")
	}
	return f
}

func (r *reader) tools(v goja.Value) []Tool {
	tools := []Tool{}
	if absent(v) {
		return tools
	}
	obj := r.object("tools", v, "must be an object, with one entry for each tool")
	if obj == nil {
		return tools
	}
	for _, name := range obj.Keys() {
		path := "tools." + name
		if !toolName.MatchString(name) {
			r.problem(path, "a tool's name is lower-case letters, digits, and underscores, such as list_issues")
		}
		t := r.object(path, obj.Get(name), "must be an object")
		if t == nil {
			continue
		}
		r.only(path, t, "title", "description", "annotations", "inputSchema", "outputSchema", "login", "frame", "run")
		tool := Tool{
			Name:        name,
			Title:       r.str(path+".title", t.Get("title"), false),
			Description: r.str(path+".description", t.Get("description"), true),
			Annotations: r.annotations(path+".annotations", t.Get("annotations")),
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}
		if s := t.Get("inputSchema"); !absent(s) {
			tool.InputSchema = r.schema(path+".inputSchema", s)
		}
		if s := t.Get("outputSchema"); !absent(s) {
			tool.OutputSchema = r.schema(path+".outputSchema", s)
		}
		if login := t.Get("login"); !absent(login) {
			tool.Login = r.login(path+".login", login)
		}
		if frame := t.Get("frame"); !absent(frame) {
			tool.Frame = r.frame(path+".frame", frame)
		}
		r.function(path+".run", t.Get("run"))
		tools = append(tools, tool)
	}
	return tools
}

func (r *reader) annotations(path string, v goja.Value) *Annotations {
	if absent(v) {
		return nil
	}
	obj := r.object(path, v, "must be an object")
	if obj == nil {
		return nil
	}
	r.only(path, obj, "readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint")
	return &Annotations{
		ReadOnlyHint:    r.boolean(path+".readOnlyHint", obj.Get("readOnlyHint")),
		DestructiveHint: r.boolean(path+".destructiveHint", obj.Get("destructiveHint")),
		IdempotentHint:  r.boolean(path+".idempotentHint", obj.Get("idempotentHint")),
		OpenWorldHint:   r.boolean(path+".openWorldHint", obj.Get("openWorldHint")),
	}
}

// schema checks that v is a JSON Schema for an object, which MCP requires of
// both a tool's input and its output, and returns it as JSON.
func (r *reader) schema(path string, v goja.Value) json.RawMessage {
	obj := r.object(path, v, `must be a JSON Schema object, with type: "object"`)
	if obj == nil {
		return nil
	}
	if t := obj.Get("type"); absent(t) || t.String() != "object" {
		r.problem(path, `must have type: "object"; MCP tools take and return objects`)
	}
	stringify, _ := goja.AssertFunction(r.vm.Get("JSON").ToObject(r.vm).Get("stringify"))
	out, err := stringify(goja.Undefined(), obj)
	if err != nil {
		r.problem(path, "is not plain JSON: %v", err)
		return nil
	}
	return json.RawMessage(out.String())
}

func (r *reader) object(path string, v goja.Value, problem string) *goja.Object {
	if absent(v) {
		r.problem(path, "%s", problem)
		return nil
	}
	obj, ok := v.(*goja.Object)
	if !ok || obj.ClassName() == "Array" || obj.ClassName() == "Function" {
		r.problem(path, "%s", problem)
		return nil
	}
	return obj
}

func (r *reader) only(path string, obj *goja.Object, allowed ...string) {
	for _, k := range obj.Keys() {
		if !slices.Contains(allowed, k) {
			where := path
			if where == "" {
				where = k
			} else {
				where += "." + k
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

func (r *reader) boolean(path string, v goja.Value) *bool {
	if absent(v) {
		return nil
	}
	b, ok := v.Export().(bool)
	if !ok {
		r.problem(path, "must be true or false")
		return nil
	}
	return &b
}

func (r *reader) function(path string, v goja.Value) {
	if _, ok := goja.AssertFunction(v); !ok {
		r.problem(path, "must be a function")
	}
}

func absent(v goja.Value) bool {
	return v == nil || goja.IsUndefined(v) || goja.IsNull(v)
}
