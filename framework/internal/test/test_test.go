package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michaelckearney/monorepo/framework/internal/project"
)

func TestTestLayoutFindsStrayTests(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"frontend/src/__tests__/App.test.ts",
		"frontend/src/components/__tests__/Button.test.ts",
		"frontend/src/components/Button.vue",
	} {
		write(t, filepath.Join(root, f))
	}
	tr := &tester{p: &project.Project{
		Root:        root,
		FrontendDir: filepath.Join(root, "frontend"),
		TestsDir:    filepath.Join(root, "tests"),
		Config:      project.Config{Modes: map[string]project.ModeConfig{"dev": {}, "prod": {}}, DefaultMode: "prod"},
	}}
	if !tr.testLayout() {
		t.Fatal("tests inside __tests__ were reported as stray")
	}
	write(t, filepath.Join(root, "frontend/src/components/Button.test.ts"))
	if tr.testLayout() {
		t.Fatal("a test outside __tests__ was not reported")
	}
}

func TestTestLayoutFindsE2ETestsOutsideAMode(t *testing.T) {
	root := t.TempDir()
	tr := &tester{p: &project.Project{
		Root:        root,
		FrontendDir: filepath.Join(root, "frontend"),
		TestsDir:    filepath.Join(root, "tests"),
		Config:      project.Config{Modes: map[string]project.ModeConfig{"dev": {}, "prod": {}}, DefaultMode: "prod"},
	}}
	// Helpers outside a mode are fine; only tests there would never run.
	for _, f := range []string{"tests/dev/chats_test.go", "tests/prod/a/b_test.go", "tests/helpers/page.go"} {
		write(t, filepath.Join(root, f))
	}
	if !tr.testLayout() {
		t.Fatal("tests in mode folders were reported as stray")
	}
	for _, f := range []string{"tests/devv/chats_test.go", "tests/top_test.go"} {
		write(t, filepath.Join(root, f))
		if tr.testLayout() {
			t.Errorf("%s was not reported", f)
		}
		os.Remove(filepath.Join(root, f))
	}
}

func TestTestFileGetsItsDirectory(t *testing.T) {
	got := testFile.ReplaceAllString("    greeter_test.go:8: first greeting\n", "${1}backend/${2}")
	if want := "    backend/greeter_test.go:8: first greeting\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
