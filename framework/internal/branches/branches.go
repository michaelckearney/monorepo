// Package branches lists a project's git branches, with what GitHub knows
// of their pull requests, and checks them out into worktrees of their own
// for the sidecar of `harness run`.
//
// The user's own checkout is never touched: each branch is checked out at
// .desktop/worktrees/<key>/ of the user's checkout, which is desktop's to
// change, so a switch never meets uncommitted work.
package branches

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Repo is the git repository a project is in.
type Repo struct {
	// Top is the top folder of the checkout the project was found in,
	// which may be a worktree of desktop's.
	Top string
	// Rel is the project's folder relative to Top: "." for a project at the
	// top of its repository.
	Rel string
	// Home is the project folder in the user's own checkout: the main
	// worktree, which holds the others.
	Home string
}

// Open finds the repository of the project at root. It fails with
// ErrNotGit when root is not in one.
func Open(root string) (*Repo, error) {
	out, err := git(context.Background(), root, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return nil, ErrNotGit
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return nil, ErrNotGit
	}
	top, common := real(lines[0]), real(lines[1])
	rel, err := filepath.Rel(top, real(root))
	if err != nil {
		return nil, err
	}
	r := &Repo{Top: top, Rel: rel, Home: filepath.Join(top, rel)}
	// The common folder is the main worktree's .git, unless the repository
	// is bare or keeps it elsewhere; then the main checkout is this one.
	if filepath.Base(common) == ".git" {
		r.Home = filepath.Join(filepath.Dir(common), rel)
	}
	return r, nil
}

var ErrNotGit = errors.New("not in a git repository")

func real(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// Branch is one entry of the sidecar's list.
type Branch struct {
	// Key names the branch to the sidecar and is its worktree's folder
	// name. It is "" for the user's own checkout.
	Key  string `json:"key"`
	Name string `json:"name"`
	// Home is the user's own checkout, whose branch is Name.
	Home    bool   `json:"home"`
	Subject string `json:"subject,omitempty"`
	// Time is the commit's, in Unix seconds.
	Time int64 `json:"time,omitempty"`
	PR   *PR   `json:"pr,omitempty"`

	local, remote string
}

// PR is what GitHub says of a branch's open pull request.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft"`
	// Fork is a pull request from another repository: code that no one
	// with access to this one wrote.
	Fork bool `json:"fork"`
	// Checks is "pass", "fail", "pending", or "" when there are none.
	Checks string `json:"checks"`

	head string
}

// Fetch updates the remote branches, and removes those deleted upstream.
func (r *Repo) Fetch(ctx context.Context) error {
	_, err := git(ctx, r.Top, "fetch", "--all", "--prune", "--quiet")
	return err
}

// List returns the user's checkout first, then every other local and remote
// branch, newest commit first. A local branch and the one it tracks on
// origin are one entry.
func (r *Repo) List(ctx context.Context) ([]Branch, error) {
	homeBranch, _ := git(ctx, r.Home, "branch", "--show-current")
	homeBranch = strings.TrimSpace(homeBranch)
	homeDesc, _ := git(ctx, r.Home, "log", "-1", "--format=%ct%x00%s")

	out, err := git(ctx, r.Top, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(committerdate:unix)%00%(subject)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	byName := map[string]*Branch{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(line, "\x00", 4)
		if len(f) != 4 {
			continue
		}
		ref, sha, subject := f[0], f[1], f[3]
		when, _ := strconv.ParseInt(f[2], 10, 64)
		var name string
		remote := false
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			name = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/remotes/"):
			name = strings.TrimPrefix(ref, "refs/remotes/")
			if strings.HasSuffix(name, "/HEAD") || !strings.Contains(name, "/") {
				continue
			}
			// origin's branches go by their own names, to pair with the
			// local branch of that name; another remote's keep its name.
			name = strings.TrimPrefix(name, "origin/")
			remote = true
		default:
			continue
		}
		b := byName[name]
		if b == nil {
			b = &Branch{Name: name, Key: Key(name)}
			byName[name] = b
			order = append(order, name)
		}
		if remote {
			b.remote = sha
		} else {
			b.local = sha
		}
		// The local branch's commit describes the entry, when there is one.
		if !remote || b.Subject == "" {
			b.Subject, b.Time = subject, when
		}
	}

	home := Branch{Home: true, Name: homeBranch}
	if f := strings.SplitN(strings.TrimSpace(homeDesc), "\x00", 2); len(f) == 2 {
		home.Time, _ = strconv.ParseInt(f[0], 10, 64)
		home.Subject = f[1]
	}
	if home.Name == "" {
		home.Name = "detached HEAD"
	}
	list := []Branch{home}
	var rest []Branch
	for _, n := range order {
		if n == homeBranch {
			continue
		}
		rest = append(rest, *byName[n])
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Time > rest[j].Time })
	return append(list, rest...), nil
}

// Key is the worktree folder name for a branch: the name, with anything
// but letters, digits, dots, dashes and underscores made a dash.
func Key(name string) string {
	k := unsafeKey.ReplaceAllString(name, "-")
	if k == "" || k == "." || k == ".." {
		k = "branch"
	}
	return k
}

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// PRs asks GitHub, through the gh command, for the repository's open pull
// requests. It fails when gh is not installed, not signed in, or the
// repository is not on GitHub; the list then has only branches.
func (r *Repo) PRs(ctx context.Context) ([]PR, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("install the gh command and run gh auth login to see pull requests")
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--state", "open", "--limit", "100",
		"--json", "number,title,author,url,isDraft,isCrossRepository,headRefName,headRepositoryOwner,statusCheckRollup")
	cmd.Dir = r.Top
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh pr list: %s", firstLine(msg))
	}
	return parsePRs(out)
}

