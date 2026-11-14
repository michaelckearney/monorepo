package engine

import (
	"encoding/json"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

func TestValidateSaysWhatIsWrong(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["owner"],"properties":{"owner":{"type":"string"},"limit":{"type":"integer"}}}`)
	for _, c := range []struct{ value, want string }{
		{`{"owner":"a"}`, ""},
		{`{}`, "missing property 'owner'"},
		{`{"owner":"a","limit":"x"}`, "/limit: got string, want integer"},
	} {
		err := validate(schema, json.RawMessage(c.value))
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != c.want {
			t.Errorf("validate(%s) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestNoToolSaysWhichToolsThereAre(t *testing.T) {
	for _, c := range []struct {
		tools []service.Tool
		want  string
	}{
		{nil, "tasklane has no tool list_issues: it has no tools"},
		{[]service.Tool{{Name: "get_issue"}, {Name: "close_issue"}}, "tasklane has no tool list_issues. Its tools: get_issue, close_issue"},
	} {
		if got := noTool(service.Manifest{Name: "tasklane", Tools: c.tools}, "list_issues").Error(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
