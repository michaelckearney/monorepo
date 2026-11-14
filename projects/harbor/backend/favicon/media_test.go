package favicon

import "testing"

func TestMatchColorSchemes(t *testing.T) {
	tests := []struct {
		name  string
		media string
		want  colorSchemes
	}{
		{"absent", "", colorSchemes{light: true, dark: true}},
		{"all", "all", colorSchemes{light: true, dark: true}},
		{"dark", "(prefers-color-scheme: dark)", colorSchemes{dark: true}},
		{"light", "(prefers-color-scheme: light)", colorSchemes{light: true}},
		{"no space", "(prefers-color-scheme:dark)", colorSchemes{dark: true}},
		{"mixed case", "(Prefers-Color-Scheme: DARK)", colorSchemes{dark: true}},
		{"anded with screen", "screen and (prefers-color-scheme: dark)", colorSchemes{dark: true}},
		{"negated", "not all and (prefers-color-scheme: dark)", colorSchemes{light: true}},
		{"list", "(prefers-color-scheme: dark), (prefers-color-scheme: light)", colorSchemes{light: true, dark: true}},
		// Features this package cannot evaluate are treated as matching, so the
		// icon stays a candidate under both schemes rather than being dropped.
		{"unevaluable feature", "(min-width: 500px)", colorSchemes{light: true, dark: true}},
		{"unevaluable with dark", "(min-width: 500px) and (prefers-color-scheme: dark)", colorSchemes{dark: true}},
		// A value CSS does not define leaves the link scheme-neutral rather than
		// removing it from both passes.
		{"unknown value", "(prefers-color-scheme: sepia)", colorSchemes{light: true, dark: true}},
		{"boolean form", "(prefers-color-scheme)", colorSchemes{light: true, dark: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchColorSchemes(test.media); got != test.want {
				t.Fatalf("matchColorSchemes(%q) = %+v, want %+v", test.media, got, test.want)
			}
		})
	}
}

func TestRelFor(t *testing.T) {
	tests := []struct {
		rel    string
		want   iconRel
		isIcon bool
	}{
		{"icon", relFavicon, true},
		{"shortcut icon", relFavicon, true},
		{"ICON", relFavicon, true},
		{"apple-touch-icon", relTouchIcon, true},
		{"apple-touch-icon-precomposed", relTouchIcon, true},
		// The rels that produced a black Grafana logo, plus the ones that were
		// never icons in the first place.
		{"mask-icon", 0, false},
		{"fluid-icon", 0, false},
		{"shortcut", 0, false},
		{"stylesheet", 0, false},
		{"", 0, false},
	}
	for _, test := range tests {
		t.Run(test.rel, func(t *testing.T) {
			got, isIcon := relFor(test.rel)
			if isIcon != test.isIcon {
				t.Fatalf("relFor(%q) recognized = %v, want %v", test.rel, isIcon, test.isIcon)
			}
			if isIcon && got != test.want {
				t.Fatalf("relFor(%q) = %v, want %v", test.rel, got, test.want)
			}
		})
	}
}

// The ordering these scores exist to produce: 32 beats 16, and both beat an
// icon large enough that it is probably different artwork.
func TestScoreSizesRanksTowardTheRenderedSize(t *testing.T) {
	score := func(sizes string) float64 {
		parsed, hasAny := parseIconSizes(sizes)
		return scoreSizes(parsed, hasAny)
	}
	if score("32x32") <= score("16x16") {
		t.Fatalf("32x32 (%v) did not beat 16x16 (%v)", score("32x32"), score("16x16"))
	}
	if score("16x16") <= score("512x512") {
		t.Fatalf("16x16 (%v) did not beat 512x512 (%v)", score("16x16"), score("512x512"))
	}
	if score("any") != 1 {
		t.Fatalf(`score("any") = %v, want 1`, score("any"))
	}
	// Chromium's desktop path scores an unsized candidate as high as an ideal
	// one, so that every undeclared icon is still tried.
	if score("") != 1 {
		t.Fatalf(`score("") = %v, want 1`, score(""))
	}
}

func TestParseIconSizes(t *testing.T) {
	parsed, hasAny := parseIconSizes("16x16 32X32 any bogus 0x0 -1x4")
	if !hasAny {
		t.Fatal("hasAny = false, want true")
	}
	want := []iconSize{{16, 16}, {32, 32}}
	if len(parsed) != len(want) {
		t.Fatalf("parsed = %+v, want %+v", parsed, want)
	}
	for i := range want {
		if parsed[i] != want[i] {
			t.Fatalf("parsed = %+v, want %+v", parsed, want)
		}
	}
}
