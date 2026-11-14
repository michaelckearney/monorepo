package branches

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo is a repository with main checked out, a branch feature/x one
// commit ahead, and the project in app/.
func repo(t *testing.T) string {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	os.WriteFile(filepath.Join(dir, "app", "f"), []byte("main\n"), 0o644)
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "first")
	run(t, dir, "checkout", "-q", "-b", "feature/x")
	os.WriteFile(filepath.Join(dir, "app", "f"), []byte("feature\n"), 0o644)
	run(t, dir, "commit", "-q", "-am", "the feature")
	run(t, dir, "checkout", "-q", "main")
	return dir
}

func TestListPutsTheCheckoutFirst(t *testing.T) {
	dir := repo(t)
	r, err := Open(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Rel != "app" {
		t.Errorf("Rel = %q", r.Rel)
	}
	list, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || !list[0].Home || list[0].Name != "main" || list[0].Key != "" {
		t.Fatalf("list = %+v", list)
	}
	if b := list[1]; b.Name != "feature/x" || b.Key != "feature-x" || b.Subject != "the feature" {
		t.Errorf("branch = %+v", b)
	}
}

// A branch is checked out beside the user's checkout, which does not change,
// and a second checkout of it moves its worktree to the branch's new commit.
func TestCheckoutLeavesTheUsersCheckoutAlone(t *testing.T) {
	dir := repo(t)
	os.WriteFile(filepath.Join(dir, "app", "f"), []byte("uncommitted\n"), 0o644)
	r, _ := Open(filepath.Join(dir, "app"))
	list, _ := r.List(context.Background())
	got, err := r.Checkout(context.Background(), list[1])
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.Home, ".desktop", "worktrees", "feature-x", "app"); got != want {
		t.Errorf("Checkout = %s, want %s", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(got, "f")); string(b) != "feature\n" {
		t.Errorf("worktree has %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "app", "f")); string(b) != "uncommitted\n" {
		t.Errorf("the user's checkout has %q", b)
	}

	// A worktree knows the user's checkout as home.
	wr, err := Open(got)
	if err != nil || wr.Home != r.Home {
		t.Errorf("Open(worktree).Home = %v, %v; want %s", wr, err, r.Home)
	}

	run(t, dir, "branch", "-f", "feature/x", "main")
	list, _ = r.List(context.Background())
	if _, err := r.Checkout(context.Background(), list[1]); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(got, "f")); string(b) != "main\n" {
		t.Errorf("worktree after the branch moved has %q", b)
	}
}

func TestPRsGoOnTheirBranches(t *testing.T) {
	prs, err := parsePRs([]byte(`[
	  {"number":3,"title":"Feature","url":"https://github.com/o/r/pull/3","headRefName":"feature/x","author":{"login":"ann"},
	   "statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"state":"PENDING"}]},
	  {"number":5,"title":"From a fork","url":"https://github.com/o/r/pull/5","headRefName":"main","isCrossRepository":true,
	   "headRepositoryOwner":{"login":"bob"},"author":{"login":"bob"},
	   "statusCheckRollup":[{"status":"COMPLETED","conclusion":"FAILURE"}]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	list := WithPRs([]Branch{{Home: true, Name: "main"}, {Key: "old", Name: "old"}, {Key: "feature-x", Name: "feature/x"}}, prs)
	var names []string
	for _, b := range list {
		names = append(names, b.Name)
	}
	if got := strings.Join(names, " "); got != "main bob:main feature/x old" {
		t.Fatalf("order = %s", got)
	}
	if list[0].PR != nil {
		t.Error("the fork's main was put on the user's main")
	}
	if pr := list[1].PR; pr == nil || !pr.Fork || pr.Checks != "fail" || list[1].Key != "pr-5" {
		t.Errorf("fork = %+v %+v", list[1], pr)
	}
	if pr := list[2].PR; pr == nil || pr.Number != 3 || pr.Checks != "pending" || pr.Author != "ann" {
		t.Errorf("feature = %+v", pr)
	}
}

func TestExcludeIsAddedOnce(t *testing.T) {
	dir := repo(t)
	r, _ := Open(dir)
	for range 2 {
		if err := r.Exclude("node_modules"); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if n := strings.Count(string(b), "\nnode_modules\n"); n != 1 {
		t.Errorf("exclude has node_modules %d times:\n%s", n, b)
	}
}
