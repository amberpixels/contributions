// Package history builds the canvas: a repository with no files, only empty
// backdated (and future-dated) commits that draw the pattern.
//
// The canvas is pushed once, into an empty GitHub repo. GitHub counted every
// commit of a 247k-commit first push to a fresh repo, but only the newest
// ~1000 of a 37k-commit update to an existing branch, and it never un-counted
// commits removed by a force-push. Deleting the repo does erase them, so a
// redraw is: delete the repo, recreate it, push a new canvas.
package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Marker starts the subject of every canvas commit, followed by its date.
const Marker = "pixel "

// Author is the identity the canvas commits are made with. It must be an email
// GitHub links to the account, or the commits will not count.
type Author struct {
	Name  string
	Email string
}

// Canvas is a local bare repository holding the generated history.
type Canvas struct {
	Dir string
}

// Create replaces the canvas with a fresh history for painted (commits per
// date) and returns its tip. Same input, same hashes.
func (c Canvas) Create(ctx context.Context, painted map[string]int, who Author) (string, error) {
	if len(painted) == 0 {
		return "", errors.New("nothing to paint")
	}
	if err := os.RemoveAll(c.Dir); err != nil {
		return "", fmt.Errorf("clear canvas: %w", err)
	}
	if err := os.MkdirAll(c.Dir, 0o750); err != nil {
		return "", fmt.Errorf("create canvas: %w", err)
	}
	if _, err := c.git(ctx, nil, "init", "--quiet", "--bare", "--initial-branch=main"); err != nil {
		return "", err
	}

	stream, err := chain(painted, who)
	if err != nil {
		return "", err
	}
	if _, err := c.git(ctx, stream, "fast-import", "--quiet"); err != nil {
		return "", err
	}

	return c.git(ctx, nil, "rev-parse", "refs/heads/main")
}

// Painted counts canvas commits per date. A canvas that does not exist yet
// counts as nothing painted.
func (c Canvas) Painted(ctx context.Context) (map[string]int, error) {
	m := make(map[string]int)
	if _, err := os.Stat(c.Dir); errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	out, err := c.git(ctx, nil, "log", "--format=%s", "refs/heads/main")
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, Marker); ok {
			date, _, _ := strings.Cut(rest, " ")
			m[date]++
		}
	}

	return m, nil
}

// Push sends the canvas to remote in a single push. It refuses a remote that
// already has branches: an update push is only partly counted, so a redraw
// needs the GitHub repo deleted and recreated first.
func (c Canvas) Push(ctx context.Context, remote string) error {
	refs, err := c.git(ctx, nil, "ls-remote", "--heads", remote)
	if err != nil {
		return err
	}
	if refs != "" {
		return fmt.Errorf("%s is not empty; delete and recreate the repo to redraw", remote)
	}
	_, err = c.git(ctx, nil, "push", "--quiet", remote, "refs/heads/main:refs/heads/main")

	return err
}

func (c Canvas) git(ctx context.Context, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = c.Dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(string(out)), nil
}

// chain builds a fast-import stream of empty commits, oldest date first, each
// at noon UTC plus a second per commit so the date never slips. The first
// commit has no parent and the tree stays empty throughout.
func chain(painted map[string]int, who Author) ([]byte, error) {
	dates := make([]string, 0, len(painted))
	for d := range painted {
		dates = append(dates, d)
	}
	slices.Sort(dates)

	var b bytes.Buffer
	for _, date := range dates {
		day, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return nil, fmt.Errorf("bad date %q: %w", date, err)
		}
		n := painted[date]
		if n > 12*60*60 {
			return nil, fmt.Errorf("%s: %d commits do not fit in half a day of seconds", date, n)
		}
		noon := day.Add(12 * time.Hour).Unix()
		for i := range n {
			msg := fmt.Sprintf("%s%s %d/%d\n", Marker, date, i+1, n)
			fmt.Fprintf(&b, "commit refs/heads/main\n")
			fmt.Fprintf(&b, "author %s <%s> %d +0000\n", who.Name, who.Email, noon+int64(i))
			fmt.Fprintf(&b, "committer %s <%s> %d +0000\n", who.Name, who.Email, noon+int64(i))
			fmt.Fprintf(&b, "data %d\n%s\n", len(msg), msg)
		}
	}

	return b.Bytes(), nil
}
