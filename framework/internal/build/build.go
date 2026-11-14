// Package build implements `harness build`: a production build of the app,
// one for each mode, for every platform desktop supports, written to build/
// in the project, a folder per platform:
//
//	build/darwin-arm64/Harbor.app   macOS: an .app bundle, so the app has its
//	                                own name and icon in the Dock and its own
//	                                settings
//	build/linux-amd64/harbor        Linux: a single executable
//	build/windows-amd64/harbor.exe  Windows: a single executable
//
// Every platform is built from any machine: the app is compiled with no cgo,
// using Wails' purego backend, which loads each system's UI libraries when
// the app starts. None of the builds is signed.
//
// Each binary embeds its platform's download of every dependency, so it
// needs nothing beside it: the app unpacks each one into its data folder
// the first time it asks.
package build

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"

	"github.com/michaelckearney/monorepo/framework/internal/deps"
	"github.com/michaelckearney/monorepo/framework/internal/generate"
	"github.com/michaelckearney/monorepo/framework/internal/gitversion"
	"github.com/michaelckearney/monorepo/framework/internal/icon"
	"github.com/michaelckearney/monorepo/framework/internal/project"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// Run builds the app in each mode for every platform and returns where it
// put them, relative to the project. A platform that fails does not stop
// the others: Run builds what it can, then returns an error that names each
// failure.
//
// version is the app's version, such as 0.5.0. With "", it is the one git
// gives HEAD (gitversion.Build), such as 0.5.0-dev.abc1234: only desktop
// release builds with a version of its own.
func Run(ctx context.Context, dir, version string) ([]string, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, steps.Fail("harness build supports macOS and Linux; this is %s", runtime.GOOS)
	}
	if err := steps.CheckTools(); err != nil {
		return nil, err
	}
	root, err := project.Find(dir)
	if err != nil {
		return nil, err
	}
	p, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	if version == "" {
		if version, err = gitversion.Build(ctx, root); err != nil {
			return nil, err
		}
	}
	steps.Logf("building version %s", version)
	if err := steps.Packages(ctx, p); err != nil {
		return nil, err
	}
	wails, err := steps.Wails3(ctx)
	if err != nil {
		return nil, err
	}
	paths, err := steps.Prepare(ctx, p, wails)
	if err != nil {
		return nil, err
	}

	// build/ is desktop's alone, and emptied first, so nothing in it is left
	// from an earlier build, as a platform that failed this time would be.
	out := filepath.Join(root, "build")
	if err := os.RemoveAll(out); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	// Like .desktop/, build/ keeps itself out of git, so a client repository
	// needs no .gitignore entry for it.
	if err := os.WriteFile(filepath.Join(out, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		return nil, err
	}
	defer generate.EmptyEmbeddedDeps(paths)

	var targets, failed []string
	for _, name := range p.Config.ModeNames() {
		mode := p.Config.Mode(name)
		steps.Logf("building the frontend (%s)", name)
		if err := steps.ViteBuild(ctx, p, paths, mode); err != nil {
			return targets, err
		}
		art, err := loadIcon(mode)
		if err != nil {
			return targets, err
		}
		// Each platform builds in a copy of the app of its own, which
		// embeds that platform's downloads, so the platforms build side by
		// side, as many at once as there are CPUs.
		type result struct {
			target string
			err    error
		}
		results := make([]result, len(project.Platforms))
		sem := make(chan struct{}, runtime.NumCPU())
		var wg sync.WaitGroup
		for i, platform := range project.Platforms {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				steps.Logf("building the backend (%s, %s)", name, platform)
				target, err := buildTarget(ctx, p, paths, mode, version, platform, art, out)
				results[i] = result{target, err}
			}()
		}
		wg.Wait()
		for i, r := range results {
			platform := project.Platforms[i]
			if r.err != nil {
				if ctx.Err() != nil {
					return targets, ctx.Err()
				}
				var pe *project.Error
				if !errors.As(r.err, &pe) {
					return targets, r.err
				}
				steps.Logf("%s for %s failed: %s", mode.Name, platform, pe.Msg)
				failed = append(failed, mode.Name+" for "+platform)
				continue
			}
			rel, _ := filepath.Rel(root, r.target)
			targets = append(targets, rel)
		}
	}
	if len(failed) > 0 {
		return targets, steps.Fail("built %d of %d; these failed, for the reasons above: %s",
			len(targets), len(targets)+len(failed), strings.Join(failed, ", "))
	}
	return targets, nil
}

// art is a mode's icon, in each form a platform reads it.
type art struct {
	img  image.Image
	icns []byte
}

func loadIcon(mode project.Mode) (art, error) {
	if mode.Icon == "" {
		return art{}, nil
	}
	img, err := icon.Load(mode.Icon)
	if err != nil {
		return art{}, err
	}
	icns, err := icon.ICNS(img)
	return art{img: img, icns: icns}, err
}

