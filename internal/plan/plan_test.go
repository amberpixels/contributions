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
	p, err := plan.Build(cal, dot(t), plan.Options{Background: true})
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

func TestBuildAhead(t *testing.T) {
	cal := fakeCalendar(func(int) int { return 0 }) // ends on a Sunday, column 52
	p, err := plan.Build(cal, dot(t), plan.Options{Background: true, Volume: 100, Ahead: 8})
	if err != nil {
		t.Fatal(err)
	}

	future := p.Days[len(p.Days)-8:]
	// Monday..Saturday finish the current column, the next Sunday opens column 53.
	for i, d := range future {
		wantCol, wantRow := 52, i+1
		if i >= 6 {
			wantCol, wantRow = 53, i-6
		}
		if !d.Future || d.Col != wantCol || d.Row != wantRow || d.Add != 1 {
			t.Errorf("ahead day %d (%s): col %d row %d add %d future %v", i, d.Date, d.Col, d.Row, d.Add, d.Future)
		}
	}
	if future[0].Date != "2026-10-12" || future[7].Date != "2026-10-19" {
		t.Errorf("ahead dates %s..%s", future[0].Date, future[7].Date)
	}
}

func TestBuildTooWide(t *testing.T) {
	wide, err := pattern.Parse("1111111111111111111111111111111111111111111111111111111\n\n\n\n\n\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Build(fakeCalendar(func(int) int { return 0 }), wide, plan.Options{}); err == nil {
		t.Error("expected an error for a pattern wider than the window")
	}
}

func TestBuildStart(t *testing.T) {
	cal := fakeCalendar(func(int) int { return 0 })
	p, err := plan.Build(cal, dot(t), plan.Options{Volume: 100, Ahead: 14, Start: "2025-10-22"})
	if err != nil {
		t.Fatal(err)
	}
	// A Wednesday pins the column of its week; the column completes on Saturday.
	if p.Offset != 1 || p.Complete != "2025-10-25" {
		t.Errorf("offset %d complete %s, want 1 and 2025-10-25", p.Offset, p.Complete)
	}

	// A start inside the days ahead is fine; past them is not.
	if p, err = plan.Build(cal, dot(t), plan.Options{Volume: 100, Ahead: 14, Start: "2026-10-18"}); err != nil {
		t.Fatal(err)
	}
	if p.Complete != "2026-10-24" {
		t.Errorf("future start completes %s, want 2026-10-24", p.Complete)
	}
	if _, err := plan.Build(cal, dot(t), plan.Options{Volume: 100, Ahead: 3, Start: "2026-10-18"}); err == nil {
		t.Error("expected an error for a start past the days ahead")
	}
}

func TestBuildTotal(t *testing.T) {
	cal := fakeCalendar(func(i int) int { return i % 11 })
	fixed := map[string]bool{"2025-10-12": true}
	plain, err := plan.Build(cal, dot(t), plan.Options{Background: true, Volume: 100})
	if err != nil {
		t.Fatal(err)
	}

	for _, total := range []int{plain.Count(), 5000, 66777} {
		p, err := plan.Build(cal, dot(t), plan.Options{Background: true, Volume: 100, Total: total, Fixed: fixed})
		if err != nil {
			t.Fatalf("total %d: %v", total, err)
		}
		if p.Count() != total {
			t.Errorf("total %d: count %d", total, p.Count())
		}
		// Filler must not change a single shade, at whatever volume it took.
		ref, err := plan.Build(cal, dot(t), plan.Options{Background: true, Volume: p.Volume})
		if err != nil {
			t.Fatal(err)
		}
		for i, d := range p.Days {
			if d.Level != ref.Days[i].Level {
				t.Errorf("total %d, %s: level %d, want %d", total, d.Date, d.Level, ref.Days[i].Level)
			}
			if fixed[d.Date] && d.Add != ref.Days[i].Add {
				t.Errorf("total %d: fixed day %s got filler", total, d.Date)
			}
		}
	}

	if _, err := plan.Build(
		cal,
		dot(t),
		plan.Options{Background: true, Volume: 100, Total: plain.Count() - 1},
	); err == nil {
		t.Error("expected an error for a total below what the drawing takes")
	}
}
