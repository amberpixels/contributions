package main

import (
	"errors"
	"fmt"
	"os"

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
	// Branch is the branch GitHub counts contributions on: the default one.
	Branch string `yaml:"branch"`
	// Remote is where `apply --push` pushes.
	Remote string `yaml:"remote"`
	// Author overrides the identity of pixel commits. Empty fields fall back
	// to the GitHub login and its noreply address.
	Author struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"author"`
}

func loadConfig(path string) (config, error) {
	c := config{Branch: "main", Remote: "origin"}

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
	if c.Volume < 0 {
		return c, errors.New("config: volume must be 0 (auto) or positive")
	}

	return c, nil
}
