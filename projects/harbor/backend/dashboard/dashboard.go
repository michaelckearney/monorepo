// Package dashboard compiles dashboards. A dashboard is one Vue file that
// shows what services' tools return. Compiling one also finds the tools it
// uses, before it runs: it reaches a service only with useService("name"),
// and its tools only as service.tool, so they can be read from the file.
//
// Vue's compiler is JavaScript, so it runs in goja, from compiler/compiler.js:
// a bundle of compiler/entry.js and Vue's compiler that TestCompiler makes.
// esbuild then removes the TypeScript types.
package dashboard

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"

	"github.com/michaelckearney/monorepo/projects/harbor/frontend/src/kit"
)

//go:embed compiler/compiler.js
var compilerJS string

// Compiled is a dashboard ready to show.
type Compiled struct {
	// JS is a CommonJS module. It gets "vue" and "harbor" with require, and
	// sets module.exports.default to the component.
	JS string `json:"js"`
	// CSS is the dashboard's styles. Scoped styles are scoped already.
	CSS string `json:"css"`
	// Uses is the tools it uses, by service and tool.
	Uses []Use `json:"uses"`
}

// Use is a tool a dashboard uses. The compiler finds the service and the
// tool; Documents fills in the rest from the service's manifest, for the
// frontend to know which tools only read.
type Use struct {
	Service string `json:"service"`
	Tool    string `json:"tool"`
	// Title is the tool's title, for asking before it runs.
	Title string `json:"title"`
	// Read is true for a tool that only reads.
	Read bool `json:"read"`
	// Destructive is true for one that changes something that cannot be put back.
	Destructive bool `json:"destructive"`
}

// Compile compiles a dashboard. A file with problems gives an error that
// lists them, one to a line, with the line of the file each is on.
func Compile(source string) (Compiled, error) {
	out, err := run(source, scopeID(source))
	if err != nil {
		return Compiled{}, err
	}
	if len(out.Problems) > 0 {
		return Compiled{}, errors.New(strings.Join(out.Problems, "\n"))
	}
	js, err := strip(out.Code)
	if err != nil {
		return Compiled{}, err
	}
	uses := make([]Use, len(out.Uses))
	for i, u := range out.Uses {
		uses[i] = Use{Service: u[0], Tool: u[1]}
	}
	return Compiled{JS: js, CSS: out.CSS, Uses: uses}, nil
}

// Check returns the problems in a dashboard. None means it compiles.
func Check(source string) []string {
	if _, err := Compile(source); err != nil {
		return strings.Split(err.Error(), "\n")
	}
	return nil
}

type result struct {
	Problems []string    `json:"problems"`
	Code     string      `json:"code"`
	CSS      string      `json:"css"`
	Uses     [][2]string `json:"uses"`
}

// The compiler takes a fifth of a second to load, so it loads once, on
// first use. A goja runtime runs one call at a time.
var (
	mu      sync.Mutex
	vm      *goja.Runtime
	compile goja.Callable
	loadErr error
)

func run(source, id string) (result, error) {
	mu.Lock()
	defer mu.Unlock()
	if compile == nil && loadErr == nil {
		vm = goja.New()
		if _, loadErr = vm.RunString(compilerJS); loadErr == nil {
			compile, _ = goja.AssertFunction(vm.Get("compileDashboard"))
		}
	}
	if loadErr != nil {
		return result{}, fmt.Errorf("the dashboard compiler did not load: %w", loadErr)
	}
	v, err := compile(goja.Undefined(), vm.ToValue(source), vm.ToValue(id), vm.ToValue(kit.Exports()), vm.ToValue(kit.Like), vm.ToValue(kit.Widgets()))
	if err != nil {
		return result{}, fmt.Errorf("the dashboard compiler failed: %w", err)
	}
	data, err := json.Marshal(v.Export())
	if err != nil {
		return result{}, err
	}
	var out result
	return out, json.Unmarshal(data, &out)
}

// strip removes the types and makes the module CommonJS. Vue's compiler
// checks the imports of <script setup>; this checks a plain <script>'s.
func strip(code string) (string, error) {
	r := api.Build(api.BuildOptions{
		Stdin:    &api.StdinOptions{Contents: code, Loader: api.LoaderTS, Sourcefile: "dashboard.vue"},
		Bundle:   true,
		External: []string{"vue", "harbor"},
		Format:   api.FormatCommonJS,
		Target:   api.ES2020,
		Write:    false,
		LogLevel: api.LogLevelSilent,
	})
	if len(r.Errors) > 0 {
		problems := make([]string, len(r.Errors))
		for i, e := range r.Errors {
			problems[i] = e.Text
			if name, ok := strings.CutPrefix(e.Text, "Could not resolve "); ok {
				problems[i] = fmt.Sprintf(`a dashboard can only import from "vue" and "harbor", not %s`, name)
			}
		}
		return "", errors.New(strings.Join(problems, "\n"))
	}
	return string(r.OutputFiles[0].Contents), nil
}

// scopeID names a dashboard's scoped styles. It comes from the source, so
// two dashboards never share one.
func scopeID(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:4])
}
