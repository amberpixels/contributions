// Command contributions draws a pattern on your GitHub contribution graph: it
// generates a history of empty, backdated commits sized against your real
// activity and pushes it once into an empty repo.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/amberpixels/contributions/internal/calendar"
	"github.com/amberpixels/contributions/internal/history"
	"github.com/amberpixels/contributions/internal/pattern"
	"github.com/amberpixels/contributions/internal/plan"
	"github.com/amberpixels/contributions/internal/render"
)

// canvasDir keeps the last generated canvas, so verify can check GitHub
// counted all of it.
const canvasDir = ".canvas"

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
				Name:  "paint",
				Usage: "generate the canvas in " + canvasDir,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "push", Usage: "push it to the (empty) canvas repo"},
				},
				Action: runPaint,
			},
			{
				Name:   "verify",
				Usage:  "compare the live graph with the painted canvas; fails on any mismatch",
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
	cfg    config
	cal    calendar.Calendar
	canvas history.Canvas
	plan   plan.Plan
}

// load reads the config, fetches the calendar and builds the plan.
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
	s.canvas = history.Canvas{Dir: canvasDir}

	s.plan, err = plan.Build(s.cal, pat, plan.Options{
		Background: s.cfg.Background,
		Volume:     s.cfg.Volume,
		Ahead:      s.cfg.Ahead,
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
	fmt.Printf("\n@%s: volume %d, busiest real day %d, +%d commits (through %s)\n",
		s.cal.Login, s.plan.Volume, s.plan.RealMax, s.plan.Added(), s.plan.Days[len(s.plan.Days)-1].Date)
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

func runPaint(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	summarize(s)

	tip, err := s.canvas.Create(ctx, s.plan.Painted(), s.author())
	if err != nil {
		return err
	}
	fmt.Printf("canvas: %d commits in %s, tip %s\n", s.plan.Added(), canvasDir, tip[:12])

	if !cmd.Bool("push") {
		return nil
	}
	if s.cfg.Canvas == "" {
		return errors.New("config: canvas repo is not set")
	}
	if err := s.canvas.Push(ctx, s.cfg.Canvas); err != nil {
		return err
	}
	fmt.Println("pushed to", s.cfg.Canvas)

	return nil
}

// runVerify checks the canvas against the live graph: every canvas commit
// counted, and every shade what the level formula in package plan predicts.
func runVerify(ctx context.Context, cmd *cli.Command) error {
	painted, err := history.Canvas{Dir: canvasDir}.Painted(ctx)
	if err != nil {
		return err
	}
	if len(painted) == 0 {
		return errors.New("no canvas in " + canvasDir + "; run paint first")
	}
	token, err := calendar.Token(ctx)
	if err != nil {
		return err
	}
	cal, err := calendar.Fetch(ctx, token)
	if err != nil {
		return err
	}

	peak := 0
	for _, week := range cal.Weeks {
		for _, d := range week {
			peak = max(peak, d.Count)
		}
	}

	off, days := 0, 0
	for _, week := range cal.Weeks {
		for _, d := range week {
			days++
			if missing := painted[d.Date] - d.Count; missing > 0 {
				fmt.Printf("%s: %d canvas commits not counted\n", d.Date, missing)
				off++
			} else if want := plan.Level(d.Count, peak); d.Level != want {
				fmt.Printf("%s: %d contributions show shade %d, formula says %d\n", d.Date, d.Count, d.Level, want)
				off++
			}
		}
	}
	if off > 0 {
		return fmt.Errorf("%d of %d days differ", off, days)
	}
	fmt.Printf("all %d days counted and shaded as predicted\n", days)

	return nil
}
