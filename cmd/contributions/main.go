// Command contributions draws a pattern on your GitHub contribution graph by
// rewriting this repo's default branch as code commits plus backdated empty
// commits, sized against your real activity.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/amberpixels/contributions/internal/calendar"
	"github.com/amberpixels/contributions/internal/history"
	"github.com/amberpixels/contributions/internal/pattern"
	"github.com/amberpixels/contributions/internal/plan"
	"github.com/amberpixels/contributions/internal/render"
)

func main() {
	app := &cli.Command{
		Name:  "contributions",
		Usage: "draw a pattern on your GitHub contribution graph",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Value: configFile, Usage: "config file"},
		},
		Commands: []*cli.Command{
			{
				Name:  "plan",
				Usage: "show what the graph would look like, change nothing",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "svg", Usage: "also write a GitHub-styled preview to this file"},
				},
				Action: runPlan,
			},
			{
				Name:  "apply",
				Usage: "rewrite the branch to draw the plan",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "push", Usage: "force-push the branch afterwards"},
				},
				Action: runApply,
			},
			{
				Name:   "verify",
				Usage:  "compare the live graph with the plan; fails on any mismatch",
				Action: runVerify,
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "contributions:", err)
		os.Exit(1)
	}
}

// state is everything a command needs: config, live calendar and the plan.
type state struct {
	cfg  config
	cal  calendar.Calendar
	repo history.Repo
	plan plan.Plan
}

func load(ctx context.Context, cmd *cli.Command) (state, error) {
	var s state
	var err error
	if s.cfg, err = loadConfig(cmd.String("config")); err != nil {
		return s, err
	}
	pat, err := pattern.Load(s.cfg.Pattern)
	if err != nil {
		return s, err
	}
	token, err := calendar.Token(ctx)
	if err != nil {
		return s, err
	}
	if s.cal, err = calendar.Fetch(ctx, token); err != nil {
		return s, err
	}

	// Subtract only pixels GitHub has seen, i.e. the pushed ones. Counting local
	// unpushed commits would lower the real activity and change the plan on
	// every run.
	s.repo = history.Repo{Dir: "."}
	painted, err := s.repo.Painted(ctx, "refs/remotes/"+s.cfg.Remote+"/"+s.cfg.Branch)
	if err != nil {
		return s, err
	}

	s.plan, err = plan.Build(s.cal, pat, painted, plan.Options{
		Background: s.cfg.Background,
		Volume:     s.cfg.Volume,
	})

	return s, err
}

func (s state) author() history.Author {
	a := history.Author{Name: s.cfg.Author.Name, Email: s.cfg.Author.Email}
	if a.Name == "" {
		a.Name = s.cal.Login
	}
	if a.Email == "" {
		a.Email = s.cal.NoreplyEmail()
	}

	return a
}

func summarize(s state) {
	fmt.Print(render.Text(s.plan))
	fmt.Printf("\n@%s: volume %d, busiest real day %d, +%d commits\n",
		s.cal.Login, s.plan.Volume, s.plan.RealMax, s.plan.Added())
	for _, w := range s.plan.Warnings {
		fmt.Println("warning:", w)
	}
}

func runPlan(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	summarize(s)

	if path := cmd.String("svg"); path != "" {
		if err := os.WriteFile(path, []byte(render.SVG(s.plan)), 0o600); err != nil {
			return fmt.Errorf("write preview: %w", err)
		}
		fmt.Println("preview:", path)
	}

	return nil
}

func runApply(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	summarize(s)

	old, err := s.repo.Tip(ctx, s.cfg.Branch)
	if err != nil {
		return fmt.Errorf("branch %s: %w", s.cfg.Branch, err)
	}
	tip, err := s.repo.Rebuild(ctx, s.cfg.Branch, s.plan.Painted(), s.author())
	if err != nil {
		return err
	}
	if tip == old {
		fmt.Println("graph already drawn, nothing to do")

		return nil
	}
	if err := s.repo.Move(ctx, s.cfg.Branch, tip, old); err != nil {
		return err
	}
	fmt.Printf("%s: %s -> %s\n", s.cfg.Branch, old[:12], tip[:12])

	if !cmd.Bool("push") {
		return nil
	}
	if err := s.repo.Push(ctx, s.cfg.Remote, s.cfg.Branch); err != nil {
		return err
	}
	fmt.Println("pushed to", s.cfg.Remote)

	return nil
}

func runVerify(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}

	live := map[string]int{}
	for _, week := range s.cal.Weeks {
		for _, d := range week {
			live[d.Date] = d.Level
		}
	}

	off := 0
	for _, d := range s.plan.Days {
		if got := live[d.Date]; got != d.Level {
			fmt.Printf("%s: shade %d, want %d (real %d + %d)\n", d.Date, got, d.Level, d.Real, d.Add)
			off++
		}
	}
	if off > 0 {
		return fmt.Errorf("%d of %d days differ from the plan", off, len(s.plan.Days))
	}
	fmt.Printf("all %d days match the plan\n", len(s.plan.Days))

	return nil
}
