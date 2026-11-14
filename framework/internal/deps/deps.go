// Package deps fetches the files an app declares under deps in desktop.yaml.
//
// Each download is kept once, in ~/.desktop/deps/<sha256>/, unpacked; a
// single file is in <sha256>-<name>/, under the dependency's name. A
// project sees its own versions through .desktop/deps/<name>, a link into
// that cache. Keying the cache by content rather than URL lets worktrees on
// different versions run side by side, lets worktrees on the same version
// share one copy, and catches a URL whose file changed under it.
//
// The hash of every download is recorded in desktop.lock, which is
// committed. A later download that does not match it is refused.
package deps

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/michaelckearney/monorepo/framework/internal/home"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/unpack"
)

// LockFile is in the project root, beside desktop.yaml.
const LockFile = "desktop.lock"

// client is replaced by the tests, which serve over TLS with their own
// certificate.
var client = http.DefaultClient

// Logf reports progress. It is set by the caller, so this package does not
// depend on how desktop prints.
var Logf = func(format string, args ...any) {}

// lockMu keeps the platforms' Downloads, which a release runs side by side,
// from writing desktop.lock over each other.
var lockMu sync.Mutex

func fail(format string, args ...any) error {
	return &project.Error{Msg: fmt.Sprintf(format, args...)}
}

// Platform is this machine's key under a dependency, such as darwin/arm64.
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// Ensure makes every dependency available for this platform as
// linkDir/<name>, downloading what the cache does not have. Links for
// dependencies no longer in desktop.yaml are removed.
func Ensure(ctx context.Context, p *project.Project, linkDir string) error {
	lockPath := filepath.Join(p.Root, LockFile)
	lock, err := readLock(lockPath)
	if err != nil {
		return err
	}
	cache, err := home.Path("deps")
	if err != nil {
		return err
	}

	names := make([]string, 0, len(p.Config.Deps))
	for name := range p.Config.Deps {
		names = append(names, name)
	}
	slices.Sort(names)

	if len(names) > 0 {
		if err := os.MkdirAll(linkDir, 0o755); err != nil {
			return err
		}
	}
	for _, name := range names {
		url, ok := p.Config.Deps[name][Platform()]
		if !ok {
			return fail("%s: dependency %s has no URL for %s; add one under it, like %s: https://...", project.ConfigFile, name, Platform(), Platform())
		}
		want := lock[url]
		if want == "" {
			want = pinned(url)
		}
		sum, err := fetch(ctx, cache, name, url, want)
		if err != nil {
			return err
		}
		lock[url] = sum
		if err := link(filepath.Join(cache, cacheKey(sum, name, url)), filepath.Join(linkDir, name)); err != nil {
			return err
		}
	}

	if err := removeStaleLinks(linkDir, names); err != nil {
		return err
	}
	return writeLock(lockPath, lock, p.Config.Deps)
}

// pinned is the sha256 desktop pins for url, such as its Chromium's, or "".
func pinned(url string) string {
	for _, d := range pin.Chromium {
		if d.URL == url {
			return d.SHA256
		}
	}
	return ""
}

// Pinned is the folder of a download desktop pins, such as its Chromium for
// this platform, fetching it into the cache if it is not there. Desktop's own
// tests use it, outside any project.
func Pinned(ctx context.Context, name string, d pin.Download) (string, error) {
	cache, err := home.Path("deps")
	if err != nil {
		return "", err
	}
	sum, err := fetch(ctx, cache, name, d.URL, d.SHA256)
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, cacheKey(sum, name, d.URL)), nil
}

// Download is one dependency's download, as harness build embeds it.
type Download struct {
	Name     string // the dependency's name in desktop.yaml
	Path     string // the downloaded file, in the cache
	FileName string // its name in its URL, which says how to unpack it
	Sum      string // its sha256
}

// Downloads returns the downloaded file of every dependency for platform,
// such as windows/amd64, as it came from its URL, kept in
// ~/.desktop/deps/downloads/. Ensure keeps only the unpacked copy, for this
// machine, so the first build after a download fetches the file again,
// checked against desktop.lock. A URL desktop.lock does not have yet, as
// for a platform other than this one, is recorded there.
func Downloads(ctx context.Context, p *project.Project, platform string) ([]Download, error) {
	lockPath := filepath.Join(p.Root, LockFile)
	lockMu.Lock()
	lock, err := readLock(lockPath)
	lockMu.Unlock()
	if err != nil {
		return nil, err
	}
	// What this call adds to desktop.lock: another platform's call may be
	// adding to it at the same time, so it is merged in at the end.
	added := map[string]string{}
	dir, err := home.Path("deps", "downloads")
	if err != nil {
		return nil, err
	}
	tmpRoot, err := home.Path("tmp")
	if err != nil {
		return nil, err
	}
	var out []Download
	for name, byPlatform := range p.Config.Deps {
		url, ok := byPlatform[platform]
		if !ok {
			if name == pin.ChromiumDep && p.Config.Browser {
				return nil, fail("desktop's Chromium, which browser: true brings, has no build for %s", platform)
			}
			return nil, fail("%s: dependency %s has no URL for %s; add one under it, like %s: https://...", project.ConfigFile, name, platform, platform)
		}
		d := Download{Name: name, FileName: FileName(url), Sum: lock[url]}
		if d.Sum == "" {
			d.Sum = pinned(url)
		}
		if d.Sum != "" {
			d.Path = filepath.Join(dir, d.Sum)
			if _, err := os.Stat(d.Path); err == nil {
				out = append(out, d)
				continue
			}
		}
		sum, err := keepDownload(ctx, name, url, platform, d.Sum, tmpRoot, dir)
		if err != nil {
			return nil, err
		}
		d.Sum, d.Path = sum, filepath.Join(dir, sum)
		added[url] = sum
		out = append(out, d)
	}
	lockMu.Lock()
	defer lockMu.Unlock()
	if lock, err = readLock(lockPath); err != nil {
		return nil, err
	}
	for url, sum := range added {
		lock[url] = sum
	}
	if err := writeLock(lockPath, lock, p.Config.Deps); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b Download) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// keepDownload downloads url into dir, named by its sha256, which it
