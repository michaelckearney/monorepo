package backend

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Updates checks for a newer release of this app, and installs it, from
// GitHub with your token. The app is released from its monorepo, tagged slipway/v<version>,
// with the app zipped for each platform; it installs a release by putting
// the release's app where it is, keeping the one it replaces beside it.
type Updates struct{ github *GitHub }

// release is a GitHub release, as its REST API writes it.
type release struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"assets"`
}

// latest is the app's newest release, newest first as GitHub lists them,
// or nil when there is none.
func (u Updates) latest() (*release, error) {
	var list []release
	if err := u.github.rest("GET", "/repos/"+releases+"/releases?per_page=100", nil, &list); err != nil {
		return nil, err
	}
	for _, r := range list {
		if !r.Draft && strings.HasPrefix(r.Tag, tagPrefix) {
			return &r, nil
		}
	}
	return nil, nil
}

// Update is what is installed, and what is released.
type Update struct {
	Current string `json:"current"` // "dev" for a build that is not a release
	Latest  string `json:"latest"`
	Newer   bool   `json:"newer"`
	// Released is whether this app is a release, in an .app bundle. Only a
	// release updates itself: a Slipway Dev that harness run started must
	// not replace the app in /Applications.
	Released bool `json:"released"`
}

// The repository the app is released from, and its tags' prefix.
const (
	releases  = "michaelckearney/monorepo"
	tagPrefix = "slipway/"
)

// bundle is the .app this app runs from, or "".
func bundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(app) != ".app" {
		return ""
	}
	return app
}

var shortVersion = regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]+)</string>`)

// installed is the release version of the app in a bundle, such as v0.2.0,
// or "dev".
func installed(app string) string {
	raw, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return "dev"
	}
	m := shortVersion.FindSubmatch(raw)
	if m == nil || strings.Contains(string(m[1]), "-") {
		return "dev"
	}
	return "v" + string(m[1])
}

// Check compares this app with the latest release.
func (up Updates) Check() (Update, error) {
	app := bundle()
	u := Update{Current: "dev", Released: app != ""}
	if app != "" {
		u.Current = installed(app)
	}
	r, err := up.latest()
	if err != nil {
		return u, fmt.Errorf("could not ask GitHub for the app's releases: %w", err)
	}
	if r != nil {
		u.Latest = strings.TrimPrefix(r.Tag, tagPrefix)
	}
	u.Newer = u.Current != "dev" && u.Latest != "" && newer(u.Latest, u.Current)
	return u, nil
}

// Install puts a release's app in place of this one, keeping this one as
// <name>.previous.app. Open the app again to run the new one.
func (u Updates) Install(version string) error {
	if !strings.HasPrefix(version, "v") {
		return fmt.Errorf("%q is not a release", version)
	}
	app := bundle()
	if app == "" {
		return errors.New("this app is not a release in an .app bundle, so it does not update itself")
	}
	tmp, err := os.MkdirTemp("", "slipway-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	asset := fmt.Sprintf("slipway-darwin-%s.zip", runtime.GOARCH)
	var r release
	if err := u.github.rest("GET", "/repos/"+releases+"/releases/tags/"+tagPrefix+version, nil, &r); err != nil {
		return fmt.Errorf("could not find release %s: %w", version, err)
	}
	url := ""
	for _, a := range r.Assets {
		if a.Name == asset {
			url = a.URL
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no %s", version, asset)
	}
	zip, err := os.Create(filepath.Join(tmp, asset))
	if err != nil {
		return err
	}
	err = u.github.download(url, zip)
	if cerr := zip.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("could not download %s: %w", asset, err)
	}
	if out, err := exec.Command("ditto", "-x", "-k", filepath.Join(tmp, asset), tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("could not unzip %s: %s", asset, strings.TrimSpace(string(out)))
	}
	unpacked, _ := filepath.Glob(filepath.Join(tmp, "*.app"))
	if len(unpacked) != 1 {
		return fmt.Errorf("%s holds no app", asset)
	}
	previous := strings.TrimSuffix(app, ".app") + ".previous.app"
	os.RemoveAll(previous)
	if err := os.Rename(app, previous); err != nil {
		return err
	}
	if err := os.Rename(unpacked[0], app); err != nil {
		os.Rename(previous, app)
		return err
	}
	return nil
}

// Relaunch quits the app and opens it again, once it has quit, so the
// release Install put in its place runs.
func (Updates) Relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// .../Slipway.app/Contents/MacOS/slipway: install.sh moved the new app
	// to the same place.
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(app) != ".app" {
		return errors.New("this app is not in an .app bundle; open it again yourself")
	}
	cmd := exec.Command("/bin/sh", "-c", `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; open "$2"`,
		"relaunch", strconv.Itoa(os.Getpid()), app)
	// Its own process group, so it outlives the app.
	group(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// newer reports whether version a is after b, both vMAJOR.MINOR.PATCH.
func newer(a, b string) bool {
	var x, y [3]int
	fmt.Sscanf(a, "v%d.%d.%d", &x[0], &x[1], &x[2])
	fmt.Sscanf(b, "v%d.%d.%d", &y[0], &y[1], &y[2])
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}
