package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const echo = `import { defineProvider } from "harbor"

export default defineProvider({
  name: "echo",
  title: "Echo",
  settings: {
    key: { title: "Key", secret: true },
    word: { title: "Word", default: "hello" },
  },
  async *generate(request: { messages: { parts: { text?: string }[] }[] }, { settings }) {
    yield { text: settings.word + " " + settings.key }
    yield { toolCall: { id: "c1", name: "look" } }
    yield { done: true, usage: { tokens: 3 } }
  },
})`

func load(t *testing.T, source string) *Provider {
	t.Helper()
	p, err := Load(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// run runs a provider to its end, and returns what it yielded.
func run(t *testing.T, p *Provider, settings map[string]string) ([]Output, error) {
	t.Helper()
	var outs []Output
	err := Model{Provider: p, Settings: settings}.Generate(t.Context(), Request{Messages: []Message{}}, func(o Output) {
		outs = append(outs, o)
	})
	return outs, err
}

func TestLoadReadsTheManifest(t *testing.T) {
	got, _ := json.Marshal(load(t, echo).Manifest)
	want := `{"name":"echo","title":"Echo","settings":[{"key":"key","title":"Key","secret":true},{"key":"word","title":"Word","default":"hello"}]}`
	if string(got) != want {
		t.Errorf("manifest:\n got %s\nwant %s", got, want)
	}
}

func TestLoadListsEveryProblem(t *testing.T) {
	_, err := Load(`export default { name: "Bad Name", settings: { key: { secret: "yes", optional: 1 } }, extra: 1 }`, nil)
	want := []string{
		"extra: is not a field Harbor knows; the fields are name, title, description, settings, generate",
		"title: is required",
		"name: use lower-case letters, digits, and hyphens, such as my-provider",
		"settings.key.title: is required",
		"settings.key.secret: must be true or false",
		"settings.key.optional: must be true or false",
		"generate: must be an async generator function: async *generate(request, context) { ... }",
	}
	if err == nil || err.Error() != strings.Join(want, "\n") {
		t.Errorf("error:\n%v\nwant:\n%s", err, strings.Join(want, "\n"))
	}
	if _, err := Load(`import x from "fs"; export default x`, nil); err == nil || !strings.Contains(err.Error(), `a provider can only import from "harbor", not "fs"`) {
		t.Errorf("import: %v", err)
	}
}

func TestGenerateYieldsWithSettingsAndDefaults(t *testing.T) {
	outs, err := run(t, load(t, echo), map[string]string{"key": "k1"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(outs)
	want := `[{"text":"hello k1"},{"toolCall":{"id":"c1","name":"look","input":{}}},{"done":true,"usage":{"tokens":3}}]`
	if string(got) != want {
		t.Errorf("outputs:\n got %s\nwant %s", got, want)
	}
}

func TestGenerateFails(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"a thrown error":       {`yield { text: "a" }; throw new Error("the key is wrong")`, "the key is wrong"},
		"a wrong yield":        {`yield { words: "a" }`, `generate yielded {"words":"a"}: json: unknown field "words"; yield { text }, { toolCall }, or { done: true }`},
		"a tool call, no name": {`yield { toolCall: { id: "c1" } }`, `generate yielded a tool call without an id or a name: {"toolCall":{"id":"c1"}}`},
		"a failed fetch":       {`await fetch("http://127.0.0.1:1/")`, "fetch http://127.0.0.1:1/: "},
	} {
		t.Run(name, func(t *testing.T) {
			p := load(t, `export default { name: "x", title: "X", async *generate() { `+tc.body+` } }`)
			_, err := run(t, p, nil)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGenerateMustBeAGenerator(t *testing.T) {
	_, err := run(t, load(t, `export default { name: "x", title: "X", async generate() { return "hi" } }`), nil)
	if err == nil || !strings.HasPrefix(err.Error(), "generate must be an async generator function") {
		t.Errorf("error = %v", err)
	}
}

func TestFetchStreams(t *testing.T) {
	// "é" is two bytes; the server splits it between two writes.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k1" || r.Method != "POST" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		w.Header().Set("X-Echo", string(body))
		w.Write([]byte("caf\xc3"))
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte("\xa9!"))
	}))
	defer server.Close()

	p := load(t, `export default { name: "x", title: "X", settings: { url: { title: "URL" } }, async *generate(_, { settings }) {
		const response = await fetch(settings.url, { method: "post", headers: { Authorization: "Bearer k1" }, body: "ping" })
		yield { text: response.status + " " + response.ok + " " + response.headers.get("x-echo") + " " + response.headers.get("x-none") + "|" }
		const reader = response.body.getReader()
		const decoder = new TextDecoder()
		for (;;) {
			const { done, value } = await reader.read()
			if (done) break
			yield { text: "[" + decoder.decode(value, { stream: true }) + "]" }
		}
		const denied = await fetch(settings.url)
		yield { text: "|" + denied.status + " " + (await denied.text()).trim() }
	} }`)
	outs, err := run(t, p, map[string]string{"url": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, o := range outs {
		text.WriteString(o.Text)
	}
	if want := "200 true ping null|[caf][é!]|401 no"; text.String() != want {
		t.Errorf("text = %q, want %q", text.String(), want)
	}
}

func TestFetchJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"a":[1,2]}`))
	}))
	defer server.Close()
	p := load(t, `export default { name: "x", title: "X", settings: { url: { title: "URL" } }, async *generate(_, { settings }) {
		const data = await (await fetch(settings.url)).json()
		yield { text: String(data.a.length) }
	} }`)
	outs, err := run(t, p, map[string]string{"url": server.URL})
	if err != nil || len(outs) != 1 || outs[0].Text != "2" {
		t.Errorf("outputs %v, error %v", outs, err)
	}
}

func TestCancelStopsTheCodeAndItsRequests(t *testing.T) {
	requested := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requested)
		<-r.Context().Done()
	}))
	defer server.Close()

	for name, body := range map[string]string{
		"a request": `await fetch(settings.url)`,
		"a loop":    `for (;;) {}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := load(t, `export default { name: "x", title: "X", settings: { url: { title: "URL" } }, async *generate(_, { settings }) { `+body+` } }`)
			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(100*time.Millisecond, cancel)
			err := Model{Provider: p, Settings: map[string]string{"url": server.URL}}.Generate(ctx, Request{}, func(Output) {})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v", err)
			}
		})
	}
	<-requested
}

func TestAProviderImportsASharedModule(t *testing.T) {
	shared := fstest.MapFS{
		"shared/words.ts": {Data: []byte(`import { end } from "./end"; export const hello = (name: string) => "hello " + name + end`)},
		"shared/end.ts":   {Data: []byte(`export const end = "!"`)},
	}
	p, err := Load(`import { hello } from "./shared/words"
		export default { name: "x", title: "X", async *generate() { yield { text: hello("you") } } }`, shared)
	if err != nil {
		t.Fatal(err)
	}
	outs, err := run(t, p, nil)
	if err != nil || len(outs) != 1 || outs[0].Text != "hello you!" {
		t.Errorf("outputs %+v, %v", outs, err)
	}
	if _, err := Load(`import { x } from "./shared/none"; export default x`, shared); err == nil || !strings.Contains(err.Error(), "there is no module shared/none.ts") {
		t.Errorf("a missing module: %v", err)
	}
}
