// Package pattern parses the 7-row text files that describe what to draw.
//
// Each row is one weekday (Sunday first), each character one week. A cell is
// either empty (space or '.') or a shade from 1 to 4, written as a digit or as
// the matching block character, so the file itself looks like the result.
package pattern

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Rows is the number of weekdays, i.e. the fixed height of every pattern.
const Rows = 7

// Pattern is a grid of shades: Cells[row][col] is 0 (empty) to 4 (darkest).
type Pattern struct {
	Cells [Rows][]int
}

// Width is the number of columns (weeks) the pattern occupies.
func (p Pattern) Width() int { return len(p.Cells[0]) }

var shades = map[rune]int{
	' ': 0, '.': 0, '0': 0,
	'1': 1, '░': 1,
	'2': 2, '▒': 2,
	'3': 3, '▓': 3,
	'4': 4, '█': 4, '#': 4,
}

// Load reads and parses a pattern file.
func Load(path string) (Pattern, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Pattern{}, fmt.Errorf("read pattern: %w", err)
	}

	return Parse(string(data))
}

// Parse turns pattern text into a grid. Rows may differ in length; shorter ones
// are padded with empty cells, so trailing spaces in the file do not matter.
// Empty rows do matter: only the file's final newline is ignored.
func Parse(text string) (Pattern, error) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != Rows {
		return Pattern{}, fmt.Errorf("pattern must have %d rows, got %d", Rows, len(lines))
	}

	width := 0
	for _, l := range lines {
		width = max(width, utf8.RuneCountInString(strings.TrimRight(l, "\r")))
	}
	if width == 0 {
		return Pattern{}, errors.New("pattern is empty")
	}

	var p Pattern
	for r, l := range lines {
		p.Cells[r] = make([]int, width)
		c := 0
		for _, ch := range strings.TrimRight(l, "\r") {
			v, ok := shades[ch]
			if !ok {
				return Pattern{}, fmt.Errorf("row %d col %d: unknown character %q", r+1, c+1, ch)
			}
			p.Cells[r][c] = v
			c++
		}
	}

	return p, nil
}