// buildTarget builds mode for platform, such as linux/arm64, into its
// folder in out, and returns what it made. It builds in a copy of the app,
// beside it so the module's relative replaces still hold, which it removes
// after: the copy embeds this platform's downloads, and another platform's
// build may be running in its own copy at the same time.
func buildTarget(ctx context.Context, p *project.Project, paths generate.Paths, mode project.Mode, version, platform string, a art, out string) (string, error) {
	goos, goarch, _ := strings.Cut(platform, "/")
	own := paths
	own.AppDir = paths.AppDir + "-" + goos + "-" + goarch
	own.EmbeddedDepsDir = filepath.Join(own.AppDir, filepath.Base(paths.EmbeddedDepsDir))
	if err := os.RemoveAll(own.AppDir); err != nil {
		return "", err
	}
	defer os.RemoveAll(own.AppDir)
	if err := copyApp(paths.AppDir, own.AppDir, paths.EmbeddedDepsDir); err != nil {
		return "", err
	}
	if err := embedDeps(ctx, p, own, platform); err != nil {
		return "", err
	}
	if goos == "windows" {
		syso := filepath.Join(own.AppDir, "rsrc_windows_"+goarch+".syso")
		if err := writeWindowsResources(syso, mode, version, a, goarch); err != nil {
			return "", err
		}
	}
	bin := steps.AppBinary(paths, mode, "-"+goos+"-"+goarch)
	build := steps.Build{Mode: mode, Tags: []string{"production"}, Release: true, Platform: platform}
	if err := steps.GoBuild(ctx, p, own.AppDir, bin, build); err != nil {
		return "", err
	}
	dir := filepath.Join(out, goos+"-"+goarch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	switch goos {
	case "darwin":
		return bundle(mode, version, bin, dir, a.icns)
	case "windows":
		target := filepath.Join(dir, project.Slug(mode.Name)+".exe")
		return target, os.Rename(bin, target)
	default:
		target := filepath.Join(dir, project.Slug(mode.Name))
		return target, os.Rename(bin, target)
	}
}

// copyApp copies the app's folder, from to to, all but its embedded
// downloads folder, skip, and any platform's Windows resources: the copy's
// build puts in its own.
func copyApp(from, to, skip string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == skip {
			return fs.SkipDir
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(dst, 0o755)
		case strings.HasSuffix(d.Name(), ".syso"):
			return nil
		case d.Type().IsRegular():
			return copyFile(path, dst)
		}
		return nil
	})
}

func writeWindowsResources(path string, mode project.Mode, version string, a art, arch string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	info := icon.WindowsInfo{Name: mode.Name, Version: gitversion.Core(version), FileName: project.Slug(mode.Name) + ".exe"}
	if err := icon.WriteWindowsResources(f, a.img, info, arch); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// embedDeps puts each dependency's download for platform where the
// generated main embeds it, as <name>/<sha256>/<file name>, replacing the
// last platform's.
func embedDeps(ctx context.Context, p *project.Project, paths generate.Paths, platform string) error {
	if err := generate.EmptyEmbeddedDeps(paths); err != nil {
		return err
	}
	downloads, err := deps.Downloads(ctx, p, platform)
	if err != nil {
		return err
	}
	for _, d := range downloads {
		to := filepath.Join(paths.EmbeddedDepsDir, d.Name, d.Sum, d.FileName)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		// A link costs nothing; a copy is for a project on another disk
		// from ~/.desktop.
		if err := os.Link(d.Path, to); err != nil {
			if err := copyFile(d.Path, to); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// iconFile is the icon's name in a bundle's Resources folder.
const iconFile = "icon.icns"

// bundle lays out <Name>.app around the binary, in dir. macOS takes only
// numbers for a bundle's version, so a build such as 0.5.0-dev.abc1234 is
// 0.5.0 there.
func bundle(mode project.Mode, version, bin, dir string, icns []byte) (string, error) {
	app := filepath.Join(dir, mode.Name+".app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		return "", err
	}
	exe := project.Slug(mode.Name)
	if err := os.Rename(bin, filepath.Join(macos, exe)); err != nil {
		return "", err
	}
	plistIcon := ""
	if icns != nil {
		resources := filepath.Join(app, "Contents", "Resources")
		if err := os.MkdirAll(resources, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(resources, iconFile), icns, 0o644); err != nil {
			return "", err
		}
		plistIcon = iconFile
	}
	var plist bytes.Buffer
	err := infoPlist.Execute(&plist, map[string]string{
		"Name":       mode.Name,
		"Executable": exe,
		"ID":         mode.ID,
		"Version":    gitversion.Core(version),
		"Icon":       plistIcon,
	})
	if err != nil {
		return "", err
	}
	return app, os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), plist.Bytes(), 0o644)
}

var infoPlist = template.Must(template.New("Info.plist").Funcs(template.FuncMap{
	"x": func(s string) (string, error) {
		var b bytes.Buffer
		err := xml.EscapeText(&b, []byte(s))
		return b.String(), err
	},
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleName</key>
	<string>{{x .Name}}</string>
	<key>CFBundleDisplayName</key>
	<string>{{x .Name}}</string>
	<key>CFBundleExecutable</key>
	<string>{{x .Executable}}</string>
	<key>CFBundleIdentifier</key>
	<string>{{x .ID}}</string>
	<key>CFBundleVersion</key>
	<string>{{x .Version}}</string>
	<key>CFBundleShortVersionString</key>
	<string>{{x .Version}}</string>
{{- if .Icon}}
	<key>CFBundleIconFile</key>
	<string>{{x .Icon}}</string>
{{- end}}
	<key>LSMinimumSystemVersion</key>
	<string>12.0.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`))
