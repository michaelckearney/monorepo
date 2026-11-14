package deps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/michaelckearney/monorepo/framework/internal/project"
)

// server serves files by path and counts the requests for each.
type server struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func newServer(t *testing.T) *server {
	s := &server{files: map[string][]byte{}, hits: map[string]int{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits[r.URL.Path]++
		body, ok := s.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	old := client
	client = s.Client()
	t.Cleanup(func() { client = old })
	return s
}

func (s *server) put(path string, body []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = body
	return s.URL + path
}

func zipFile(t *testing.T, files map[string]string, links map[string]string) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	for name, dest := range links {
		h := &zip.FileHeader{Name: name}
		h.SetMode(fs.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(dest))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func tgzFile(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}

// testProject is a project with deps and nothing else desktop reads here.
func testProject(t *testing.T, deps map[string]string) *project.Project {
	t.Helper()
	p := &project.Project{Root: t.TempDir()}
	p.DesktopDir = filepath.Join(p.Root, ".desktop")
	p.Config.Deps = map[string]map[string]string{}
	for name, url := range deps {
		p.Config.Deps[name] = map[string]string{Platform(): url}
	}
	return p
}

func ensure(t *testing.T, p *project.Project) error {
	t.Helper()
	return Ensure(context.Background(), p, filepath.Join(p.DesktopDir, "deps"))
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEnsureUnpacksAndLocks(t *testing.T) {
	t.Setenv("DESKTOP_HOME", t.TempDir())
	s := newServer(t)
	p := testProject(t, map[string]string{
		"browser": s.put("/chrome-mac.zip", zipFile(t,
			map[string]string{"chrome/Versions/A/bin": "binary"},
			map[string]string{"chrome/Current": "Versions/A"})),
		"compiler": s.put("/pkg-1.0.tgz", tgzFile(t, map[string]string{"package/lib/tsc": "tsc"})),
		"tool":     s.put("/jq-macos-arm64", []byte("jq")),
	})
	if err := ensure(t, p); err != nil {
		t.Fatal(err)
	}
	deps := filepath.Join(p.DesktopDir, "deps")
	if got := read(t, filepath.Join(deps, "browser", "chrome", "Current", "bin")); got != "binary" {
		t.Errorf("zip with a link: %q", got)
	}
	if got := read(t, filepath.Join(deps, "compiler", "package", "lib", "tsc")); got != "tsc" {
		t.Errorf("tgz: %q", got)
	}
	fi, err := os.Stat(filepath.Join(deps, "tool", "tool"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("a bare download should be executable: %v %v", fi, err)
	}
	lock := read(t, filepath.Join(p.Root, LockFile))
	if n := strings.Count(lock, "https://"); n != 3 {
		t.Errorf("lock has %d URLs, want 3:\n%s", n, lock)
	}

	// A second worktree on the same versions downloads nothing.
	p2 := testProject(t, nil)
	p2.Config.Deps = p.Config.Deps
	if err := os.WriteFile(filepath.Join(p2.Root, LockFile), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensure(t, p2); err != nil {
		t.Fatal(err)
	}
	if s.hits["/jq-macos-arm64"] != 1 {
		t.Errorf("the shared cache was not used: %d downloads", s.hits["/jq-macos-arm64"])
	}

	// Dropping a dependency drops its link and its lock line.
	delete(p.Config.Deps, "tool")
	if err := ensure(t, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(deps, "tool")); err == nil {
		t.Error("the link for a removed dependency is still there")
	}
	if strings.Contains(read(t, filepath.Join(p.Root, LockFile)), "jq-macos-arm64") {
		t.Error("the lock still names a removed dependency")
	}
}

func TestEnsureRefusesAChangedFile(t *testing.T) {
	t.Setenv("DESKTOP_HOME", t.TempDir())
	s := newServer(t)
	p := testProject(t, map[string]string{"tool": s.put("/tool", []byte("v1"))})
	if err := ensure(t, p); err != nil {
		t.Fatal(err)
	}
	// Another machine, with an empty cache, gets different bytes from the
	// same URL.
	t.Setenv("DESKTOP_HOME", t.TempDir())
	s.put("/tool", []byte("v2"))
	err := ensure(t, p)
	if err == nil || !strings.Contains(err.Error(), "does not match desktop.lock") {
		t.Fatalf("got %v, want a lock mismatch", err)
	}
}

func TestEnsureErrors(t *testing.T) {
	t.Setenv("DESKTOP_HOME", t.TempDir())
	s := newServer(t)

	p := testProject(t, nil)
	p.Config.Deps = map[string]map[string]string{"tool": {"plan9/arm": "https://x"}}
	if err := ensure(t, p); err == nil || !strings.Contains(err.Error(), "has no URL for "+Platform()) {
		t.Errorf("missing platform: %v", err)
	}

	p = testProject(t, map[string]string{"tool": s.URL + "/missing"})
	if err := ensure(t, p); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing file: %v", err)
	}

	p = testProject(t, map[string]string{"evil": s.put("/evil.zip", zipFile(t, map[string]string{"../../escape": "x"}, nil))})
	if err := ensure(t, p); err == nil || !strings.Contains(err.Error(), "outside its folder") {
		t.Errorf("escaping entry: %v", err)
	}

	p = testProject(t, map[string]string{"evil": s.put("/link.zip", zipFile(t, nil, map[string]string{"a": "../../etc"}))})
	if err := ensure(t, p); err == nil || !strings.Contains(err.Error(), "link outside its folder") {
		t.Errorf("escaping link: %v", err)
	}
}

// A build for another platform downloads that platform's file, as it came,
// and records its hash, which a later download must then match.
func TestDownloadsForAnotherPlatform(t *testing.T) {
	t.Setenv("DESKTOP_HOME", t.TempDir())
	s := newServer(t)
	p := testProject(t, nil)
	url := s.put("/jq-windows-amd64.exe", []byte("jq for windows"))
	p.Config.Deps = map[string]map[string]string{"jq": {"windows/amd64": url}}

	downloads, err := Downloads(context.Background(), p, "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 || downloads[0].FileName != "jq-windows-amd64.exe" || read(t, downloads[0].Path) != "jq for windows" {
		t.Fatalf("downloads: %+v", downloads)
	}
	if lock := read(t, filepath.Join(p.Root, LockFile)); !strings.Contains(lock, downloads[0].Sum+"  "+url) {
		t.Errorf("desktop.lock does not record the download:\n%s", lock)
	}

	// Kept, so the next build does not download it again.
	if _, err := Downloads(context.Background(), p, "windows/amd64"); err != nil {
		t.Fatal(err)
	}
	if s.hits["/jq-windows-amd64.exe"] != 1 {
		t.Errorf("downloaded %d times, want 1", s.hits["/jq-windows-amd64.exe"])
	}

	// A changed file is refused.
	os.RemoveAll(filepath.Join(os.Getenv("DESKTOP_HOME"), "deps", "downloads"))
	s.put("/jq-windows-amd64.exe", []byte("something else"))
	if _, err := Downloads(context.Background(), p, "windows/amd64"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("changed file: %v", err)
	}

	if _, err := Downloads(context.Background(), p, "linux/arm64"); err == nil || !strings.Contains(err.Error(), "has no URL for linux/arm64") {
		t.Errorf("missing platform: %v", err)
	}
}
