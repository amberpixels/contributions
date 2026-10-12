# contributions

Draws a pattern on my GitHub contribution graph.

The repo's `main` branch is its code commits followed by a chain of empty,
backdated "pixel" commits. A daily Action reads the live calendar, works out how
many commits each day needs on top of my real activity, and rewrites the chain.

```
contributions plan     # preview in the terminal (+ --svg file), changes nothing
contributions apply    # rewrite main locally; --push to force-push it
contributions verify   # compare the live graph with the plan
```

## How it draws

GitHub shades a day by its share of the busiest day in the window: with
`q = floor(max/4)`, counts `1..q` are shade 1, `q+1..2q` shade 2, and so on.
This was measured against a real calendar, not documented, which is what
`verify` is for.

So a darkest pixel gets `volume` commits, and every day with real activity stays
in shade 1 as long as it is below `volume/4`. With `background: true` every day
also gets one commit, so the graph has no gaps. Real activity can only be
outshone, never hidden: a day cannot show less than what was actually done.

The pattern is centered in the full past weeks of the window. The first column
can be partial, and the current week has not happened yet. On Sundays the window
shifts, the pattern moves to the next week's dates, and the chain is redrawn.
Other days only append today's background commit, which is a fast-forward.

## Config

`contributions.yml`:

```yaml
pattern: art/ship-it.txt   # 7 rows, Sunday first; space = empty, ░▒▓█ = shades 1-4
background: true           # one commit on every day
volume: 0                  # commits on a darkest day; 0 = auto from the busiest real day
```

`branch` (default `main`), `remote` (default `origin`) and `author.name` /
`author.email` (default: GitHub login and its noreply address) can be set too.
The author email must be one GitHub links to the account, or nothing counts.

## Setup

1. Create the GitHub repo and push `main` once (code commits only).
2. Add a `CONTRIBUTIONS_TOKEN` secret: a personal access token that can read the
   account's contributions and push to this repo, workflow files included (the
   rewritten history contains them). `GITHUB_TOKEN` cannot do this: it would read
   the bot's calendar.
3. Run the Paint workflow once by hand, or wait for the daily schedule.

Locally the token comes from `CONTRIBUTIONS_TOKEN`, `GITHUB_TOKEN` or `gh auth token`.

## Caveats

- Every redraw adds roughly `pixels × volume` commits to the year's headline
  count (about 27k at volume 300 for the SHIP IT pattern).
- Code committed on top of pixel commits is kept: the next run re-parents it onto
  the code before the chain, so local clones need `git pull --rebase`.
- GitHub disables scheduled workflows in public repos after 60 days without
  activity. The daily pushes should count as activity, but if the graph stops
  updating, re-enable the workflow.
- Only pushed pixels are subtracted from the calendar. If GitHub has not counted
  a push yet, the next run sees inflated real activity for those days.
