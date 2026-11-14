package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRelPathsAreFromTheRoot(t *testing.T) {
	in := "../../backend/x.go:3:1: undefined: y\n# example.com/app\n(../../cli/main.go:4) and ../../../desktop/data.go:9\n"
	want := "backend/x.go:3:1: undefined: y\n# example.com/app\n(cli/main.go:4) and ../desktop/data.go:9\n"
	if got := string(RelPaths([]byte(in))); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The pinned Wails module has every file of the runtime the generated
// configs name. A Wails upgrade that moves them fails here, not in a
// client's build.
func TestWailsRuntimeIsInThePinnedModule(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the Wails module, which go may download")
	}
	dir, err := WailsRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.ts", "global.d.ts", filepath.Join("plugins", "vite.ts")} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
}

// A desktop built from a checkout finds the checkout, as this test binary
// does.
func TestDesktopSourceIsThisCheckout(t *testing.T) {
	want, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if got := DesktopSource(); got != want {
		t.Errorf("DesktopSource() = %q, want %q", got, want)
	}
}
