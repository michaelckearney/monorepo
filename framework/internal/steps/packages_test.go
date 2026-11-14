package steps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeNPM puts an npm on PATH that writes node_modules/.package-lock.json
// from package.json, and counts its runs in count.
func fakeNPM(t *testing.T) (count string) {
	bin := t.TempDir()
	count = filepath.Join(bin, "count")
	script := "#!/bin/sh\necho x >> " + count + "\nmkdir -p node_modules && cp package.json node_modules/.package-lock.json\n"
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return count
}

func runs(t *testing.T, count string) int {
	b, _ := os.ReadFile(count)
	return strings.Count(string(b), "x")
}

// Two package.json files are two installs, each made once, and switching
// between them only moves the link.
func TestPackagesAreCachedPerPackageFiles(t *testing.T) {
	count := fakeNPM(t)
	root := t.TempDir()
	write := func(v string) {
		os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"v":"`+v+`"}`), 0o644)
	}
	ctx := context.Background()
	for i, v := range []string{"1", "2", "1", "2"} {
		write(v)
		if err := ensureNodeModules(ctx, root); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Lstat(filepath.Join(root, "node_modules"))
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("node_modules is not a link: %v %v", fi, err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "node_modules", npmStamp))
		if want := `{"v":"` + v + `"}`; string(got) != want {
			t.Errorf("step %d: node_modules is for %s, want %s", i, got, want)
		}
	}
	if n := runs(t, count); n != 2 {
		t.Errorf("npm ran %d times, want 2", n)
	}
}

// A real node_modules from before the cache is adopted, not reinstalled
// from nothing, and an entry npm changed in place is not reused.
func TestPackagesAdoptAndInvalidate(t *testing.T) {
	count := fakeNPM(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{}`), 0o644)
	os.MkdirAll(filepath.Join(root, "node_modules", "kept"), 0o755)
	ctx := context.Background()
	if err := ensureNodeModules(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", "kept")); err != nil {
		t.Errorf("the old node_modules was not kept: %v", err)
	}
	// As `npm install <package>` does through the link.
	os.WriteFile(filepath.Join(root, "node_modules", npmStamp), []byte("changed"), 0o644)
	if err := ensureNodeModules(ctx, root); err != nil {
		t.Fatal(err)
	}
	if n := runs(t, count); n != 2 {
		t.Errorf("npm ran %d times, want 2", n)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", "kept")); err != nil {
		t.Errorf("the changed install was not the start of the new one: %v", err)
	}
}
