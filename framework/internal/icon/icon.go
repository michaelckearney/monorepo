// Package icon turns an app's icon, one square PNG, into what each platform
// reads from a built app: an .icns file in a macOS bundle, and the resources
// a Windows executable carries its icon in. It is pure Go, so desktop makes
// either on any machine.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
	"golang.org/x/image/draw"
)

// Load decodes the PNG at path. project.Load has checked that it is one.
func Load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// icnsTypes are the images an .icns holds, by the size of each: macOS picks
// the one that fits what it draws, at 1x and 2x.
var icnsTypes = []struct {
	kind string
	size int
}{
	{"icp4", 16},
	{"icp5", 32},
	{"ic11", 32}, // 16@2x
	{"ic12", 64}, // 32@2x
	{"ic07", 128},
	{"ic08", 256},
	{"ic13", 256}, // 128@2x
	{"ic09", 512},
	{"ic14", 512}, // 256@2x
	{"ic10", 1024},
}

// ICNS is img as a macOS .icns file: a PNG at every size macOS uses.
func ICNS(img image.Image) ([]byte, error) {
	pngs := map[int][]byte{}
	var body bytes.Buffer
	for _, t := range icnsTypes {
		data, ok := pngs[t.size]
		if !ok {
			var buf bytes.Buffer
			if err := png.Encode(&buf, resize(img, t.size)); err != nil {
				return nil, err
			}
			data = buf.Bytes()
			pngs[t.size] = data
		}
		body.WriteString(t.kind)
		binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// resize scales img to size x size.
func resize(img image.Image, size int) image.Image {
	if b := img.Bounds(); b.Dx() == size && b.Dy() == size {
		return img
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}

// WindowsInfo is what a Windows executable says about itself in Explorer.
type WindowsInfo struct {
	Name     string // the app's name in its mode
	Version  string // such as 1.2.0
	FileName string // such as harbor.exe
}

// WriteWindowsResources writes, for arch (amd64 or arm64), the object file
// that gives a Windows executable its icon, version information, and
// manifest. Go links any rsrc_windows_<arch>.syso in the package it builds.
// img may be nil, for no icon.
func WriteWindowsResources(w io.Writer, img image.Image, info WindowsInfo, arch string) error {
	rs := &winres.ResourceSet{}
	if img != nil {
		ico, err := winres.NewIconFromResizedImage(img, []int{256, 128, 64, 48, 32, 24, 16})
		if err != nil {
			return err
		}
		// Explorer shows the executable's first icon group.
		if err := rs.SetIcon(winres.ID(1), ico); err != nil {
			return err
		}
	}
	vi := version.Info{}
	vi.SetFileVersion(info.Version)
	vi.SetProductVersion(info.Version)
	for key, value := range map[string]string{
		version.ProductName:      info.Name,
		version.FileDescription:  info.Name,
		version.OriginalFilename: info.FileName,
	} {
		if err := vi.Set(version.LangDefault, key, value); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(vi)
	// As Wails' own Windows template: sharp at any display scaling, and the
	// current look for the few native controls a window has.
	rs.SetManifest(winres.AppManifest{
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
	})
	return rs.WriteObject(w, winres.Arch(arch))
}
