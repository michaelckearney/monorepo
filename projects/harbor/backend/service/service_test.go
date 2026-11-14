package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const starter = `import { defineService } from "harbor"

export default defineService({
  name: "example",
  title: "Example",
  description: "What this service does.",
  url: "https://example.com",
  tools: {
    get_title: {
      description: "Read the title of the home page.",
      annotations: { readOnlyHint: true },
      outputSchema: { type: "object", properties: { title: { type: "string" } } },
      async run() { return { title: document.title } },
    },
  },
})`

func TestReadStarter(t *testing.T) {
	m, err := Read(starter)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(m)
	want := `{"name":"example","title":"Example","description":"What this service does.","url":"https://example.com",` +
		`"tools":[{"name":"get_title","description":"Read the title of the home page.","annotations":{"readOnlyHint":true},` +
		`"inputSchema":{"type":"object"},"outputSchema":{"type":"object","properties":{"title":{"type":"string"}}}}]}`
	if string(got) != want {
		t.Errorf("manifest:\n got %s\nwant %s", got, want)
	}
}

func TestReadServiceKeepsTypesHelpersAndOrder(t *testing.T) {
	m, err := Read(`
import { defineService } from "harbor"

interface Issue { number: number }
async function page(path: string): Promise<Document> {
  return new DOMParser().parseFromString(await (await fetch(path)).text(), "text/html")
}

export default defineService({
  name: "gitea",
  title: "Gitea",
  url: "http://localhost:7100",
  login: { url: "/user/login", check: async () => (await page("/")).querySelector("a") !== null },
  tools: {
    zebra: { description: "Z", run: async (): Promise<{ issues: Issue[] }> => ({ issues: [] }) },
    apple: {
      description: "A",
      inputSchema: { type: "object", required: ["owner"], properties: { owner: { type: "string" } } },
      run: async ({ owner }: { owner: string }) => ({ owner }),
    },
  },
})`)
	if err != nil {
		t.Fatal(err)
	}
	if m.Login == nil || m.Login.URL != "/user/login" {
		t.Errorf("login = %+v", m.Login)
	}
	if len(m.Tools) != 2 || m.Tools[0].Name != "zebra" || m.Tools[1].Name != "apple" {
		t.Fatalf("tools out of order: %+v", m.Tools)
	}
	if got := string(m.Tools[1].InputSchema); got != `{"type":"object","required":["owner"],"properties":{"owner":{"type":"string"}}}` {
		t.Errorf("input schema = %s", got)
	}
}

func TestReadServiceProblems(t *testing.T) {
	defer func(d time.Duration) { readTimeout = d }(readTimeout)
	readTimeout = 50 * time.Millisecond
	for _, c := range []struct {
		name, source string
		want         []string
	}{
		{"syntax error", `export default {`, []string{"line 1:"}},
		{"no export", `const x = 1`, []string{"the file must export a service"}},
		{"other imports", `import x from "lodash"; export default x`, []string{`only import from "harbor", not "lodash"`}},
		{"page at the top level", `const t = document.title; export default {}`,
			[]string{"document is not defined", "move it into a function"}},
		{"endless loop", `while (true) {}; export default {}`, []string{"longer than a second"}},
		{"everything missing", `export default {}`, []string{"name: is required", "title: is required", "url: is required"}},
		{"bad values", `export default {
			name: "My Service", title: "T", url: "example.com", colour: "red",
			login: { url: "https://example.com/login", check: true },
			tools: {
				"List-Issues": { run() {} },
				list: {
					description: "L",
					annotations: { readOnlyHint: "yes" },
					inputSchema: { type: "array" },
					outputSchema: [],
				},
			},
		}`, []string{
			"name: use lower-case letters",
			"url: must be a full http or https address",
			"colour: is not a field Harbor knows",
			"login.url: must be a path",
			"login.check: must be a function",
			"tools.List-Issues: a tool's name is lower-case",
			"tools.List-Issues.description: is required",
			"tools.list.annotations.readOnlyHint: must be true or false",
			`tools.list.inputSchema: must have type: "object"`,
			"tools.list.outputSchema: must be a JSON Schema object",
			"tools.list.run: must be a function",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Read(c.source)
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error does not mention %q:\n%v", w, err)
				}
			}
		})
	}
}
