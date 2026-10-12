// Package plan decides how many commits each day of the graph needs so that,
// added to the real activity already there, the graph shows the pattern.
//
// GitHub shades a day by its share of the busiest day in the window: with a
// quantum q = floor(max/4), counts 1..q are level 1, q+1..2q level 2, and so
// on. That was measured against a real calendar, not documented by GitHub, so
// `contributions verify` exists to catch it changing.
package plan

import (
	"errors"
	"fmt"

	"github.com/amberpixels/contributions/internal/calendar"
	"github.com/amberpixels/contributions/internal/pattern"
)

// Options controls how the pattern is laid over real activity.
type Options struct {
	// Background adds one commit to every day, so days without real activity
	// show the lightest shade instead of a gap.
	Background bool
	// Volume is the commit count of a darkest-shade day. 0 picks it from the
	// busiest real day, high enough that real activity stays at level 1.
	Volume int
}

// Day is one cell of the planned graph.
type Day struct {
	Date  string
	Col   int // week column in the window
	Row   int // weekday, 0 = Sunday
	Real  int // contributions not made by this tool
	Add   int // commits this tool makes on the day
	Level int // shade GitHub is expected to render
}

// Plan is the full graph plus the numbers it was derived from.
type Plan struct {
	Days     []Day
	Weeks    int
	Offset   int // first column of the pattern
	Volume   int
	RealMax  int
	Warnings []string
}

// Added is the total number of commits the plan makes.
func (p Plan) Added() int {
	n := 0
	for _, d := range p.Days {
		n += d.Add
	}

	return n
}

// Painted returns commits to make per date, for days with any.
func (p Plan) Painted() map[string]int {
	m := make(map[string]int)
	for _, d := range p.Days {
		if d.Add > 0 {
			m[d.Date] = d.Add
		}
	}

	return m
}

// Level is GitHub's shade for a day with total contributions, given the
// busiest day in the window.
func Level(total, peak int) int {
	if total <= 0 {
		return 0
	}
	q := max(peak/4, 1)

	return min((total+q-1)/q, 4)
}

// autoStep rounds an auto-picked volume, so a slightly busier real day does
// not change it and force a full redraw.
const autoStep = 50

// Build plans the graph. painted holds commits this tool already made per
// date; they are part of the calendar's counts and get subtracted to recover
// real activity.
func Build(cal calendar.Calendar, pat pattern.Pattern, painted map[string]int, opts Options) (Plan, error) {
	usable := fullPastWeeks(cal)
	if len(usable) < pat.Width() {
		return Plan{}, fmt.Errorf("pattern is %d weeks wide, only %d full weeks available", pat.Width(), len(usable))
	}

	p := Plan{
		Weeks:  len(cal.Weeks),
		Offset: usable[0] + (len(usable)-pat.Width())/2,
	}

	for c, week := range cal.Weeks {
		for _, d := range week {
			own := max(d.Count-painted[d.Date], 0)
			p.RealMax = max(p.RealMax, own)
			p.Days = append(p.Days, Day{Date: d.Date, Col: c, Row: d.Weekday, Real: own})
		}
	}

	p.Volume = opts.Volume
	if p.Volume == 0 {
		// Background puts real days at real+1, which must stay within level 1.
		// The extra quarter is headroom for a busier day later in the week.
		p.Volume = roundUp(5*(p.RealMax+1), autoStep)
	}
	q := p.Volume / 4
	if q == 0 {
		return Plan{}, errors.New("volume must be at least 4")
	}
	if p.RealMax+1 > q {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"busiest real day has %d contributions; at volume %d anything above %d shows darker than the background",
			p.RealMax, p.Volume, q-1))
	}

	peak := 0
	for i := range p.Days {
		d := &p.Days[i]
		want := 0
		if col := d.Col - p.Offset; col >= 0 && col < pat.Width() {
			want = pat.Cells[d.Row][col]
		}

		switch {
		case want == 4:
			d.Add = max(p.Volume-d.Real, 0)
		case want > 0:
			// Aim at the middle of the level's band, away from both edges.
			target := (2*want - 1) * q / 2
			d.Add = max(target-d.Real, 0)
			if d.Real > want*q {
				p.Warnings = append(p.Warnings, fmt.Sprintf(
					"%s: %d real contributions already exceed shade %d", d.Date, d.Real, want))
			}
		case opts.Background:
			d.Add = 1
		}
		peak = max(peak, d.Real+d.Add)
	}

	for i := range p.Days {
		p.Days[i].Level = Level(p.Days[i].Real+p.Days[i].Add, peak)
	}

	return p, nil
}

// fullPastWeeks lists the columns safe to draw in: complete weeks, minus the
// current one, whose remaining days have not happened yet.
func fullPastWeeks(cal calendar.Calendar) []int {
	var cols []int
	for c, week := range cal.Weeks[:len(cal.Weeks)-1] {
		if len(week) == pattern.Rows {
			cols = append(cols, c)
		}
	}

	return cols
}

func roundUp(n, step int) int {
	return (n + step - 1) / step * step
}
