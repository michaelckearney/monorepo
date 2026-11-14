package engine

import (
	"encoding/json"
	"fmt"
)

// Files a tool takes and returns. A tool returns a File or a Blob anywhere
// in its output; Harbor keeps it, and the output has a FileRef in its place.
// A tool takes one as an input property with format: "file", whose value is
// a FileRef's ID; its run gets a File. A file never crosses in a tool's JSON
// as base64, which makes it a third larger and leaves every caller to
// decode it.

// Files keeps the files tools take and return.
type Files interface {
	SaveFile(f File) (FileRef, error)
	File(id string) (File, error)
}

// File is a file's name, media type, and bytes.
type File struct {
	Name string
	Type string
	Data []byte
}

// FileRef is a file in a tool's output: its ID, for File, and what it is.
type FileRef struct {
	File string `json:"file"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

// schemaNode is what filesIn reads of a JSON Schema.
type schemaNode struct {
	Type       any                    `json:"type"`
	Format     string                 `json:"format"`
	Properties map[string]*schemaNode `json:"properties"`
	Items      *schemaNode            `json:"items"`
}

// filesIn is input with each file's ID, where the schema says a property is
// a file, replaced by the file, as the page's half of invoke takes it.
func (e *Engine) filesIn(schema json.RawMessage, input json.RawMessage) (any, error) {
	var s schemaNode
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return nil, err
	}
	return e.withFiles(&s, v)
}

func (e *Engine) withFiles(s *schemaNode, v any) (any, error) {
	if s == nil {
		return v, nil
	}
	switch x := v.(type) {
	case string:
		if s.Format != "file" {
			return x, nil
		}
		f, err := e.files.File(x)
		if err != nil {
			return nil, fmt.Errorf("there is no file %q: %w", x, err)
		}
		return map[string]any{"__harborFile": map[string]any{"name": f.Name, "type": f.Type, "data": f.Data}}, nil
	case map[string]any:
		for k, item := range x {
			out, err := e.withFiles(s.Properties[k], item)
			if err != nil {
				return nil, err
			}
			x[k] = out
		}
	case []any:
		for i, item := range x {
			out, err := e.withFiles(s.Items, item)
			if err != nil {
				return nil, err
			}
			x[i] = out
		}
	}
	return v, nil
}

// filesOut keeps the files a tool returned, and puts a FileRef in the
// output in place of each.
func (e *Engine) filesOut(value json.RawMessage, files []pageFile) (json.RawMessage, error) {
	if len(files) == 0 {
		return value, nil
	}
	refs := make([]FileRef, len(files))
	for i, f := range files {
		ref, err := e.files.SaveFile(File{Name: f.Name, Type: f.Type, Data: f.Data})
		if err != nil {
			return nil, fmt.Errorf("could not keep the file %q the tool returned: %w", f.Name, err)
		}
		refs[i] = ref
	}
	var v any
	if err := json.Unmarshal(value, &v); err != nil {
		return nil, err
	}
	return json.Marshal(placeRefs(v, refs))
}

func placeRefs(v any, refs []FileRef) any {
	switch x := v.(type) {
	case map[string]any:
		if n, ok := x["__harborFile"].(float64); ok && len(x) == 1 && int(n) < len(refs) {
			return refs[int(n)]
		}
		for k, item := range x {
			x[k] = placeRefs(item, refs)
		}
	case []any:
		for i, item := range x {
			x[i] = placeRefs(item, refs)
		}
	}
	return v
}
