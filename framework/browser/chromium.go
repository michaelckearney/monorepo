package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Executable is Chromium's program in dir, the unpacked download of the
// Chromium desktop pins. Each platform's download lays it out differently.
func Executable(dir string) (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		for _, arch := range []string{"arm64", "x64"} {
			candidates = append(candidates, filepath.Join("chrome-mac-"+arch, "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing"))
		}
	case "linux":
		candidates = []string{filepath.Join("chrome-linux64", "chrome"), filepath.Join("chrome-linux", "chrome")}
	case "windows":
		candidates = []string{filepath.Join("chrome-win64", "chrome.exe")}
	}
	for _, c := range candidates {
		path := filepath.Join(dir, c)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no Chromium program in %s for %s", dir, runtime.GOOS)
}

// userAgent is what Chromium says to sites, once it is started headless:
// Chrome's own reduced User-Agent, with this Chromium's major version.
func (b *Browser) userAgent() (string, error) {
	b.agentOnce.Do(func() { b.agent, b.agentErr = userAgent(b.exec) })
	return b.agent, b.agentErr
}

func userAgent(program string) (string, error) {
	version, err := chromiumVersion(program)
	if err != nil {
		return "", err
	}
	major, _, _ := strings.Cut(version, ".")
	// The platforms Chrome's reduced User-Agent names, whatever the
	// machine's own version.
	platform := "X11; Linux x86_64"
	switch runtime.GOOS {
	case "darwin":
		platform = "Macintosh; Intel Mac OS X 10_15_7"
	case "windows":
		platform = "Windows NT 10.0; Win64; x64"
	}
	return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36", platform, major), nil
}

// versionManifest is the file Chromium on Windows keeps beside chrome.exe,
// named after its version, such as 151.0.7922.34.manifest.
var versionManifest = regexp.MustCompile(`^\d+(\.\d+){3}\.manifest$`)

// chromiumVersion is the version of the Chromium at program, such as
// 151.0.7922.34.
func chromiumVersion(program string) (string, error) {
	if runtime.GOOS == "windows" {
		// chrome.exe --version prints nothing on Windows.
		return manifestVersion(filepath.Dir(program))
	}
	// Prints such as "Google Chrome for Testing 151.0.7922.34".
	out, err := exec.Command(program, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("could not read Chromium's version: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("Chromium printed no version")
	}
	return fields[len(fields)-1], nil
}

// manifestVersion is the version in the name of the <version>.manifest in
// dir, as Chromium on Windows has beside chrome.exe.
func manifestVersion(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("could not read Chromium's version: %w", err)
	}
	for _, e := range entries {
		if versionManifest.MatchString(e.Name()) {
			return strings.TrimSuffix(e.Name(), ".manifest"), nil
		}
	}
	return "", fmt.Errorf("could not read Chromium's version: no <version>.manifest in %s", dir)
}
