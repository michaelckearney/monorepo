package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func square(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	return img
}

// Every entry is a PNG of the size its type says, and the lengths add up.
func TestICNS(t *testing.T) {
	data, err := ICNS(square(1024))
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "icns" || int(binary.BigEndian.Uint32(data[4:8])) != len(data) {
		t.Fatalf("bad header: % x, length %d", data[:8], len(data))
	}
	got := map[string]int{}
	for pos := 8; pos < len(data); {
		kind := string(data[pos : pos+4])
		n := int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		cfg, err := png.DecodeConfig(bytes.NewReader(data[pos+8 : pos+n]))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if cfg.Width != cfg.Height {
			t.Errorf("%s is %dx%d", kind, cfg.Width, cfg.Height)
		}
		got[kind] = cfg.Width
		pos += n
	}
	for _, want := range icnsTypes {
		if got[want.kind] != want.size {
			t.Errorf("%s: %d pixels, want %d", want.kind, got[want.kind], want.size)
		}
	}
}

// The object file is COFF for the machine asked for, with or without an icon.
func TestWriteWindowsResources(t *testing.T) {
	info := WindowsInfo{Name: "Demo", Version: "1.2.0", FileName: "demo.exe"}
	for arch, machine := range map[string]uint16{"amd64": 0x8664, "arm64": 0xaa64} {
		for _, img := range []image.Image{square(1024), nil} {
			var buf bytes.Buffer
			if err := WriteWindowsResources(&buf, img, info, arch); err != nil {
				t.Fatal(err)
			}
			if got := binary.LittleEndian.Uint16(buf.Bytes()[:2]); got != machine {
				t.Errorf("%s: machine %#x, want %#x", arch, got, machine)
			}
		}
	}
}
