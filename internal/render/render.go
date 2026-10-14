// Package render draws a plan the way GitHub would show it: as block
// characters for the terminal, or as an SVG in GitHub's own colors.
package render

import (
	"fmt"
	"strings"

	"github.com/amberpixels/contributions/internal/pattern"
	"github.com/amberpixels/contributions/internal/plan"
)

var blocks = []rune{'·', '░', '▒', '▓', '█'}

// Text renders the planned graph as 7 lines of block characters.
func Text(p plan.Plan) string {
	grid := cells(p)
	var b strings.Builder
	for _, row := range grid {
		for _, lvl := range row {
			if lvl < 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(blocks[lvl])
			}
		}
		b.WriteByte('\n')
	}

	return b.String()
}

type theme struct {
	name, bg, fg string
	levels       [5]string
}

var themes = []theme{
	{"dark", "#0d1117", "#e6edf3", [5]string{"#161b22", "#0e4429", "#006d32", "#26a641", "#39d353"}},
	{"light", "#ffffff", "#1f2328", [5]string{"#ebedf0", "#9be9a8", "#40c463", "#30a14e", "#216e39"}},
}

// SVG renders the planned graph in GitHub's dark and light palettes, one
// above the other.
func SVG(p plan.Plan) string {
	const cell, gap, pad, title = 11, 3, 20, 22
	grid := cells(p)
	gridH := pattern.Rows*(cell+gap) - gap
	w := p.Weeks*(cell+gap) - gap + 2*pad
	h := len(themes) * (title + gridH + pad + pad)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`+"\n", w, h)
	y := 0
	for _, t := range themes {
		panelH := title + gridH + 2*pad
		fmt.Fprintf(&b, `<rect y="%d" width="%d" height="%d" fill="%s"/>`+"\n", y, w, panelH, t.bg)
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-family="Helvetica, Arial, sans-serif" font-size="13">`+
			`%s: +%d commits, volume %d</text>`+"\n", pad, y+pad+12, t.fg, t.name, p.Added(), p.Volume)
		top := y + pad + title
		for r, row := range grid {
			for c, lvl := range row {
				if lvl < 0 {
					continue
				}
				fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s"/>`+"\n",
					pad+c*(cell+gap), top+r*(cell+gap), cell, cell, t.levels[lvl])
			}
		}
		y += panelH
	}
	b.WriteString("</svg>\n")

	return b.String()
}

// cells lays the plan's visible window out as rows by weeks, -1 where the
// window has no day. Days ahead are kept only for the current week.
func cells(p plan.Plan) [pattern.Rows][]int {
	var grid [pattern.Rows][]int
	for r := range grid {
		grid[r] = make([]int, p.Weeks)
		for c := range grid[r] {
			grid[r][c] = -1
		}
	}
	for _, d := range p.Days {
		if d.Col < p.Weeks {
			grid[d.Row][d.Col] = d.Level
		}
	}

	return grid
}
