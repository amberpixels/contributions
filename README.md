# contributions

Draws a pattern on my GitHub contribution graph.

It generates a history of empty commits, backdated and future-dated, sized
against my real activity, and pushes it once into an empty repo (the canvas,
[amberpixels/empty-spaces](https://github.com/amberpixels/empty-spaces)).
Nothing runs afterwards: GitHub counts a future-dated commit once its day
arrives.

```
contributions plan          # preview in the terminal (+ --svg file), changes nothing
contributions paint         # generate the canvas in .canvas
contributions paint --push  # ... and push it into the empty canvas repo
contributions verify        # check GitHub counted all of it and shades as predicted
```

## What GitHub does with it

Measured, not documented, so `verify` exists to catch changes:

- **Shades** are a day's share of the busiest day in the window: counts up to
  about `max/4` are shade 1, up to `max/2` shade 2, and so on.
- **A first push into an empty repo counts fully**: 247k commits in one push
  were all counted. An update push to an existing branch only got its newest
  ~1000 commits counted.
- **Force-pushing commits away does not un-count them**, not even two days
  later. **Deleting the repo does**, within minutes.

So a redraw is: `j canvas:reset` (deletes and recreates the canvas repo),
wait for the total to drop, `paint --push` again. `paint --push` refuses a
canvas repo that is not empty.

## How it draws

A darkest pixel gets `volume` commits. Every day with real activity stays in
shade 1 as long as it is below `volume/4`. With `background: true` every day,
including the next `ahead` days, also gets one commit, so the graph has no
gaps. Real activity can only be outshone, never hidden.

Without `start`, the pattern is centered in the full past weeks of today's
window. With `start`, its first column is that date's week; columns past today
fill in as their days arrive.

Each view is shaded against its own busiest day, so a label only reads right
in the views that contain all of it. A label inside the current year, from its
first full week, is whole in both "last 12 months" and that year's view. `years`
draws the same label in past years' views, at the same spot, and `plan` refuses
one that would reach into the last-12-months window. The rolling view moves a
column every Sunday, so its label slides left and, in the next year, off the
edge.

`totals` pins a past year's "N contributions in" number. Every shade is a band
(at volume 300, a background day holds 1 to 75 and stays lightest), so the
missing commits are spread over the days in proportion to the room left in
their band, and no day changes shade. If the room is not enough, that year's
volume rises in steps of 50. Days the rolling view also shows get no filler,
since they would show darker there. Only finished years: the current one keeps
moving.

## Config

`contributions.yml`:

```yaml
pattern: art/who-cares.txt # 7 rows, Sunday first; space = empty, ░▒▓█ = shades 1-4
background: true           # one commit on every day
volume: 300                # commits on a darkest day; 0 = auto from the busiest real day
start: 2026-01-04          # first column's week; empty = centered in the window
years: [2025, 2024]        # past years' views that show the pattern too
totals: {2024: 66777, 2025: 77666} # exact counts for past years' views
ahead: 365                 # future days that get their background in advance
canvas: git@github.com:amberpixels/empty-spaces.git
```

`author.name` / `author.email` default to the GitHub login and its noreply
address. The email must be one GitHub links to the account, or nothing counts.

`j edit` opens `editor.html`, a grid editor in GitHub's colors that reads and
writes this pattern format.

The calendar is read with `CONTRIBUTIONS_TOKEN`, `GITHUB_TOKEN` or `gh auth token`.
