# Running & deploying SIGINT

Operational notes. The front-facing overview is in [README.md](README.md).

## Running it

```bash
export SCORING_API_KEY=...

go run ./cmd/edition -dry              # fetch and dedupe only, spend nothing
go run ./cmd/edition                   # the real thing
go run ./cmd/edition -requadrant       # redraw quadrants after editing AxisHigh

go run ./cmd/edition -label eval.csv   # write blank score columns
go run ./cmd/edition -compare eval.csv # see where the rubric disagrees with you

cd docs && python3 -m http.server 8000 # then open localhost:8000
```

Stdlib only. No `go get`, no `npm`, no build step, no site generator.
Serve the page — never open `docs/index.html` as a `file://`, or the
`edition.json` fetch is blocked and the page hangs on "Loading".

## Layout

```
cmd/edition/main.go       the five steps, wired together
internal/config/          brand, sources, keywords, thresholds — start here
internal/model/           the Item struct, URL canonicalization, CVE extraction
internal/fetch/           arxiv.go, hn.go, kev.go
internal/score/           the rubric. this is the product.
internal/label/           CSV round-trip for -label and -compare
internal/store/           archive, dedupe, edition building, prior-coverage link
docs/                     index.html, style.css, app.js — hand written; Pages root
data/scored.json          the archive (committed; it is the seen-set)
.github/workflows/        the 30-minute cron
```

## Renaming

Everything brand-facing is in `internal/config/config.go`: `Name`, `Mark`,
`Standfirst`, `Colophon`. Change those four and the `<title>` in
`docs/index.html`.

## Before you deploy — fill these placeholders

| Placeholder | File | What to put |
|---|---|---|
| `firstRun` | `cmd/edition/main.go` | launch date, `time.Date(YYYY, M, D, ...)` |
| `MYCODE` | `docs/index.html` | GoatCounter code (goatcounter.com signup, free) |
| Kit form id | `docs/index.html` | already set to your form endpoint |
| `userAgent` | `internal/fetch/arxiv.go` | a resolving URL + contactable email |

## Deploy runbook

One at a time. Debugging DNS and Pages together is misery.

1. **Local green first.** `go vet ./...`, then `go run ./cmd/edition`. Confirm
   it writes `docs/edition.json` and renders at `cd docs && python3 -m http.server 8000`.
2. **Public repo.** Confirm no key leaked: `grep -rn "sk-ant-[A-Za-z0-9]" .` returns nothing.
3. **Push.** The archive (`data/scored.json`) commits too — it is the seen-set.
4. **Settings → Pages** → deploy from `main`, folder `/docs`. Verify at the
   `github.io` URL before touching the domain.
5. **Settings → Secrets → Actions** → add `SCORING_API_KEY`. Then Actions tab →
   run the `edition` workflow manually once. Watch it go green.
6. **Buy the domain** (confirm flat renewal). ALIAS on the apex → `<you>.github.io`,
   `www` CNAME → same.
7. **Settings → Pages → Custom domain** → wait for the check, tick Enforce HTTPS.
8. **Cross-link** with jaysrinivasan.dev, both directions.

## Cost control

- `MaxScorePerRun` in config is the fuse: a dedup bug can never bill you for
  more than that many items in one run. The Console spend cap is the backstop.
- Steady state on the current sources is a few dollars a month. First run is
  the only spike.
- To cut further: change the cron in `.github/workflows/edition.yml` from
  `*/30` to `0 */2 * * *`. Quarters the cost; a newspaper does not need
  30-minute freshness.

## The scoring loop (optional but worth an hour)

`-label eval.csv`, score 30-50 items yourself by gut in one sitting,
`go run ./cmd/edition`, then `-compare eval.csv`. Aim for 6-7 of 10 top-ten
overlap. Edit the anchors in `internal/score/score.go` for whichever level is
wrong, re-run against the same file. Two or three rounds. KEV items are correct
by construction, so the rubric only has to carry arXiv and HN.

## Etiquette

This audience will check. Public APIs only, never scraping. arXiv gets one
request per 3 seconds and a contactable User-Agent (set in
`internal/fetch/arxiv.go`). Headlines, links, and our own one-line summary;
never anyone's full text.
