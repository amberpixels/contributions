package history_test

import (
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

func TestCreate(t *testing.T) {
	c := history.Canvas{Dir: filepath.Join(t.TempDir(), "canvas")}
	painted := map[string]int{"2026-01-01": 3, "2026-01-02": 1}

	tip, err := c.Create(t.Context(), painted, who)
	if err != nil {
		t.Fatal(err)
	}
	if n := run(t, c.Dir, "rev-list", "--count", "main"); n != "4" {
		t.Errorf("%s commits, want 4", n)
	}
	if files := run(t, c.Dir, "ls-tree", "-r", "main"); files != "" {
		t.Errorf("canvas has files: %q", files)
	}
	// Noon UTC keeps the date the same in every timezone GitHub might use.
	if last := run(
		t,
		c.Dir,
		"log",
		"-1",
		"--format=%ad",
		"--date=format:%Y-%m-%d %H",
		"main",
	); last != "2026-01-02 12" {
		t.Errorf("last commit dated %q", last)
	}

	got, err := c.Painted(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["2026-01-01"] != 3 || got["2026-01-02"] != 1 {
		t.Errorf("painted = %v, want %v", got, painted)
	}

	// Same input, same history: a regenerated canvas is byte-identical.
	again, err := c.Create(t.Context(), painted, who)
	if err != nil {
		t.Fatal(err)
	}
	if again != tip {
		t.Errorf("regenerated tip %s, want %s", again, tip)
	}
}

func TestPaintedWithoutCanvas(t *testing.T) {
	got, err := history.Canvas{Dir: filepath.Join(t.TempDir(), "none")}.Painted(t.Context())
	if err != nil || len(got) != 0 {
		t.Errorf("painted %v, err %v", got, err)
	}
}

func TestPushOnlyIntoEmptyRepo(t *testing.T) {
	remote := t.TempDir()
	run(t, remote, "init", "-q", "--bare")
	c := history.Canvas{Dir: filepath.Join(t.TempDir(), "canvas")}
	tip, err := c.Create(t.Context(), map[string]int{"2026-01-01": 2}, who)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Push(t.Context(), remote); err != nil {
		t.Fatal(err)
	}
	if got := run(t, remote, "rev-parse", "main"); got != tip {
		t.Errorf("remote at %s, want %s", got, tip)
	}

	// A second push would be an update, which GitHub only partly counts.
	if err := c.Push(t.Context(), remote); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("pushing into a non-empty repo: err %v", err)
	}
}
