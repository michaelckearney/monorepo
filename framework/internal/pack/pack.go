// Package pack makes a build's apps into release assets: each app zipped,
// each other file copied, named for its platform, and SHA256SUMS.
package pack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michaelckearney/monorepo/framework/internal/project"
)

// Pack makes each target a release asset, in build/release/, and writes
// their SHA256SUMS:
//
//	build/darwin-arm64/Harbor Dev.app  ->  harbor-dev-darwin-arm64.zip
//	build/linux-amd64/harbor           ->  harbor-linux-amd64
//	build/windows-amd64/harbor.exe     ->  harbor-windows-amd64.exe
func Pack(root string, targets []string) ([]string, error) {
	dir := filepath.Join(root, "build", "release")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var assets []string
	for _, t := range targets {
		src := filepath.Join(root, t)
		platform := filepath.Base(filepath.Dir(src))
		base := filepath.Base(src)
		ext := filepath.Ext(base)
		name := project.Slug(strings.TrimSuffix(base, ext)) + "-" + platform
		var err error
		switch ext {
		case ".app":
			name += ".zip"
			err = zipDir(src, filepath.Join(dir, name))
		default:
			name += ext
			err = copyFile(src, filepath.Join(dir, name))
		}
		if err != nil {
			return nil, err
		}
		assets = append(assets, filepath.Join(dir, name))
	}
	sort.Strings(assets)
	sums, err := sha256sums(assets)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(path, []byte(sums), 0o644); err != nil {
		return nil, err
	}
	return append(assets, path), nil
}

// sha256sums is the shasum -a 256 listing of files, by base name.
func sha256sums(files []string) (string, error) {
	var b strings.Builder
	for _, f := range files {
		in, err := os.Open(f)
		if err != nil {
			return "", err
		}
		h := sha256.New()
		_, err = io.Copy(h, in)
		in.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), filepath.Base(f))
	}
	return b.String(), nil
}

// zipDir zips the folder src, as its own name at the top of the archive,
// the way ditto -c -k --keepParent does, keeping each file's mode, so the
// app's executable is still one once unzipped.
func zipDir(src, to string) error {
	f, err := os.Create(to)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	parent := filepath.Dir(src)
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			h.Name += "/"
			_, err = zw.CreateHeader(h)
			return err
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			w, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			_, err = io.WriteString(w, target)
			return err
		}
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
