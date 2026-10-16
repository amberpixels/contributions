// Package calendar fetches the authenticated user's contribution calendar from
// the GitHub GraphQL API.
package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const endpoint = "https://api.github.com/graphql"

// Day is one cell of the contribution graph.
type Day struct {
	Date    string // YYYY-MM-DD
	Weekday int    // 0 = Sunday
	Count   int
	Level   int // 0..4, as GitHub currently renders it
}

// Calendar is the rolling one-year graph, one slice of days per week column.
// The first and last weeks may be partial.
type Calendar struct {
	Login string
	ID    int64
	Weeks [][]Day
}

// NoreplyEmail is the address GitHub attributes to this account without
// exposing a real one.
func (c Calendar) NoreplyEmail() string {
	return fmt.Sprintf("%d+%s@users.noreply.github.com", c.ID, c.Login)
}

const query = `query($from: DateTime, $to: DateTime) {
  viewer {
    login
    databaseId
    contributionsCollection(from: $from, to: $to) {
      contributionCalendar {
        weeks { contributionDays { date weekday contributionCount contributionLevel } }
      }
    }
  }
}`

var levels = map[string]int{
	"NONE":            0,
	"FIRST_QUARTILE":  1,
	"SECOND_QUARTILE": 2,
	"THIRD_QUARTILE":  3,
	"FOURTH_QUARTILE": 4,
}

type response struct {
	Data struct {
		Viewer struct {
			Login                   string `json:"login"`
			DatabaseID              int64  `json:"databaseId"`
			ContributionsCollection struct {
				ContributionCalendar struct {
					Weeks []struct {
						ContributionDays []struct {
							Date              string `json:"date"`
							Weekday           int    `json:"weekday"`
							ContributionCount int    `json:"contributionCount"`
							ContributionLevel string `json:"contributionLevel"`
						} `json:"contributionDays"`
					} `json:"weeks"`
				} `json:"contributionCalendar"`
			} `json:"contributionsCollection"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Fetch loads the calendar of whoever owns the token: the rolling last year
// for year 0, otherwise that calendar year, as its own view on the profile.
func Fetch(ctx context.Context, token string, year int) (Calendar, error) {
	vars := map[string]any{}
	if year != 0 {
		vars["from"] = fmt.Sprintf("%d-01-01T00:00:00Z", year)
		vars["to"] = fmt.Sprintf("%d-12-31T23:59:59Z", year)
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return Calendar{}, fmt.Errorf("encode query: %w", err)
	}

	var r response
	for attempt := 1; ; attempt++ {
		var retry bool
		r, retry, err = post(ctx, token, body)
		if err == nil || !retry || attempt == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return Calendar{}, ctx.Err()
		case <-time.After(time.Duration(attempt) * 5 * time.Second):
		}
	}
	if err != nil {
		return Calendar{}, err
	}

	v := r.Data.Viewer
	cal := Calendar{Login: v.Login, ID: v.DatabaseID}
	for _, w := range v.ContributionsCollection.ContributionCalendar.Weeks {
		week := make([]Day, 0, len(w.ContributionDays))
		for _, d := range w.ContributionDays {
			week = append(week, Day{
				Date:    d.Date,
				Weekday: d.Weekday,
				Count:   d.ContributionCount,
				Level:   levels[d.ContributionLevel],
			})
		}
		cal.Weeks = append(cal.Weeks, week)
	}
	if len(cal.Weeks) == 0 {
		return Calendar{}, errors.New("github returned an empty calendar")
	}

	return cal, nil
}

// PublicTotal reads the "N contributions in YEAR" header of a year's view as a
// logged-out visitor sees it. It can differ from the API's sum of days: private
// contributions the token cannot read, and real activity near midnight that the
// page files under another day, possibly another year.
func PublicTotal(ctx context.Context, login string, year int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://github.com/users/%s/contributions?from=%d-01-01&to=%d-12-31", login, year, year)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch %d profile view: %w", year, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetch %d profile view: %s", year, resp.Status)
	}
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read %d profile view: %w", year, err)
	}

	header := regexp.MustCompile(`([\d,]+)\s+contributions?\s+in\s+` + strconv.Itoa(year))
	m := header.FindSubmatch(page)
	if m == nil {
		return 0, fmt.Errorf("no contributions header in the %d profile view", year)
	}

	return strconv.Atoi(strings.ReplaceAll(string(m[1]), ",", ""))
}

// attempts bounds retries of GitHub's occasional 502/504 on this query.
const attempts = 4

// post runs the query once. retry reports whether a failure is worth retrying.
func post(ctx context.Context, token string, body []byte) (response, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return response{}, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return response{}, true, fmt.Errorf("query github: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return response{}, resp.StatusCode >= 500, fmt.Errorf("query github: %s", resp.Status)
	}

	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return response{}, false, fmt.Errorf("decode response: %w", err)
	}
	if len(r.Errors) > 0 {
		return response{}, false, fmt.Errorf("github: %s", r.Errors[0].Message)
	}

	return r, false, nil
}

// Token finds a GitHub token: CONTRIBUTIONS_TOKEN, then GITHUB_TOKEN, then the
// gh CLI's stored login.
func Token(ctx context.Context) (string, error) {
	for _, env := range []string{"CONTRIBUTIONS_TOKEN", "GITHUB_TOKEN"} {
		if t := os.Getenv(env); t != "" {
			return t, nil
		}
	}

	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return "", errors.New("no token: set CONTRIBUTIONS_TOKEN or log in with `gh auth login`")
	}

	return strings.TrimSpace(string(out)), nil
}
