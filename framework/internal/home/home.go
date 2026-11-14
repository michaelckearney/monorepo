// Package home finds ~/.desktop, where desktop keeps what every project
// shares: its tools, the browsers they drive, and downloaded dependencies.
// Everything in it can be downloaded again, so deleting it is always safe.
package home

import (
	"os"
	"path/filepath"
)

// Dir is ~/.desktop, or $DESKTOP_HOME when that is set. The override is for
// desktop's own tests, which must not fill the real cache.
func Dir() (string, error) {
	if d := os.Getenv("DESKTOP_HOME"); d != "" {
		return d, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".harness", "desktop"), nil
}

// Path is a path inside Dir.
func Path(elem ...string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{d}, elem...)...), nil
}

// BrowsersEnv points Playwright at ~/.desktop/browsers instead of its own
// default in the OS cache folder, so all of desktop's downloads are in one
// place.
func BrowsersEnv() ([]string, error) {
	d, err := Path("browsers")
	if err != nil {
		return nil, err
	}
	return []string{"PLAYWRIGHT_BROWSERS_PATH=" + d}, nil
}
