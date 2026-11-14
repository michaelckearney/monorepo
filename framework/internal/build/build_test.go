package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework/internal/project"
)

func TestBundle(t *testing.T) {
	mode := project.Mode{Mode: "dev", Name: "Notes & Co Dev", ID: "com.example.notes.dev"}
	for _, icns := range [][]byte{[]byte("icns bytes"), nil} {
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		app, err := bundle(mode, "1.2.0-dev.abc1234", bin, dir, icns)
		if err != nil {
			t.Fatal(err)
		}
		if app != filepath.Join(dir, "Notes & Co Dev.app") {
			t.Errorf("bundle at %s", app)
		}
		if raw, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "notes-co-dev")); err != nil || string(raw) != "binary" {
			t.Errorf("executable: %q, %v", raw, err)
		}
		plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"<string>Notes &amp; Co Dev</string>", "<string>com.example.notes.dev</string>", "<string>1.2.0</string>"} {
			if !strings.Contains(string(plist), want) {
				t.Errorf("Info.plist is missing %s:\n%s", want, plist)
			}
		}
		raw, iconErr := os.ReadFile(filepath.Join(app, "Contents", "Resources", iconFile))
		hasKey := strings.Contains(string(plist), "<key>CFBundleIconFile</key>\n\t<string>icon.icns</string>")
		if icns != nil && (iconErr != nil || string(raw) != "icns bytes" || !hasKey) {
			t.Errorf("with an icon: file %q, %v; plist names it: %v", raw, iconErr, hasKey)
		}
		if icns == nil && (iconErr == nil || strings.Contains(string(plist), "CFBundleIconFile")) {
			t.Errorf("with no icon, the bundle has one:\n%s", plist)
		}
		if lint, err := exec.LookPath("plutil"); err == nil {
			if out, err := exec.Command(lint, "-lint", filepath.Join(app, "Contents", "Info.plist")).CombinedOutput(); err != nil {
				t.Errorf("plutil: %s", out)
			}
		}
	}
}
