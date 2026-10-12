package history_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amberpixels/contributions/internal/history"
)

var who = history.Author{Name: "pixel", Email: "1+pixel@users.noreply.github.com"}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=dev", "GIT_AUTHOR_EMAIL=dev@example.com", "GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=dev", "GIT_COMMITTER_EMAIL=dev@example.com", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

// newRepo returns a repo whose main branch holds one code commit.
func newRepo(t *testing.T) history.Repo {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "-q", "-m", "init")

	return history.Repo{Dir: dir}
}

// apply rebuilds main for painted and moves it, returning old and new tips.
func apply(t *testing.T, r history.Repo, painted map[string]int) (string, string) {
	t.Helper()
	old, err := r.Tip(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	tip, err := r.Rebuild(t.Context(), "main", painted, who)
	if err != nil {
		t.Fatal(err)
	}
	if tip != old {
		if err := r.Move(t.Context(), "main", tip, old); err != nil {
			t.Fatal(err)
		}
	}

	return old, tip
}

func TestRebuildPaintsAndCounts(t *testing.T) {
	r := newRepo(t)
	painted := map[string]int{"2026-01-01": 3, "2026-01-02": 1}
	apply(t, r, painted)

	got, err := r.Painted(t.Context(), "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if missing, err := r.Painted(t.Context(), "refs/remotes/origin/main"); err != nil || len(missing) != 0 {
		t.Errorf("missing ref: painted %v, err %v", missing, err)
	}
	if len(got) != 2 || got["2026-01-01"] != 3 || got["2026-01-02"] != 1 {
		t.Errorf("painted = %v, want %v", got, painted)
	}

	// Dates land at noon UTC, so the day is the same in every timezone GitHub uses.
	dates := run(t, r.Dir, "log", "--format=%ad", "--date=format:%Y-%m-%d %H", "-n", "1", "main")
	if dates != "2026-01-02 12" {
		t.Errorf("last pixel dated %q", dates)
	}
}

func TestRebuildIsDeterministic(t *testing.T) {
	r := newRepo(t)
	painted := map[string]int{"2026-01-01": 2}
	_, tip := apply(t, r, painted)

	// Same plan again: nothing to change.
	if old, again := apply(t, r, painted); again != old || again != tip {
		t.Errorf("redraw moved main: %s -> %s", old, again)
	}

	// One more day: the old tip stays an ancestor, so the push is a fast-forward.
	painted["2026-01-02"] = 1
	_, next := apply(t, r, painted)
	run(t, r.Dir, "merge-base", "--is-ancestor", tip, next)

	// A smaller plan drops the extra pixels.
	delete(painted, "2026-01-02")
	if _, back := apply(t, r, painted); back != tip {
		t.Errorf("shrinking the plan gave %s, want %s", back, tip)
	}
}

func TestRebuildKeepsCodeCommittedOnTop(t *testing.T) {
	r := newRepo(t)
	apply(t, r, map[string]int{"2026-01-01": 2})

	if err := os.WriteFile(filepath.Join(r.Dir, "NOTES.md"), []byte("later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, r.Dir, "add", "NOTES.md")
	run(t, r.Dir, "commit", "-q", "-m", "add notes")

	apply(t, r, map[string]int{"2026-01-01": 1})

	subjects := run(t, r.Dir, "log", "--reverse", "--format=%s", "main")
	want := "init\nadd notes\npixel 2026-01-01 1/1"
	if subjects != want {
		t.Errorf("history:\n%s\nwant:\n%s", subjects, want)
	}
	if files := run(t, r.Dir, "ls-tree", "--name-only", "main"); files != "NOTES.md\nREADME.md" {
		t.Errorf("tree lost files: %q", files)
	}
}
