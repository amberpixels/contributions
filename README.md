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

So a redraw is: delete the canvas repo on GitHub, wait for the total to drop,
recreate it empty, `paint --push` again. `paint --push` refuses a canvas repo
that is not empty.

## How it draws

A darkest pixel gets `volume` commits. Every day with real activity stays in
shade 1 as long as it is below `volume/4`. With `background: true` every day,
including the next `ahead` days, also gets one commit, so the graph has no
gaps. Real activity can only be outshone, never hidden.

The pattern is centered in the full past weeks of today's window. The window
moves a column every Sunday and the pattern moves with it, sliding left until it
falls off the edge.

## Config

`contributions.yml`:

```yaml
pattern: art/ship-it.txt   # 7 rows, Sunday first; space = empty, ░▒▓█ = shades 1-4
background: true           # one commit on every day
volume: 300                # commits on a darkest day; 0 = auto from the busiest real day
ahead: 365                 # future days that get their background in advance
canvas: git@github.com:amberpixels/empty-spaces.git
```

`author.name` / `author.email` default to the GitHub login and its noreply
address. The email must be one GitHub links to the account, or nothing counts.

The calendar is read with `CONTRIBUTIONS_TOKEN`, `GITHUB_TOKEN` or `gh auth token`.
