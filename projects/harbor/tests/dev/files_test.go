package dev

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

const filesService = `export default {
  name: "files",
  title: "Files",
  url: "URL",
  tools: {
    statement: {
      description: "Download a statement.",
      outputSchema: { type: "object", properties: { pdf: { type: "object", format: "file" } } },
      async run() {
        return { pdf: new File([new Uint8Array([37, 80, 68, 70, 0, 255])], "march.pdf", { type: "application/pdf" }) }
      },
    },
    photos: {
      description: "Several files, in an array.",
      async run() {
        return { photos: [new Blob(["a"], { type: "image/png" }), new Blob(["bb"], { type: "image/png" })] }
      },
    },
    upload: {
      description: "Take a file, and say what arrived.",
      inputSchema: { type: "object", required: ["photo"], properties: { photo: { type: "string", format: "file" } } },
      async run({ photo }) {
        const bytes = [...new Uint8Array(await photo.arrayBuffer())]
        return { file: photo instanceof File, name: photo.name, type: photo.type, bytes }
      },
    },
  },
}
`

func TestToolsTakeAndReturnFiles(t *testing.T) {
	srv := site(t)
	files := &memoryFiles{}
	e := engine.New(t.TempDir(), &memorySecrets{}, noWindow{}, files)
	t.Cleanup(e.Close)
	svc := load(t, filesService, srv.URL)

	got := call(t, e, svc, "statement", "{}")
	pdf, _ := got["pdf"].(map[string]any)
	if pdf == nil || pdf["name"] != "march.pdf" || pdf["type"] != "application/pdf" || pdf["size"] != float64(6) {
		t.Fatalf("statement = %v", got)
	}
	kept, err := files.File(pdf["file"].(string))
	if err != nil || string(kept.Data) != "%PDF\x00\xff" {
		t.Errorf("kept %q, %v", kept.Data, err)
	}

	got = call(t, e, svc, "photos", "{}")
	if photos, _ := got["photos"].([]any); len(photos) != 2 || photos[1].(map[string]any)["size"] != float64(2) {
		t.Errorf("photos = %v", got)
	}

	input, _ := json.Marshal(map[string]string{"photo": pdf["file"].(string)})
	got = call(t, e, svc, "upload", string(input))
	if got["file"] != true || got["name"] != "march.pdf" || got["type"] != "application/pdf" || len(got["bytes"].([]any)) != 6 {
		t.Errorf("upload = %v", got)
	}
	if _, err := e.Call(context.Background(), svc, "upload", json.RawMessage(`{"photo":"nope"}`)); err == nil {
		t.Error("a file that does not exist was taken")
	}
}
