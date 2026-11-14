// Package kit is the dashboard kit as the backend sees it: the components a
// dashboard imports from "harbor", read from their own files, to search and
// to check imports against.
//
// The files are the catalog. A component's name is its file's name, and its
// kind is its folder. Each file starts with a comment that describes it:
//
//	<!--
//	description: One sentence: what it shows.
//	keywords: words, someone, would, use, when, they, want, it
//	example:
//	  <InboxWidget title="Messages" :messages="[...]" />
//	-->
//
// and its props are its defineProps, with their comments, and the shapes of
// data they name (shapes.ts). A widget in the catalog also takes the Widget
// frame's props, which fall through to it.
package kit

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

//go:embed widgets/*.vue primitives/*.vue layout/*.vue controls/*.vue shapes.ts
var files embed.FS

// Functions are what "harbor" has besides the components, as the frontend's
// harbor() makes it (dashboards.ts).
var Functions = []string{"useService", "format", "fileURL"}

// The kinds, by folder.
var kinds = map[string]string{"widgets": "widget", "primitives": "primitive", "layout": "layout", "controls": "control"}

// Component is one of the kit's components, as its file describes it.
type Component struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind" jsonschema:"widget, primitive, layout, or control"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
	// Props is its defineProps and defineModel, with the comment above them,
	// and the shapes they name.
	Props    string   `json:"props,omitempty"`
	Examples []string `json:"examples"`
}

var all = mustLoad()

func mustLoad() []Component {
	cs, err := load(files)
	if err != nil {
		panic(err)
	}
	return cs
}

