// Command contributions draws a pattern on your GitHub contribution graph: it
// generates a history of empty, backdated commits sized against your real
// activity and pushes it once into an empty repo.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

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

// frame is one view of the graph: the rolling last year (year 0) or a
// calendar year from the year selector, each planned against its own calendar.
type frame struct {
	year int
	cal  calendar.Calendar
	plan plan.Plan
}

func (f frame) name() string {
	if f.year == 0 {
		return "last 12 months"
	}

	return strconv.Itoa(f.year)
}

// state is everything a command needs: config, the planned frames, the canvas.
type state struct {
	cfg    config
	frames []frame // rolling view first
	canvas history.Canvas
}

// load reads the config, fetches every view's calendar and plans each.
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
	s.canvas = history.Canvas{Dir: canvasDir}

	rolling := frame{}
	if rolling.cal, err = calendar.Fetch(ctx, token, 0); err != nil {
		return s, err
	}
	rolling.plan, err = plan.Build(rolling.cal, pat, plan.Options{
		Background: s.cfg.Background,
		Volume:     s.cfg.Volume,
		Ahead:      s.cfg.Ahead,
		Start:      s.cfg.Start,
	})
	if err != nil {
		return s, err
	}
	s.frames = append(s.frames, rolling)

	window := map[string]bool{}
	for _, d := range rolling.plan.Days {
		window[d.Date] = true
	}
	for _, y := range s.cfg.Years {
		f := frame{year: y}
		if f.cal, err = calendar.Fetch(ctx, token, y); err != nil {
			return s, err
		}
		// Same volume everywhere, so every view shades alike, unless a total
		// needs more. Days the rolling view shows too get no filler: at this
		// view's higher volume it would show darker there.
		f.plan, err = plan.Build(f.cal, pat, plan.Options{
			Background: s.cfg.Background,
			Volume:     rolling.plan.Volume,
			Start:      firstSunday(y),
			Total:      s.cfg.Totals[y],
			Fixed:      window,
		})
		if err != nil {
			return s, fmt.Errorf("%d: %w", y, err)
		}
		for _, d := range f.plan.Days {
			if d.Shade > 0 && window[d.Date] {
				return s, fmt.Errorf("%d: the pattern reaches %s, inside the last-12-months view", y, d.Date)
			}
		}
		s.frames = append(s.frames, f)
	}

	return s, nil
}

// firstSunday is the first day of a year's first full week.
func firstSunday(year int) string {
	d := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	for d.Weekday() != time.Sunday {
		d = d.AddDate(0, 0, 1)
	}

	return d.Format(time.DateOnly)
}

// painted merges the frames' commits per date. Views overlap only on
// background days, so the larger count is the one both frames asked for.
func (s state) painted() map[string]int {
	m := map[string]int{}
	for _, f := range s.frames {
		for date, n := range f.plan.Painted() {
			m[date] = max(m[date], n)
		}
	}

	return m
}

func (s state) author() history.Author {
	cal := s.frames[0].cal
	a := history.Author{Name: s.cfg.Author.Name, Email: s.cfg.Author.Email}
	if a.Name == "" {
		a.Name = cal.Login
	}
	if a.Email == "" {
		a.Email = cal.NoreplyEmail()
	}

	return a
}

func summarize(s state) {
	for _, f := range s.frames {
		p := f.plan
		fmt.Printf("--- %s\n%s", f.name(), render.Text(p))
		fmt.Printf("%d contributions, volume %d, busiest real day %d, pattern complete on %s\n",
			p.Count(), p.Volume, p.RealMax, p.Complete)
		for _, w := range p.Warnings {
			fmt.Println("warning:", w)
		}
		fmt.Println()
	}
	total, last := 0, ""
	for date, n := range s.painted() {
		total += n
		last = max(last, date)
	}
	fmt.Printf("@%s: +%d commits, through %s\n", s.frames[0].cal.Login, total, last)
}

func runPlan(ctx context.Context, cmd *cli.Command) error {
	s, err := load(ctx, cmd)
	if err != nil {
		return err
	}
	summarize(s)

	if path := cmd.String("svg"); path != "" {
		if err := os.WriteFile(path, []byte(render.SVG(s.frames[0].plan)), 0o600); err != nil {
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

	painted := s.painted()
	tip, err := s.canvas.Create(ctx, painted, s.author())
	if err != nil {
		return err
	}
	fmt.Printf("canvas: %s in %s\n", tip[:12], canvasDir)

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

// runVerify checks the canvas against every live view: each canvas commit
// counted, and each shade what the level formula in package plan predicts.
func runVerify(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd.String("config"))
	if err != nil {
		return err
	}
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

	failed := 0
	for _, year := range append([]int{0}, cfg.Years...) {
		cal, err := calendar.Fetch(ctx, token, year)
		if err != nil {
			return err
		}
		f := frame{year: year, cal: cal}
		if want, ok := cfg.Totals[year]; ok {
			if got := count(cal); got != want {
				fmt.Printf("%s: %d contributions, want %d\n", f.name(), got, want)
				failed++
			}
		}
		off, days := check(cal, painted)
		if off > 0 {
			fmt.Printf("%s: %d of %d days differ\n", f.name(), off, days)
			failed++
		} else {
			fmt.Printf("%s: all %d days counted and shaded as predicted\n", f.name(), days)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d checks failed", failed)
	}

	return nil
}

// count is the view's total, the number in its "N contributions in" header.
func count(cal calendar.Calendar) int {
	n := 0
	for _, week := range cal.Weeks {
		for _, d := range week {
			n += d.Count
		}
	}

	return n
}

// check compares one view with the canvas and prints every day that differs.
func check(cal calendar.Calendar, painted map[string]int) (int, int) {
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
				fmt.Printf("  %s: %d canvas commits not counted\n", d.Date, missing)
				off++
			} else if want := plan.Level(d.Count, peak); d.Level != want {
				fmt.Printf("  %s: %d contributions show shade %d, formula says %d\n", d.Date, d.Count, d.Level, want)
				off++
			}
		}
	}

	return off, days
}
