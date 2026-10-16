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
	"slices"
	"time"

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
	// Ahead is how many days after today get their background in advance.
	// GitHub counts a future-dated commit once its day arrives.
	Ahead int
	// Start pins the pattern's first column to the week of this date
	// (YYYY-MM-DD). Empty centers it in the full past weeks of the window.
	// Columns past today fill in as their days arrive.
	Start string
	// Total, when set, is the exact sum the window's days must reach. Volume
	// is raised until the days can hold it, and the commits the drawing leaves
	// short are spread over the days as filler that keeps every shade.
	Total int
	// Fixed lists days that get no filler, such as days another view shows too.
	Fixed map[string]bool
}

// Day is one cell of the planned graph.
type Day struct {
	Date  string
	Col   int // week column in the window
	Row   int // weekday, 0 = Sunday
	Real  int // contributions already on the calendar
	Add   int // commits this tool makes on the day
	Level int // shade GitHub is expected to render
	Shade int // the pattern's shade here, 0 outside the pattern
	// Future marks days after today, outside the calendar GitHub returned.
	Future bool
}

// Plan is the full graph plus the numbers it was derived from.
type Plan struct {
	Days     []Day
	Weeks    int
	Offset   int    // first column of the pattern
	Complete string // date the pattern's last cell arrives
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

// Count is the window's total contributions once the plan is painted.
func (p Plan) Count() int {
	n := 0
	for _, d := range p.Days {
		n += d.Real + d.Add
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

// autoStep rounds an auto-picked volume to a tidy number.
const autoStep = 50

// Build plans the graph, from the calendar's first day to opts.Ahead days
// after today. The calendar must not contain an older canvas: delete that
// repo, and wait for the total to drop, before planning a new one.
func Build(cal calendar.Calendar, pat pattern.Pattern, opts Options) (Plan, error) {
	p := Plan{Weeks: len(cal.Weeks)}

	for c, week := range cal.Weeks {
		for _, d := range week {
			p.RealMax = max(p.RealMax, d.Count)
			p.Days = append(p.Days, Day{Date: d.Date, Col: c, Row: d.Weekday, Real: d.Count})
		}
	}
	future, err := ahead(p.Days[len(p.Days)-1], opts.Ahead)
	if err != nil {
		return Plan{}, err
	}
	p.Days = append(p.Days, future...)

	if p.Offset, p.Complete, err = place(cal, pat, p.Days, opts.Start); err != nil {
		return Plan{}, err
	}

	p.Volume = opts.Volume
	if p.Volume == 0 {
		// Background puts real days at real+1, which must stay within level 1.
		// The extra quarter is headroom for busier days to come.
		p.Volume = roundUp(5*(p.RealMax+1), autoStep)
	}
	if p.Volume/4 == 0 {
		return Plan{}, errors.New("volume must be at least 4")
	}

	draw(&p, pat, opts.Background)
	if opts.Total > 0 {
		if err := fill(&p, pat, opts); err != nil {
			return Plan{}, err
		}
	}

	q := p.Volume / 4
	if p.RealMax+1 > q {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"busiest real day has %d contributions; at volume %d anything above %d shows darker than the background",
			p.RealMax, p.Volume, q-1))
	}
	peak := 0
	for _, d := range p.Days {
		if d.Shade > 0 && d.Shade < 4 && d.Real > d.Shade*q {
			p.Warnings = append(p.Warnings, fmt.Sprintf(
				"%s: %d real contributions already exceed shade %d", d.Date, d.Real, d.Shade))
		}
		peak = max(peak, d.Real+d.Add)
	}
	for i := range p.Days {
		p.Days[i].Level = Level(p.Days[i].Real+p.Days[i].Add, peak)
	}

	return p, nil
}

// draw sets each day's shade and the commits it needs at the plan's volume.
func draw(p *Plan, pat pattern.Pattern, background bool) {
	q := p.Volume / 4
	for i := range p.Days {
		d := &p.Days[i]
		d.Shade, d.Add = 0, 0
		if col := d.Col - p.Offset; col >= 0 && col < pat.Width() {
			d.Shade = pat.Cells[d.Row][col]
		}

		switch {
		case d.Shade == 4:
			d.Add = max(p.Volume-d.Real, 0)
		case d.Shade > 0:
			// Aim at the middle of the level's band, away from both edges.
			target := (2*d.Shade - 1) * q / 2
			d.Add = max(target-d.Real, 0)
		case background:
			d.Add = 1
		}
	}
}

// fill brings the window to exactly opts.Total. Each lit day below the darkest
// shade has room up to the top of its shade's band; the volume is raised until
// that room holds what the drawing leaves short, which is then spread over the
// days in proportion to their room, so no day is pushed to its band's edge.
func fill(p *Plan, pat pattern.Pattern, opts Options) error {
	for ; p.Volume <= opts.Total; p.Volume += autoStep {
		draw(p, pat, opts.Background)
		short := opts.Total - p.Count()
		if short < 0 {
			return fmt.Errorf(
				"total %d is below the %d the drawing takes at volume %d",
				opts.Total,
				p.Count(),
				p.Volume,
			)
		}

		q := p.Volume / 4
		room := make([]int, len(p.Days))
		sum := 0
		for i, d := range p.Days {
			top := 0
			switch {
			case opts.Fixed[d.Date], d.Shade == 4:
			case d.Shade > 0:
				top = d.Shade * q
			case d.Real+d.Add > 0:
				top = q
			}
			room[i] = max(top-d.Real-d.Add, 0)
			sum += room[i]
		}
		if short > sum {
			continue
		}

		// Shares round down, each by less than one, so one more commit on
		// that many days settles the rest without leaving any day's room.
		given := 0
		for i, r := range room {
			n := short * r / max(sum, 1)
			p.Days[i].Add += n
			room[i] -= n
			given += n
		}
		for i := range p.Days {
			if given == short {
				break
			}
			if room[i] > 0 {
				p.Days[i].Add++
				given++
			}
		}

		return nil
	}

	return fmt.Errorf("total %d is out of reach: the days have no room to hold it", opts.Total)
}

// place picks the pattern's first column and the date its last cell arrives.
func place(cal calendar.Calendar, pat pattern.Pattern, days []Day, start string) (int, string, error) {
	var offset int
	if start == "" {
		usable := fullPastWeeks(cal)
		if len(usable) < pat.Width() {
			return 0, "", fmt.Errorf("pattern is %d weeks wide, only %d full weeks available", pat.Width(), len(usable))
		}
		offset = usable[0] + (len(usable)-pat.Width())/2
	} else {
		i := slices.IndexFunc(days, func(d Day) bool { return d.Date == start })
		if i < 0 {
			return 0, "", fmt.Errorf("start %s is outside the window and the days ahead", start)
		}
		offset = days[i].Col
	}

	last := offset + pat.Width() - 1
	for _, d := range days {
		if d.Col == last && d.Row == pattern.Rows-1 {
			return offset, d.Date, nil
		}
	}

	return 0, "", errors.New("pattern ends past the last planned day; raise ahead")
}

// ahead lists the n days after last, continuing its week columns.
func ahead(last Day, n int) ([]Day, error) {
	day, err := time.Parse(time.DateOnly, last.Date)
	if err != nil {
		return nil, fmt.Errorf("bad calendar date %q: %w", last.Date, err)
	}
	days := make([]Day, 0, n)
	for k := 1; k <= n; k++ {
		days = append(days, Day{
			Date:   day.AddDate(0, 0, k).Format(time.DateOnly),
			Col:    last.Col + (last.Row+k)/pattern.Rows,
			Row:    (last.Row + k) % pattern.Rows,
			Future: true,
		})
	}

	return days, nil
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