func load(fsys fs.FS) ([]Component, error) {
	data, err := fs.ReadFile(fsys, "shapes.ts")
	if err != nil {
		return nil, err
	}
	shapes := shapesIn(string(data))
	var out []Component
	var frame string
	for folder, kind := range kinds {
		paths, err := fs.Glob(fsys, folder+"/*.vue")
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return nil, err
			}
			c, err := parse(string(data))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			c.Name = strings.TrimSuffix(path.Base(p), ".vue")
			c.Kind = kind
			for _, s := range shapes {
				if regexp.MustCompile(`\b` + s.name + `\b`).MatchString(c.Props) {
					c.Props += "\n\n" + s.text
				}
			}
			if c.Name == "Widget" {
				frame = c.Props
			}
			out = append(out, c)
		}
	}
	for i, c := range out {
		if c.Kind == "widget" && c.Name != "Widget" {
			out[i].Props += "\n\n// And the Widget frame's props, which fall through to it:\n" + frame
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// A shape of data, in shapes.ts: its name, and its interface with its comments.
type shape struct{ name, text string }

var shapeStart = regexp.MustCompile(`(?m)^/\*\*.*\*/\nexport interface (\w+) \{`)

// shapesIn is the interfaces in shapes.ts, each with the comment above it.
func shapesIn(source string) []shape {
	var out []shape
	for _, m := range shapeStart.FindAllStringSubmatchIndex(source, -1) {
		end := strings.Index(source[m[0]:], "\n}\n")
		if end < 0 {
			panic("shapes.ts has an interface that does not end")
		}
		out = append(out, shape{name: source[m[2]:m[3]], text: source[m[0] : m[0]+end+2]})
	}
	if len(out) == 0 {
		panic("shapes.ts has no shapes")
	}
	return out
}

// parse reads a component's file: its leading comment, and its props.
func parse(source string) (Component, error) {
	var c Component
	if !strings.HasPrefix(source, "<!--") {
		return c, fmt.Errorf("does not start with a <!-- comment --> that describes it")
	}
	end := strings.Index(source, "-->")
	if end < 0 {
		return c, fmt.Errorf("its first comment does not end")
	}
	var example []string
	flush := func() {
		if len(example) > 0 {
			c.Examples = append(c.Examples, strings.Join(example, "\n"))
			example = nil
		}
	}
	inExample := false
	for _, line := range strings.Split(source[len("<!--"):end], "\n") {
		if inExample && (strings.HasPrefix(line, "  ") || strings.TrimSpace(line) == "") {
			if strings.TrimSpace(line) != "" {
				example = append(example, strings.TrimPrefix(line, "  "))
			}
			continue
		}
		flush()
		inExample = false
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "description":
			c.Description = value
		case "keywords":
			for _, k := range strings.Split(value, ",") {
				if k = strings.TrimSpace(k); k != "" {
					c.Keywords = append(c.Keywords, k)
				}
			}
		case "example":
			inExample = true
		}
	}
	flush()
	c.Props = props(source)
	return c, nil
}

// props is the statements of <script setup> that define props or a model,
// each with the comment above it.
func props(source string) string {
	start := strings.Index(source, "<script setup")
	end := strings.Index(source, "</script>")
	if start < 0 || end < start {
		return ""
	}
	lines := strings.Split(source[start:end], "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], "defineProps") && !strings.Contains(lines[i], "defineModel") && !strings.Contains(lines[i], "withDefaults(") {
			continue
		}
		from := i
		for from > 0 && strings.HasPrefix(strings.TrimSpace(lines[from-1]), "//") {
			from--
		}
		depth := 0
		to := i
		for ; to < len(lines); to++ {
			for _, r := range lines[to] {
				switch r {
				case '(', '{', '[':
					depth++
				case ')', '}', ']':
					depth--
				}
			}
			if depth <= 0 {
				break
			}
		}
		out = append(out, strings.Join(lines[from:min(to+1, len(lines))], "\n"))
		i = to
	}
	return strings.Join(out, "\n\n")
}

// All is the kit's components, by name.
func All() []Component { return all }

// Exports is everything "harbor" has: its functions, then its components.
func Exports() []string {
	out := append([]string{}, Functions...)
	for _, c := range all {
		out = append(out, c.Name)
	}
	return out
}

// Widgets is the names of the kit's widgets: the catalog's, and the Widget frame.
func Widgets() []string {
	var out []string
	for _, c := range all {
		if c.Kind == "widget" {
			out = append(out, c.Name)
		}
	}
	return out
}

// Find is the component with this name, ignoring case.
func Find(name string) (Component, bool) {
	for _, c := range all {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return Component{}, false
}

// Like is the names closest to name: ones that contain it or that it
// contains, else ones a few letters from it.
func Like(name string) []string {
	n := strings.ToLower(name)
	var out []string
	for _, e := range Exports() {
		l := strings.ToLower(e)
		if n != "" && (strings.Contains(l, n) || strings.Contains(n, l)) {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		for _, e := range Exports() {
			if distance(n, strings.ToLower(e)) <= 3 {
				out = append(out, e)
			}
		}
	}
	return out
}

// distance is the Levenshtein distance between a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// Match is a component a search found, best first.
type Match struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

// Words that say nothing about what to show.
var stop = map[string]bool{
	"a": true, "an": true, "the": true, "my": true, "me": true, "our": true, "your": true, "i": true,
	"of": true, "on": true, "in": true, "for": true, "to": true, "from": true, "with": true, "and": true, "or": true,
	"show": true, "see": true, "all": true, "some": true, "dashboard": true, "something": true, "thing": true, "things": true,
	"widget": true, "component": true,
}

// Search ranks the components by how well their name, keywords, and
// description match query's words: a word in the name counts most, then a
// keyword, then the description; a keyword of several words that query
// says counts more. A widget comes before another kind that matches as
// well, since widgets are what to use first. It returns the best limit.
func Search(query string, limit int) []Match {
	words := tokens(query)
	if len(words) == 0 {
		return nil
	}
	q := " " + strings.Join(words, " ") + " "
	type scored struct {
		c     Component
		score float64
	}
	var found []scored
	for _, c := range all {
		name := set(tokens(splitCamel(c.Name)))
		desc := set(tokens(c.Description))
		keys := map[string]bool{}
		score := 0.0
		for _, k := range c.Keywords {
			kw := tokens(k)
			for _, w := range kw {
				keys[w] = true
			}
			if len(kw) > 1 && strings.Contains(q, " "+strings.Join(kw, " ")+" ") {
				score += 4
			}
		}
		for _, w := range words {
			switch {
			case name[w]:
				score += 5
			case keys[w]:
				score += 3
			case desc[w]:
				score += 1
			}
		}
		if score > 0 {
			if c.Kind == "widget" {
				score += 0.5
			}
			found = append(found, scored{c, score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	var out []Match
	for i := 0; i < len(found) && i < limit; i++ {
		out = append(out, Match{Name: found[i].c.Name, Kind: found[i].c.Kind, Description: found[i].c.Description})
	}
	return out
}

var camel = regexp.MustCompile(`([a-z])([A-Z])`)

func splitCamel(s string) string { return camel.ReplaceAllString(s, "$1 $2") }

// tokens is text's words, lower case and singular, without stop words.
func tokens(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if stop[w] {
			continue
		}
		out = append(out, stem(w))
	}
	return out
}

// stem makes a plural singular, roughly: "messages" and "message" match,
// as do "deliveries" and "delivery".
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 4 && (strings.HasSuffix(w, "sses") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "xes")):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us"):
		return w[:len(w)-1]
	}
	return w
}

func set(ws []string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

var tag = regexp.MustCompile(`<([A-Z][A-Za-z]*)`)

// Dashboard is an example as a whole dashboard: a <script setup> that
// imports from "harbor" the components it uses, and the example as its
// template.
func Dashboard(example string) string {
	seen := map[string]bool{}
	var names []string
	for _, m := range tag.FindAllStringSubmatch(example, -1) {
		if _, ok := Find(m[1]); ok && !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return "<script setup lang=\"ts\">\nimport { " + strings.Join(names, ", ") + " } from \"harbor\"\n</script>\n\n<template>\n" + example + "\n</template>\n"
}
