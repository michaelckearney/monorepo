package steps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michaelckearney/monorepo/framework/internal/branches"
)

// Each checkout's node_modules is a link into the package cache, to the
// install for its package.json and package-lock.json:
//
//	.desktop/packages/<key>/node_modules   one install per pair of files
//	.desktop/packages/<key>/stamp          what npm left, when it finished
//
// The cache is in the user's own checkout, and the sidecar's worktrees share
// it, so a branch whose packages match one already installed switches at
// once, and a branch seen before switches back at once. A cache entry is
// never changed by desktop once made, so it can be reused for as long as it
// is there.

// ensureNodeModules makes root/node_modules the cached install for root's
// package files, running npm when the cache does not have it.
func ensureNodeModules(ctx context.Context, root string) error {
	cache := filepath.Join(packagesHome(root), ".desktop", "packages")
	key, err := packageKey(root)
	if err != nil {
		return err
	}
	link := filepath.Join(root, "node_modules")
	entry := filepath.Join(cache, key)
	if valid(entry) {
		return linkTo(link, entry)
	}

	// Not cached: npm installs in root, as it would without desktop, into a
	// real node_modules, which then moves into the cache. npm starts from
	// what is there, so changes are quick.
	fi, err := os.Lstat(link)
	switch {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		// The link's entry, if npm changed it in place, as `npm install
		// <package>` in the checkout does, is the best start; it no longer
		// matches its key, so it leaves the cache. An entry that does
		// match stays, for the checkouts that use it.
		target, _ := os.Readlink(link)
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		old := filepath.Dir(target)
		if err := os.Remove(link); err != nil {
			return err
		}
		if inside(cache, old) && !valid(old) {
			if err := os.Rename(target, link); err != nil && !os.IsNotExist(err) {
				return err
			}
			_ = os.RemoveAll(old)
		}
	case err == nil && !fi.IsDir():
		if err := os.Remove(link); err != nil {
			return err
		}
	}

	Logf("installing packages (npm install)")
	cmd := exec.CommandContext(ctx, "npm", "install", "--no-fund", "--no-audit")
	cmd.Dir = root
	cmd.Stdout = NewPrefixWriter("[npm] ", os.Stdout)
	cmd.Stderr = NewPrefixWriter("[npm] ", os.Stderr)
	if err := cmd.Run(); err != nil {
		return Fail("npm install failed; fix the errors above, then run desktop again")
	}
	// npm may have rewritten package-lock.json; the entry is for the files
	// as they are now.
	if key, err = packageKey(root); err != nil {
		return err
	}
	entry = filepath.Join(cache, key)
	if err := store(link, entry); err != nil {
		return err
	}
	return linkTo(link, entry)
}

// packagesHome is the user's own checkout of the project at root, whose
// .desktop holds the cache: root itself, unless root is a worktree.
func packagesHome(root string) string {
	repo, err := branches.Open(root)
	if err != nil {
		return root
	}
	// A link in the checkout is not a change to it.
	_ = repo.Exclude("node_modules")
	return repo.Home
}

// packageKey names the install for root's package.json and
// package-lock.json.
func packageKey(root string) (string, error) {
	h := sha256.New()
	for _, f := range []string{"package.json", "package-lock.json"} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		h.Write([]byte(f + "\x00"))
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:20], nil
}

// npmStamp is the file npm rewrites in node_modules on every install.
const npmStamp = ".package-lock.json"

// valid reports whether entry holds a whole install that npm has not
// changed since: its stamp, written when it was made, matches what npm
// keeps in node_modules.
func valid(entry string) bool {
	want, err := os.ReadFile(filepath.Join(entry, "stamp"))
	if err != nil {
		return false
	}
	have, err := os.ReadFile(filepath.Join(entry, "node_modules", npmStamp))
	if err != nil {
		return false
	}
	return string(want) == stampOf(have)
}

func stampOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// store moves the install at link into entry, replacing what is there.
func store(link, entry string) error {
	have, err := os.ReadFile(filepath.Join(link, npmStamp))
	if errors.Is(err, os.ErrNotExist) {
		// npm writes no stamp for a project with no dependencies.
		if err := os.MkdirAll(link, 0o755); err != nil {
			return err
		}
		have = []byte("{}\n")
		if err := os.WriteFile(filepath.Join(link, npmStamp), have, 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.RemoveAll(entry); err != nil {
		return err
	}
	if err := os.MkdirAll(entry, 0o755); err != nil {
		return err
	}
	if err := os.Rename(link, filepath.Join(entry, "node_modules")); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(entry, "stamp"), []byte(stampOf(have)), 0o644)
}

// linkTo makes link point at entry's node_modules, replacing whatever link
// is. A real node_modules, from before desktop cached packages, matches
// entry, so it is only removed.
func linkTo(link, entry string) error {
	target := filepath.Join(entry, "node_modules")
	if have, err := os.Readlink(link); err == nil && have == target {
		return nil
	}
	if err := os.RemoveAll(link); err != nil {
		return err
	}
	return os.Symlink(target, link)
}

func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}
