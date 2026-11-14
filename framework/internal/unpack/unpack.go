// Package unpack writes a dependency's download into a folder. The CLI uses
// it to fill its cache, and a built app to unpack what it embeds.
package unpack

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IsArchive reports whether a download is unpacked into a tree of files,
// rather than kept as one file.
func IsArchive(fileName string) bool {
	lower := strings.ToLower(fileName)
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
}

// Unpack writes the download in r into dir. fileName is the download's name
// in its URL, which says what kind of file it is. An archive is unpacked.
// Anything else is one file, named after the dependency rather than the URL,
// so the backend uses the same name on every platform: a .gz is
// decompressed, and other files are kept as they are. A single file is made
// executable, since a bare download is almost always a program.
func Unpack(r io.ReaderAt, size int64, fileName, depName, dir string) error {
	f := io.NewSectionReader(r, 0, size)
	lower := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return unzip(f, size, dir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		return untar(gz, dir)
	case strings.HasSuffix(lower, ".gz"):
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		return writeFile(filepath.Join(dir, depName), gz, 0o755)
	default:
		return writeFile(filepath.Join(dir, depName), f, 0o755)
	}
}

// target is where an archive entry goes. An entry that would land outside
// dir is refused, so a hostile archive cannot write anywhere else.
func target(dir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the archive has an entry outside its folder: %s", name)
	}
	return filepath.Join(dir, clean), nil
}

// checkLink refuses a symbolic link that points outside dir. Chromium's
// macOS archive is full of links, all inside it.
func checkLink(dir, at, linkTarget string) error {
	if filepath.IsAbs(linkTarget) {
		return fmt.Errorf("the archive has a link to an absolute path: %s", linkTarget)
	}
	resolved := filepath.Join(filepath.Dir(at), linkTarget)
	if rel, err := filepath.Rel(dir, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("the archive has a link outside its folder: %s", linkTarget)
	}
	return nil
}

func unzip(f io.ReaderAt, size int64, dir string) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return err
	}
	for _, zf := range zr.File {
		at, err := target(dir, zf.Name)
		if err != nil {
			return err
		}
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(at, 0o755); err != nil {
				return err
			}
		case mode&fs.ModeSymlink != 0:
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			dest, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := symlink(dir, at, string(dest)); err != nil {
				return err
			}
		default:
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			err = writeFile(at, rc, filePerm(mode))
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func untar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		at, err := target(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(at, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(at, tr, filePerm(h.FileInfo().Mode())); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := symlink(dir, at, h.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			src, err := target(dir, h.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
				return err
			}
			if err := os.Link(src, at); err != nil {
				return err
			}
		}
	}
}

func symlink(dir, at, dest string) error {
	if err := checkLink(dir, at, dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}
	return os.Symlink(dest, at)
}

// filePerm keeps an entry's executable bits and makes it readable. Some
// archives record no permissions at all.
func filePerm(m fs.FileMode) fs.FileMode {
	perm := m.Perm() | 0o644
	if m.Perm()&0o111 != 0 {
		perm |= 0o755
	}
	return perm
}

func writeFile(path string, r io.Reader, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
