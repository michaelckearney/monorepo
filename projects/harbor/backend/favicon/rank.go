package favicon

import (
	"strconv"
	"strings"
)

// iconRel is the kind of icon a rel attribute declares.
//
// The set is exactly the one Chromium recognizes (LinkRelAttribute): "icon",
// "apple-touch-icon", and "apple-touch-icon-precomposed". Notably absent are
// "mask-icon" and "fluid-icon". A mask-icon is Safari's pinned-tab glyph — a
// monochrome silhouette, not the site's mark — and treating it as a favicon is
// how Grafana's orange logo came out solid black.
type iconRel int

const (
	relFavicon iconRel = iota
	relTouchIcon
)

// desiredPixelSizes is the target Chromium's desktop favicon handler aims at:
// gfx::kFaviconSize (16 CSS px) at each supported scale factor, which is 1x and
// 2x. It is what makes a 32px icon beat a 512px one, rather than the other way
// around.
var desiredPixelSizes = []int{16, 32}

type iconSize struct {
	width  int
	height int
}

// scoreSizes rates a candidate from its sizes attribute alone, mirroring
// FaviconHandler::FaviconCandidate::FromFaviconURL.
//
// The two 1.0 cases are not a shortcut. "any" is an explicit claim to be
// scalable, and Chromium's desktop path deliberately scores an unsized
// candidate "as high as candidates with an ideal explicit size information" so
// that every undeclared icon is still tried. Format plays no part: an SVG has
// no advantage over a PNG here, which is the same choice Chromium makes.
func scoreSizes(sizes []iconSize, hasAny bool) float64 {
	if hasAny || len(sizes) == 0 {
		return 1
	}
	total := 0.0
	for _, desired := range desiredPixelSizes {
		total += bestSizeScore(sizes, desired)
	}
	return total / float64(len(desiredPixelSizes))
}

// bestSizeScore is GetCandidateIndexWithBestScore from
// components/favicon_base/select_favicon_frames.cc, without the index.
//
// The magic constants are Chromium's. What they encode, in order of weight: an
// exact match beats everything; an icon eight times the target or larger is
// pushed to the bottom because oversized favicons are usually a different piece
// of artwork; downscaling beats upscaling; and less scaling beats more.
func bestSizeScore(sizes []iconSize, desired int) float64 {
	for _, size := range sizes {
		if size.width == desired && size.height == desired {
			return 1
		}
	}
	hugeEdge := desired * 8
	best := 0.0
	for i, size := range sizes {
		average := float64(size.width+size.height) / 2
		if average == 0 {
			continue
		}
		var score float64
		switch {
		case size.width >= hugeEdge || size.height >= hugeEdge:
			score = min(1, float64(desired)/average) * 0.01
		case size.width >= desired && size.height >= desired:
			score = float64(desired)/average*0.01 + 0.15
		default:
			score = min(1, average/float64(desired))*0.01 + 0.1
		}
		if i == 0 || score > best {
			best = score
		}
	}
	return best
}

// parseIconSizes reads a sizes attribute, reporting the "any" keyword
// separately because it carries no dimension.
//
// It follows WebIconSizesParser: whitespace-separated tokens, each either "any"
// or WxH with both parts positive integers. A token that does not parse is
// dropped rather than poisoning the rest.
func parseIconSizes(sizes string) ([]iconSize, bool) {
	var parsed []iconSize
	hasAny := false
	for _, token := range strings.Fields(strings.ToLower(sizes)) {
		if token == "any" {
			hasAny = true
			continue
		}
		width, height, found := strings.Cut(token, "x")
		if !found {
			continue
		}
		widthValue, err := strconv.Atoi(width)
		if err != nil || widthValue <= 0 {
			continue
		}
		heightValue, err := strconv.Atoi(height)
		if err != nil || heightValue <= 0 {
			continue
		}
		parsed = append(parsed, iconSize{width: widthValue, height: heightValue})
	}
	return parsed, hasAny
}

// relFor maps a rel attribute to an icon kind.
//
// "shortcut icon" is recognized through its "icon" token, exactly as Chromium
// recognizes it; a bare "shortcut" declares nothing.
func relFor(rel string) (iconRel, bool) {
	kind := relFavicon
	found := false
	// The last recognized token wins, as in LinkRelAttribute's loop, so
	// rel="apple-touch-icon icon" resolves the same way here as in Chromium.
	for _, token := range strings.Fields(strings.ToLower(rel)) {
		switch token {
		case "icon":
			kind, found = relFavicon, true
		case "apple-touch-icon", "apple-touch-icon-precomposed":
			kind, found = relTouchIcon, true
		}
	}
	return kind, found
}
