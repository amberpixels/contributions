package pattern_test

import (
	"testing"

	"github.com/amberpixels/contributions/internal/pattern"
)

func TestLoadShipIt(t *testing.T) {
	p, err := pattern.Load("../../art/ship-it.txt")
	if err != nil {
		t.Fatal(err)
	}
	if p.Width() != 47 {
		t.Errorf("width = %d, want 47", p.Width())
	}
	// Top-left of the "/" in "</>" and the first "S" pixel.
	if p.Cells[0][6] != 4 || p.Cells[3][0] != 3 || p.Cells[0][0] != 0 {
		t.Errorf("unexpected cells in row 0/3: %v / %v", p.Cells[0][:8], p.Cells[3][:8])
	}
}

func TestParseShades(t *testing.T) {
	p, err := pattern.Parse(".1234\n░▒▓█#\n\n\n\n\n\n")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]int{{0, 1, 2, 3, 4}, {1, 2, 3, 4, 4}}
	for r, row := range want {
		for c, v := range row {
			if p.Cells[r][c] != v {
				t.Errorf("cell %d,%d = %d, want %d", r, c, p.Cells[r][c], v)
			}
		}
	}
	if p.Cells[6][4] != 0 {
		t.Error("short rows should be padded with empty cells")
	}
}

func TestParseErrors(t *testing.T) {
	for name, text := range map[string]string{
		"too few rows": "1\n1\n",
		"unknown char": "x\n\n\n\n\n\n\n",
		"empty":        "\n\n\n\n\n\n\n",
	} {
		if _, err := pattern.Parse(text); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
