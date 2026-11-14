package favicon

import "strings"

// colorSchemes records which prefers-color-scheme values a media query admits.
// A link with neither is never a candidate; a link with both is scheme-neutral,
// which is what an absent media attribute means.
type colorSchemes struct {
	light bool
	dark  bool
}

func (c colorSchemes) matches(dark bool) bool {
	if dark {
		return c.dark
	}
	return c.light
}

// matchColorSchemes decides, for each color scheme, whether a <link>'s media
// attribute would let Chromium consider it.
//
// Chromium evaluates the attribute as a real media query against the live
// document (Document::IconURLs), which it can do because it has a rendering
// engine. We have an HTTP client. Rather than grow a media-query engine for a
// decorative cache, this reads only prefers-color-scheme and treats every other
// feature as matching.
//
// Over-matching is the deliberate direction of the error: a query we cannot
// evaluate keeps its icon as a candidate rather than discarding it, so an
// unusual media attribute costs a slightly worse icon instead of no icon.
func matchColorSchemes(media string) colorSchemes {
	trimmed := strings.TrimSpace(media)
	if trimmed == "" {
		return colorSchemes{light: true, dark: true}
	}
	// A media query list matches when any single query in it matches, so the
	// results union.
	var result colorSchemes
	for _, query := range strings.Split(strings.ToLower(trimmed), ",") {
		matched := matchQueryColorSchemes(query)
		result.light = result.light || matched.light
		result.dark = result.dark || matched.dark
	}
	return result
}

func matchQueryColorSchemes(query string) colorSchemes {
	query = strings.TrimSpace(query)
	// "not" negates the whole query, so it is read before the features are.
	negated := query == "not" || strings.HasPrefix(query, "not ") || strings.HasPrefix(query, "not(")
	if negated {
		query = strings.TrimSpace(strings.TrimPrefix(query, "not"))
	}

	requested, found := requestedColorScheme(query)
	if !found {
		// No color-scheme condition: the query says nothing about the split, so
		// it admits both schemes — or, negated, neither.
		return colorSchemes{light: !negated, dark: !negated}
	}
	matched := colorSchemes{light: requested == "light", dark: requested == "dark"}
	if negated {
		return colorSchemes{light: !matched.light, dark: !matched.dark}
	}
	return matched
}

// requestedColorScheme pulls the value out of the first
// "(prefers-color-scheme: value)" condition in a lowercased query.
//
// A query naming a value CSS does not define — a future keyword, or a typo —
// reports not-found, which leaves the link scheme-neutral rather than dropping
// it from both passes.
func requestedColorScheme(query string) (string, bool) {
	const feature = "prefers-color-scheme"
	index := strings.Index(query, feature)
	if index < 0 {
		return "", false
	}
	rest := query[index+len(feature):]
	_, value, found := strings.Cut(rest, ":")
	if !found {
		// The boolean form, "(prefers-color-scheme)", is true whenever the
		// feature exists at all, which is in both schemes.
		return "", false
	}
	value, _, _ = strings.Cut(value, ")")
	switch strings.TrimSpace(value) {
	case "light":
		return "light", true
	case "dark":
		return "dark", true
	}
	return "", false
}
