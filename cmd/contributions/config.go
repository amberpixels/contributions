package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"
)

const configFile = "contributions.yml"

type config struct {
	// Pattern is the path of the pattern file, relative to the repo root.
	Pattern string `yaml:"pattern"`
	// Background fills every day up to the lightest shade.
	Background bool `yaml:"background"`
	// Volume is the commit count of a darkest-shade day; 0 picks it automatically.
	Volume int `yaml:"volume"`
	// Ahead is how many future days get their background in advance.
	Ahead int `yaml:"ahead"`
	// Start pins the pattern's first column to this date's week; empty centers
	// it in the window.
	Start string `yaml:"start"`
	// Years also draws the pattern in these calendar years' own views, from
	// each year's first full week.
	Years []int `yaml:"years"`
	// Totals pins a past year's view to an exact contribution count. The year
	// must be one of Years.
	Totals map[int]int `yaml:"totals"`
	// Canvas is the empty GitHub repo the generated history is pushed to.
	Canvas string `yaml:"canvas"`
	// Author overrides the identity of canvas commits. Empty fields fall back
	// to the GitHub login and its noreply address.
	Author struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"author"`
}

func loadConfig(path string) (config, error) {
	c := config{Ahead: 365}

	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Pattern == "" {
		return c, errors.New("config: pattern is required")
	}
	if c.Volume < 0 || c.Ahead < 0 {
		return c, errors.New("config: volume and ahead must not be negative")
	}

	for y, n := range c.Totals {
		if !slices.Contains(c.Years, y) {
			return c, fmt.Errorf("config: totals has %d, which is not in years", y)
		}
		if y >= time.Now().Year() {
			return c, fmt.Errorf("config: %d is not over yet; only a past year's total stays put", y)
		}
		if n <= 0 {
			return c, fmt.Errorf("config: total for %d must be positive", y)
		}
	}

	return c, nil
}