// returns. want is the sha256 desktop.lock has, or "" for one it does not
// have yet.
func keepDownload(ctx context.Context, name, url, platform, want, tmpRoot, dir string) (string, error) {
	for _, d := range []string{tmpRoot, dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	file, err := os.CreateTemp(tmpRoot, "download-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	Logf("downloading %s for %s, to embed it in the app", name, platform)
	size, sum, err := download(ctx, url, file)
	if err != nil {
		return "", fail("could not download %s from %s: %v", name, url, err)
	}
	if want != "" && sum != want {
		return "", fail("the download of %s does not match %s: the file at %s has changed. If you expected that, delete its line from %s and run desktop again",
			name, LockFile, url, LockFile)
	}
	Logf("downloaded %s for %s (%s)", name, platform, megabytes(size))
	if err := file.Close(); err != nil {
		return "", err
	}
	return sum, os.Rename(file.Name(), filepath.Join(dir, sum))
}

// cacheKey is the download's folder in the cache. A single file is named
// after its dependency, so the name is part of the key: the same file under
// two names is two folders.
func cacheKey(sum, name, url string) string {
	if unpack.IsArchive(FileName(url)) {
		return sum
	}
	return sum + "-" + name
}

// fetch returns the hash of url's content, downloading and unpacking it into
// the cache unless that is already there. want is the hash desktop.lock has
// for url, or "" for a URL it does not have yet.
func fetch(ctx context.Context, cache, name, url, want string) (string, error) {
	if want != "" {
		if _, err := os.Stat(filepath.Join(cache, cacheKey(want, name, url))); err == nil {
			return want, nil
		}
	}
	tmpRoot, err := home.Path("tmp")
	if err != nil {
		return "", err
	}
	// The temporary files sit on the same file system as the cache, so the
	// final rename is a single step that another process never sees half
	// done.
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}

	file, err := os.CreateTemp(tmpRoot, "download-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	Logf("downloading %s for %s", name, Platform())
	size, sum, err := download(ctx, url, file)
	if err != nil {
		return "", fail("could not download %s from %s: %v", name, url, err)
	}
	if want != "" && sum != want {
		return "", fail("the download of %s does not match %s: the file at %s has changed. If you expected that, delete its line from %s and run desktop again",
			name, LockFile, url, LockFile)
	}
	Logf("downloaded %s (%s)", name, megabytes(size))

	final := filepath.Join(cache, cacheKey(sum, name, url))
	if _, err := os.Stat(final); err == nil {
		return sum, nil
	}
	dir, err := os.MkdirTemp(tmpRoot, "unpack-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := unpack.Unpack(file, size, FileName(url), name, dir); err != nil {
		return "", fail("could not unpack %s from %s: %v", name, url, err)
	}
	if err := os.Rename(dir, final); err != nil {
		// Another worktree finished the same download first. Its copy has
		// the same content, so use it.
		if _, statErr := os.Stat(final); statErr == nil {
			return sum, nil
		}
		return "", err
	}
	return sum, nil
}

func download(ctx context.Context, url string, w io.Writer) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("the server answered %s", resp.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), resp.Body)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// FileName is the last part of url's path, which says what kind of file it
// is.
func FileName(url string) string {
	u := url
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	name := path.Base(u)
	if name == "" || name == "/" || name == "." {
		return "download"
	}
	return name
}

func megabytes(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

// link points dst at target, replacing whatever dst was.
func link(target, dst string) error {
	if got, err := os.Readlink(dst); err == nil && got == target {
		return nil
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Symlink(target, dst)
}

func removeStaleLinks(linkDir string, names []string) error {
	entries, err := os.ReadDir(linkDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !slices.Contains(names, e.Name()) {
			if err := os.RemoveAll(filepath.Join(linkDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

const lockHeader = "# Written by desktop. Commit this file.\n" +
	"# The sha256 of each dependency download in desktop.yaml, then its URL.\n"

// readLock reads desktop.lock into a map from URL to sha256.
func readLock(path string) (map[string]string, error) {
	lock := map[string]string{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return lock, nil
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sum, url, ok := strings.Cut(line, " ")
		url = strings.TrimSpace(url)
		if !ok || len(sum) != 64 || url == "" {
			return nil, fail("%s line %d is not \"<sha256> <url>\"; delete the line and run desktop again", LockFile, n)
		}
		lock[url] = sum
	}
	return lock, sc.Err()
}

// writeLock keeps the lines for every URL desktop.yaml still names, on any
// platform: a hash recorded on another machine stays until its URL goes.
func writeLock(path string, lock map[string]string, deps map[string]map[string]string) error {
	var urls []string
	for _, byPlatform := range deps {
		for _, url := range byPlatform {
			if lock[url] != "" && !slices.Contains(urls, url) {
				urls = append(urls, url)
			}
		}
	}
	if len(urls) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	slices.Sort(urls)
	var b strings.Builder
	b.WriteString(lockHeader)
	for _, url := range urls {
		fmt.Fprintf(&b, "%s  %s\n", lock[url], url)
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == b.String() {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
