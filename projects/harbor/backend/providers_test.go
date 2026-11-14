package backend

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/provider"
)

func TestProvidersAreSetUpBeforeTheAgentUsesOne(t *testing.T) {
	p := newProviders(desktop.TestDB(t))
	if got := p.List(); len(got) != 2 || got[0].Name != "openai" || got[1].Name != "openai-compatible" {
		t.Fatalf("providers: %+v", got)
	}
	if _, err := p.model(); err != errNoProvider {
		t.Errorf("with none: %v", err)
	}
	if err := p.Save("openai", map[string]string{"apiKey": " ", "model": ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.model(); err == nil || err.Error() != "OpenAI needs its API key. Add it in Providers" {
		t.Errorf("with no key: %v", err)
	}
	// A server such as vLLM needs no key; its model has no default.
	if err := p.Save("openai-compatible", map[string]string{"model": "qwen"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.model(); err != nil {
		t.Errorf("with no key: %v", err)
	}
	if err := p.Save("openai", map[string]string{"apiKey": "sk-1"}); err != nil {
		t.Fatal(err)
	}
	m, err := p.model()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.(provider.Model).Settings; len(got) != 1 || got["apiKey"] != "sk-1" {
		t.Errorf("settings: %v", got)
	}
	if err := p.Save("openai", map[string]string{"color": "red"}); err == nil || err.Error() != "OpenAI has no setting color" {
		t.Errorf("an unknown setting: %v", err)
	}
	if err := p.Save("nope", nil); err == nil {
		t.Error("an unknown provider was saved")
	}
}

// openAI runs the OpenAI provider against a fake API that answers with
// reply, and returns what the provider yielded, and the request it sent.
func openAI(t *testing.T, status int, reply string, req provider.Request) ([]provider.Output, map[string]any, error) {
	t.Helper()
	return openAIWith(t, "openai", map[string]string{"apiKey": "sk-test"}, "/v1/responses", "Bearer sk-test", status, reply, req)
}

// openAIWith is openAI with this provider and these settings, expecting a
// request to path with this Authorization header.
func openAIWith(t *testing.T, name string, settings map[string]string, path, auth string, status int, reply string, req provider.Request) ([]provider.Output, map[string]any, error) {
	t.Helper()
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.Header.Get("Authorization") != auth {
			t.Errorf("request: %s %s, %q", r.Method, r.URL, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sent)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		// Each event in its own write, as a stream would arrive.
		for _, chunk := range strings.SplitAfter(reply, "\n\n") {
			w.Write([]byte(chunk))
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: redirect{target}}

	p := newProviders(desktop.TestDB(t)).find(name)
	var outs []provider.Output
	err := provider.Model{Provider: p, Settings: settings, Client: client}.
		Generate(t.Context(), req, func(o provider.Output) { outs = append(outs, o) })
	return outs, sent, err
}

// redirect sends every request to the test server.
type redirect struct{ to *url.URL }

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

func event(data string) string {
	var e struct{ Type string }
	json.Unmarshal([]byte(data), &e)
	return "event: " + e.Type + "\ndata: " + data + "\n\n"
}

func TestOpenAISendsTheChat(t *testing.T) {
	req := provider.Request{
		Instructions: "Be brief.",
		Messages: []provider.Message{
			{Role: "user", Parts: []provider.Part{{Text: "Save milk"}}},
			{Role: "assistant", Parts: []provider.Part{
				{Text: "Saving."},
				{ToolCall: &provider.ToolCall{ID: "c1", Name: "save", Input: map[string]any{"text": "milk"}}},
				{ToolCall: &provider.ToolCall{ID: "c2", Name: "check", Input: map[string]any{}}},
			}},
			{Role: "tool", Parts: []provider.Part{{ToolResult: &provider.ToolResult{ID: "c1", Output: map[string]any{"saved": true}}}}},
			{Role: "tool", Parts: []provider.Part{{ToolResult: &provider.ToolResult{ID: "c2", Error: "no"}}}},
		},
		Tools: []provider.Tool{{Name: "save", Description: "Save.", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	_, sent, err := openAI(t, 200, event(`{"type":"response.completed","response":{"usage":{"total_tokens":1}}}`), req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(sent)
	want := `{"input":[` +
		`{"content":"Save milk","role":"user"},` +
		`{"content":"Saving.","role":"assistant"},` +
		`{"arguments":"{\"text\":\"milk\"}","call_id":"c1","name":"save","type":"function_call"},` +
		`{"arguments":"{}","call_id":"c2","name":"check","type":"function_call"},` +
		`{"call_id":"c1","output":"{\"output\":{\"saved\":true}}","type":"function_call_output"},` +
		`{"call_id":"c2","output":"{\"error\":\"no\"}","type":"function_call_output"}],` +
		`"instructions":"Be brief.","model":"gpt-5","stream":true,` +
		`"tools":[{"description":"Save.","name":"save","parameters":{"type":"object"},"strict":false,"type":"function"}]}`
	if string(got) != want {
		t.Errorf("request:\n got %s\nwant %s", got, want)
	}
}

func TestOpenAIStreamsTextAndToolCalls(t *testing.T) {
	reply := event(`{"type":"response.created","response":{"status":"in_progress"}}`) +
		event(`{"type":"response.output_item.added","item":{"type":"reasoning"}}`) +
		event(`{"type":"response.output_text.delta","delta":"Café "}`) +
		event(`{"type":"response.output_text.delta","delta":"time."}`) +
		event(`{"type":"response.function_call_arguments.delta","delta":"{\"te"}`) +
		event(`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"save","arguments":"{\"text\":\"milk\"}"}}`) +
		event(`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c2","name":"check","arguments":""}}`) +
		event(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`)
	outs, _, err := openAI(t, 200, reply, provider.Request{Messages: []provider.Message{}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(outs)
	want := `[{"text":"Café "},{"text":"time."},` +
		`{"toolCall":{"id":"c1","name":"save","input":{"text":"milk"}}},` +
		`{"toolCall":{"id":"c2","name":"check","input":{}}},` +
		`{"done":true,"usage":{"input_tokens":10,"output_tokens":5}}]`
	if string(got) != want {
		t.Errorf("outputs:\n got %s\nwant %s", got, want)
	}
}

func TestOpenAISaysWhatWentWrong(t *testing.T) {
	for name, tc := range map[string]struct {
		status      int
		reply, want string
	}{
		"a wrong key": {401, `{"error":{"message":"Incorrect API key provided."}}`,
			"OpenAI said 401 Unauthorized: Incorrect API key provided."},
		"a failure": {200, event(`{"type":"response.failed","response":{"error":{"message":"The server had an error."}}}`),
			"OpenAI could not reply: The server had an error."},
		"an error event": {200, event(`{"type":"error","message":"Rate limit reached."}`),
			"OpenAI could not reply: Rate limit reached."},
		"a cut-off reply": {200, event(`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`),
			"OpenAI stopped the reply early: max_output_tokens"},
		"an unfinished stream": {200, event(`{"type":"response.output_text.delta","delta":"Hel"}`),
			"OpenAI's reply ended before it was complete"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := openAI(t, tc.status, tc.reply, provider.Request{Messages: []provider.Message{}})
			if err == nil || err.Error() != tc.want {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAnotherServerWithAModelThatCannotSeeImages(t *testing.T) {
	settings := map[string]string{"baseUrl": "http://localhost:8000/v1/", "model": "qwen", "images": "No"}
	req := provider.Request{Messages: []provider.Message{
		{Role: "tool", Parts: []provider.Part{{ToolResult: &provider.ToolResult{ID: "c1", Output: "shot",
			Images: []provider.Image{{MIMEType: "image/png", Data: "iVBO"}}}}}},
	}}
	_, sent, err := openAIWith(t, "openai-compatible", settings, "/v1/responses", "", 200, event(`{"type":"response.completed","response":{"usage":{}}}`), req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(sent["input"])
	want := `[{"call_id":"c1","output":"{\"output\":\"shot\",\"note\":\"The tool returned 1 image(s), left out: this model cannot see images.\"}","type":"function_call_output"}]`
	if string(got) != want || sent["model"] != "qwen" {
		t.Errorf("request:\n got %s, model %v\nwant %s", got, sent["model"], want)
	}

	_, _, err = openAIWith(t, "openai-compatible", settings, "/v1/responses", "", 400, `{"error":{"message":"bad"}}`, req)
	if err == nil || err.Error() != "localhost:8000 said 400 Bad Request: bad" {
		t.Errorf("error = %v", err)
	}
}

func TestOpenAISendsImagesToAModelThatSeesThem(t *testing.T) {
	req := provider.Request{Messages: []provider.Message{
		{Role: "tool", Parts: []provider.Part{{ToolResult: &provider.ToolResult{ID: "c1", Output: "shot",
			Images: []provider.Image{{MIMEType: "image/png", Data: "iVBO"}}}}}},
	}}
	_, sent, err := openAI(t, 200, event(`{"type":"response.completed","response":{"usage":{}}}`), req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(sent["input"])
	want := `[{"call_id":"c1","output":[{"text":"{\"output\":\"shot\"}","type":"input_text"},{"image_url":"data:image/png;base64,iVBO","type":"input_image"}],"type":"function_call_output"}]`
	if string(got) != want {
		t.Errorf("input:\n got %s\nwant %s", got, want)
	}
}

func TestABenchmarkModelIsProviderSlashModel(t *testing.T) {
	p := newProviders(desktop.TestDB(t))
	if err := p.Save("openai", map[string]string{"apiKey": "sk-1"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Save("openai-compatible", map[string]string{"model": "qwen", "images": "no"}); err != nil {
		t.Fatal(err)
	}
	// Either provider, whichever is in use, and a model with a slash in it.
	m, err := p.modelAt("openai/gpt-5-mini")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.(provider.Model); got.Provider.Name != "openai" || got.Settings["model"] != "gpt-5-mini" || got.Settings["apiKey"] != "sk-1" {
		t.Errorf("openai: %s %v", got.Provider.Name, got.Settings)
	}
	m, err = p.modelAt("openai-compatible/nvidia/Qwen3.6-35B-A3B-NVFP4")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.(provider.Model); got.Provider.Name != "openai-compatible" || got.Settings["model"] != "nvidia/Qwen3.6-35B-A3B-NVFP4" || got.Settings["images"] != "no" {
		t.Errorf("openai-compatible: %s %v", got.Provider.Name, got.Settings)
	}
	for at, want := range map[string]string{
		"gpt-5":      `write "gpt-5" as provider/model, such as openai/gpt-5`,
		"openai/":    `write "openai/" as provider/model, such as openai/gpt-5`,
		"nope/gpt-5": "there is no provider named nope",
	} {
		if _, err := p.modelAt(at); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", at, err, want)
		}
	}
}