func parsePRs(out []byte) ([]PR, error) {
	var raw []struct {
		Number              int
		Title               string
		URL                 string
		IsDraft             bool
		IsCrossRepository   bool
		HeadRefName         string
		Author              struct{ Login string }
		HeadRepositoryOwner struct {
			Login string
		}
		StatusCheckRollup []struct {
			Status, Conclusion, State string
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh pr list: %v", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		pr := PR{Number: p.Number, Title: p.Title, Author: p.Author.Login, URL: p.URL,
			Draft: p.IsDraft, Fork: p.IsCrossRepository, head: p.HeadRefName}
		if pr.Fork {
			pr.head = p.HeadRepositoryOwner.Login + ":" + p.HeadRefName
		}
		var fail, pending, any bool
		for _, c := range p.StatusCheckRollup {
			any = true
			// A check run has a status and, once complete, a conclusion; a
			// commit status has only a state.
			v := c.Conclusion
			if c.State != "" {
				v = c.State
			} else if c.Status != "COMPLETED" {
				v = "PENDING"
			}
			switch v {
			case "SUCCESS", "NEUTRAL", "SKIPPED":
			case "PENDING", "EXPECTED", "QUEUED", "IN_PROGRESS", "WAITING", "REQUESTED":
				pending = true
			default:
				fail = true
			}
		}
		switch {
		case fail:
			pr.Checks = "fail"
		case pending:
			pr.Checks = "pending"
		case any:
			pr.Checks = "pass"
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// WithPRs puts each pull request on its branch. A pull request whose branch
// is not here, which every one from a fork is, gets an entry of its own,
// checked out from GitHub's refs/pull/<n>/head. Pull requests come after the
// user's checkout, newest first, and the other branches after them.
func WithPRs(list []Branch, prs []PR) []Branch {
	byName := map[string]int{}
	for i, b := range list {
		byName[b.Name] = i
	}
	var withPR, extra []Branch
	used := map[int]bool{}
	for _, pr := range prs {
		pr := pr
		if i, ok := byName[pr.head]; ok && !pr.Fork {
			list[i].PR = &pr
			if list[i].Home {
				continue
			}
			withPR = append(withPR, list[i])
			used[i] = true
			continue
		}
		extra = append(extra, Branch{Key: fmt.Sprintf("pr-%d", pr.Number), Name: pr.head, PR: &pr})
	}
	withPR = append(withPR, extra...)
	sort.SliceStable(withPR, func(i, j int) bool { return withPR[i].PR.Number > withPR[j].PR.Number })
	out := []Branch{}
	rest := []Branch{}
	for i, b := range list {
		switch {
		case b.Home:
			out = append(out, b)
		case !used[i]:
			rest = append(rest, b)
		}
	}
	out = append(out, withPR...)
	return append(out, rest...)
}

// WorktreeDir is where the branch of key is checked out.
func (r *Repo) WorktreeDir(key string) string {
	return filepath.Join(r.Home, ".desktop", "worktrees", key)
}

// ProjectDir is the project's folder in the worktree of key.
func (r *Repo) ProjectDir(key string) string {
	return filepath.Join(r.WorktreeDir(key), r.Rel)
}

// Checkout puts b's newest commit in its worktree and returns the project's
// folder there. A pull request is fetched from GitHub first, so a push to
// it since the list was made is included.
func (r *Repo) Checkout(ctx context.Context, b Branch) (string, error) {
	commit, err := r.commit(ctx, b)
	if err != nil {
		return "", err
	}
	dir := r.WorktreeDir(b.Key)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// The worktree is desktop's: anything changed in it is discarded.
		// Its .desktop/, with the branch's data, is ignored, so it stays.
		if _, err := git(ctx, dir, "checkout", "--force", "--detach", commit); err != nil {
			return "", err
		}
		return r.ProjectDir(b.Key), nil
	}
	_, _ = git(ctx, r.Top, "worktree", "prune")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	// Detached, since git checks a branch out in only one worktree at a
	// time, and the user's may have it.
	if _, err := git(ctx, r.Top, "worktree", "add", "--force", "--detach", dir, commit); err != nil {
		return "", err
	}
	return r.ProjectDir(b.Key), nil
}

// commit is the commit to check out for b: a pull request's head, or the
// local branch's, unless the remote one is ahead of it.
func (r *Repo) commit(ctx context.Context, b Branch) (string, error) {
	if b.local == "" && b.remote == "" {
		if b.PR == nil {
			return "", fmt.Errorf("branch %s is gone; refresh the list", b.Name)
		}
		if _, err := git(ctx, r.Top, "fetch", "--quiet", "origin", fmt.Sprintf("refs/pull/%d/head", b.PR.Number)); err != nil {
			return "", err
		}
		out, err := git(ctx, r.Top, "rev-parse", "FETCH_HEAD")
		return strings.TrimSpace(out), err
	}
	if b.remote == "" {
		return b.local, nil
	}
	if b.local == "" {
		return b.remote, nil
	}
	if _, err := git(ctx, r.Top, "merge-base", "--is-ancestor", b.local, b.remote); err == nil {
		return b.remote, nil
	}
	return b.local, nil
}

// Exclude adds pattern to the repository's info/exclude, which ignores it in
// every worktree without a change to the project's .gitignore.
func (r *Repo) Exclude(pattern string) error {
	out, err := git(context.Background(), r.Top, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	file := strings.TrimSpace(out)
	have, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, l := range strings.Split(string(have), "\n") {
		if strings.TrimSpace(l) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	add := ""
	if len(have) > 0 && !bytes.HasSuffix(have, []byte("\n")) {
		add = "\n"
	}
	add += "# desktop links node_modules to its package cache\n" + pattern + "\n"
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(add)
	return err
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return string(out), fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return string(out), nil
}
