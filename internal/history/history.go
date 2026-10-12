// Package history rewrites a branch as its code commits followed by a chain of
// empty, backdated "pixel" commits that draw the pattern.
//
// The chain is deterministic: same plan, same base, same author, same hashes.
// So a run that changes nothing produces the branch it started from, and a run
// that only adds today's commits produces a fast-forward.
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

// Marker starts the subject of every commit this tool makes. Anything else on
// the branch is treated as code and kept.
const Marker = "pixel "

const tmpRef = "refs/contributions/next"

// Author is the identity the pixel commits are made with. It must be an email
// GitHub links to the account, or the commits will not count.
type Author struct {
	Name  string
	Email string
}

// Repo is a git working copy.
type Repo struct {
	Dir string
}

// Tip returns the commit a branch points at.
func (r Repo) Tip(ctx context.Context, branch string) (string, error) {
	return r.git(ctx, nil, nil, "rev-parse", "--verify", "refs/heads/"+branch)
}

// Painted counts pixel commits per date reachable from ref. A missing ref
// counts as nothing painted.
func (r Repo) Painted(ctx context.Context, ref string) (map[string]int, error) {
	m := make(map[string]int)
	if _, err := r.git(ctx, nil, nil, "rev-parse", "--verify", "--quiet", ref); err != nil {
		return m, nil //nolint:nilerr // no ref yet means a first run
	}
	out, err := r.git(ctx, nil, nil, "log", "--first-parent", "--format=%s", ref)
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

type commit struct {
	hash, subject string
	parents       int
}

// Rebuild writes the branch's code commits plus a pixel chain for painted and
// returns the resulting commit, without moving the branch. Code commits made
// on top of an older chain are re-parented onto the code before it.
func (r Repo) Rebuild(ctx context.Context, branch string, painted map[string]int, who Author) (string, error) {
	commits, err := r.firstParent(ctx, branch)
	if err != nil {
		return "", err
	}

	base, sawPixel := "", false
	for _, c := range commits {
		if strings.HasPrefix(c.subject, Marker) {
			sawPixel = true

			continue
		}
		if !sawPixel {
			base = c.hash

			continue
		}
		if c.parents > 1 {
			return "", fmt.Errorf(
				"merge commit %s sits on top of pixel commits; rebase it onto the code first",
				c.hash[:12],
			)
		}
		if base, err = r.reparent(ctx, c.hash, base); err != nil {
			return "", err
		}
	}
	if base == "" {
		return "", errors.New("branch has no code commits; commit the project first")
	}
	if len(painted) == 0 {
		return base, nil
	}

	stream, err := chain(base, painted, who)
	if err != nil {
		return "", err
	}
	if _, err := r.git(ctx, stream, nil, "fast-import", "--quiet", "--force"); err != nil {
		return "", err
	}
	defer func() { _, _ = r.git(context.WithoutCancel(ctx), nil, nil, "update-ref", "-d", tmpRef) }()

	return r.git(ctx, nil, nil, "rev-parse", tmpRef)
}

// Move points the branch at tip, failing if it no longer points at old.
func (r Repo) Move(ctx context.Context, branch, tip, old string) error {
	_, err := r.git(ctx, nil, nil, "update-ref", "refs/heads/"+branch, tip, old)

	return err
}

// PushBatch caps the new commits per push. GitHub counted only the newest
// ~1000 commits of a single 37k-commit push and silently dropped the rest.
const PushBatch = 500

// Push force-pushes the branch oldest commits first, at most batch new commits
// per push, each step leased on the remote state the previous one left.
func (r Repo) Push(ctx context.Context, remote, branch string, batch int) error {
	tip, err := r.Tip(ctx, branch)
	if err != nil {
		return err
	}

	// No remote branch yet: the lease "" means "must not exist".
	lease, base := "", ""
	if old, err := r.git(
		ctx,
		nil,
		nil,
		"rev-parse",
		"--verify",
		"--quiet",
		"refs/remotes/"+remote+"/"+branch,
	); err == nil {
		lease = old
		if mb, err := r.git(ctx, nil, nil, "merge-base", old, tip); err == nil {
			base = mb
		}
	}

	args := []string{"rev-list", "--reverse", "--first-parent", tip}
	if base != "" {
		args = append(args, "^"+base)
	}
	out, err := r.git(ctx, nil, nil, args...)
	if err != nil {
		return err
	}
	commits := strings.Fields(out)

	var steps []string
	for i := batch - 1; i < len(commits); i += batch {
		steps = append(steps, commits[i])
	}
	// The tip itself, also when it is an ancestor of the remote (a shrink).
	if len(steps) == 0 || steps[len(steps)-1] != tip {
		steps = append(steps, tip)
	}

	ref := "refs/heads/" + branch
	for _, sha := range steps {
		if _, err := r.git(
			ctx,
			nil,
			nil,
			"push",
			"--force-with-lease="+ref+":"+lease,
			remote,
			sha+":"+ref,
		); err != nil {
			return err
		}
		lease = sha
	}

	return nil
}

func (r Repo) firstParent(ctx context.Context, branch string) ([]commit, error) {
	out, err := r.git(ctx, nil, nil, "log", "--first-parent", "--reverse", "--format=%H %P%x00%s", "refs/heads/"+branch)
	if err != nil {
		return nil, err
	}

	var commits []commit
	for line := range strings.SplitSeq(out, "\n") {
		hashes, subject, _ := strings.Cut(line, "\x00")
		f := strings.Fields(hashes)
		commits = append(commits, commit{hash: f[0], subject: subject, parents: len(f) - 1})
	}

	return commits, nil
}

// reparent recreates a commit on a new parent, keeping its tree, message,
// author and committer, so the same input always gives the same hash.
func (r Repo) reparent(ctx context.Context, hash, parent string) (string, error) {
	meta, err := r.git(
		ctx,
		nil,
		nil,
		"log",
		"-1",
		"--format=%an%x00%ae%x00%ad%x00%cn%x00%ce%x00%cd",
		"--date=raw",
		hash,
	)
	if err != nil {
		return "", err
	}
	f := strings.Split(meta, "\x00")
	msg, err := r.git(ctx, nil, nil, "log", "-1", "--format=%B", hash)
	if err != nil {
		return "", err
	}
	env := []string{
		"GIT_AUTHOR_NAME=" + f[0], "GIT_AUTHOR_EMAIL=" + f[1], "GIT_AUTHOR_DATE=" + f[2],
		"GIT_COMMITTER_NAME=" + f[3], "GIT_COMMITTER_EMAIL=" + f[4], "GIT_COMMITTER_DATE=" + f[5],
	}

	return r.git(ctx, []byte(msg+"\n"), env, "commit-tree", hash+"^{tree}", "-p", parent)
}

func (r Repo) git(ctx context.Context, stdin []byte, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), env...)
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
// at noon UTC plus a second per commit so the date never slips.
func chain(base string, painted map[string]int, who Author) ([]byte, error) {
	dates := make([]string, 0, len(painted))
	for d := range painted {
		dates = append(dates, d)
	}
	slices.Sort(dates)

	var b bytes.Buffer
	first := true
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
			fmt.Fprintf(&b, "commit %s\n", tmpRef)
			fmt.Fprintf(&b, "author %s <%s> %d +0000\n", who.Name, who.Email, noon+int64(i))
			fmt.Fprintf(&b, "committer %s <%s> %d +0000\n", who.Name, who.Email, noon+int64(i))
			fmt.Fprintf(&b, "data %d\n%s", len(msg), msg)
			if first {
				fmt.Fprintf(&b, "from %s\n", base)
				first = false
			}
			b.WriteByte('\n')
		}
	}

	return b.Bytes(), nil
}
