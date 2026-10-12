package plan_test

import (
	"testing"
	"time"

	"github.com/amberpixels/contributions/internal/calendar"
	"github.com/amberpixels/contributions/internal/pattern"
	"github.com/amberpixels/contributions/internal/plan"
)

// The thresholds measured on a real calendar whose busiest day had 51.
func TestLevelMatchesGitHub(t *testing.T) {
	for total, want := range map[int]int{0: 0, 1: 1, 12: 1, 13: 2, 24: 2, 25: 3, 36: 3, 38: 4, 51: 4} {
		if got := plan.Level(total, 51); got != want {
			t.Errorf("Level(%d, 51) = %d, want %d", total, got, want)
		}
	}
}

// fakeCalendar has 53 weeks starting on a Sunday, the last one holding only
// that Sunday, with count(i) contributions on day i.
func fakeCalendar(count func(i int) int) calendar.Calendar {
	start := time.Date(2025, 10, 12, 0, 0, 0, 0, time.UTC)
	var cal calendar.Calendar
	i := 0
	for w := range 53 {
		days := 7
		if w == 52 {
			days = 1
		}
		var week []calendar.Day
		for d := range days {
			week = append(week, calendar.Day{
				Date:    start.AddDate(0, 0, i).Format(time.DateOnly),
				Weekday: d,
				Count:   count(i),
			})
			i++
		}
		cal.Weeks = append(cal.Weeks, week)
	}

	return cal
}

func dot(t *testing.T) pattern.Pattern {
	t.Helper()
	p, err := pattern.Parse("4\n3\n\n\n\n\n\n")
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestBuild(t *testing.T) {
	cal := fakeCalendar(func(i int) int { return i % 11 }) // busiest real day: 10
	p, err := plan.Build(cal, dot(t), nil, plan.Options{Background: true})
	if err != nil {
		t.Fatal(err)
	}

	if p.Volume != 100 { // 5*(10+1) rounded up to 50
		t.Errorf("volume = %d, want 100", p.Volume)
	}
	if p.Offset != 25 { // a 1-wide pattern centered in 52 full past weeks
		t.Errorf("offset = %d, want 25", p.Offset)
	}
	if len(p.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", p.Warnings)
	}

	for _, d := range p.Days {
		switch {
		case d.Col == p.Offset && d.Row == 0:
			if d.Real+d.Add != p.Volume || d.Level != 4 {
				t.Errorf("darkest pixel: real %d + add %d, level %d", d.Real, d.Add, d.Level)
			}
		case d.Col == p.Offset && d.Row == 1:
			if d.Level != 3 {
				t.Errorf("shade-3 pixel: real %d + add %d, level %d", d.Real, d.Add, d.Level)
			}
		default:
			if d.Add != 1 || d.Level != 1 {
				t.Errorf("%s background: add %d, level %d", d.Date, d.Add, d.Level)
			}
		}
	}
}

func TestBuildSubtractsPainted(t *testing.T) {
	cal := fakeCalendar(func(int) int { return 0 })
	first, err := plan.Build(cal, dot(t), nil, plan.Options{Background: true})
	if err != nil {
		t.Fatal(err)
	}

	// Once drawn, the calendar counts the plan's own commits.
	painted := first.Painted()
	for _, week := range cal.Weeks {
		for i := range week {
			week[i].Count += painted[week[i].Date]
		}
	}
	again, err := plan.Build(cal, dot(t), painted, plan.Options{Background: true})
	if err != nil {
		t.Fatal(err)
	}
	if again.Volume != first.Volume || again.Added() != first.Added() {
		t.Errorf("redraw changed the plan: volume %d -> %d, added %d -> %d",
			first.Volume, again.Volume, first.Added(), again.Added())
	}
}

func TestBuildTooWide(t *testing.T) {
	wide, err := pattern.Parse("1111111111111111111111111111111111111111111111111111111\n\n\n\n\n\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Build(fakeCalendar(func(int) int { return 0 }), wide, nil, plan.Options{}); err == nil {
		t.Error("expected an error for a pattern wider than the window")
	}
}
