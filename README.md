# SIGINT

Security research and practitioner news, scored for novelty and actionability.

Signals intelligence — and, to anyone who has hit Ctrl+C, the interrupt signal.

## What this is

A cron job and a static page. Not an agent: the binary does the same five
things in the same order every run, which is what makes it cheap to debug.

```
fetch → normalize → skip-if-seen → score → write JSON
                                             ↓
                       browser reads JSON, draws the page
```

| Step | Where | What happens |
|---|---|---|
| fetch | `internal/fetch` | arXiv Atom (`cs.CR`, `quant-ph`) and HN via Algolia |
| normalize | `internal/model` | everything becomes one `Item`; URLs canonicalized |
| skip-if-seen | `internal/store` | anything already in the archive costs nothing |
| score | `internal/score` | batches of 10 → Haiku → two 1–5 scores, dek, caveat |
| write | `internal/store` | `site/edition.json`, the only file the robot touches |

`data/scored.json` is the archive **and** the seen-set. An item is "seen"
exactly when it is already in there, which is why each story is paid for once,
ever. A 30-minute run scores the 5–20 items that are new, not all 300.

The quadrant is derived in Go from the two axes, never asked of the model:

|                   | act low       | act high          |
| ----------------- | ------------- | ----------------- |
| **novelty high**  | `NEW GROUND`  | `BREAK GLASS`     |
| **novelty low**   | `NOISE FLOOR` | `STANDING ORDERS` |

A ladder collapses two independent judgements into one number and throws away
the interesting half. A Fortinet advisory is novelty 2, actionability 5 —
important and not remotely new. No rung can say that; `STANDING ORDERS` can.

Retuning `AxisHigh` costs nothing — see `-requadrant`.

## Running it

```bash
export ANTHROPIC_API_KEY=sk-ant-...

go run ./cmd/edition -dry              # fetch and dedupe only, spend nothing
go run ./cmd/edition                   # the real thing
go run ./cmd/edition -requadrant       # redraw quadrants after editing AxisHigh

go run ./cmd/edition -label eval.csv   # write blank score columns
go run ./cmd/edition -compare eval.csv # see where the rubric disagrees with you

cd site && python3 -m http.server 8000   # then open localhost:8000
```

Stdlib only. No `go get`, no `npm`, no build step, no site generator.

## Layout

```
cmd/edition/main.go       the five steps, wired together
internal/config/          brand, sources, keywords, thresholds — start here
internal/model/           the Item struct, URL canonicalization
internal/fetch/           arxiv.go, hn.go
internal/score/           the rubric. this is the product.
internal/label/           CSV round-trip for -label and -compare
internal/store/           archive, dedupe, edition building
site/                     index.html, style.css, app.js — hand written
data/scored.json          the archive (gitignored until first run)
.github/workflows/        the 30-minute cron
```

## Renaming

Everything brand-facing is in `internal/config/config.go`: `Name`, `Mark`,
`Standfirst`, `Colophon`. Change those four and the `<title>` in
`site/index.html`, and SIGINT becomes whatever you want.

## Before you deploy — fill these four placeholders

| Placeholder | File | What to put |
|---|---|---|
| `firstRun` | `cmd/edition/main.go` | your launch date, `time.Date(YYYY, M, D, ...)` |
| `MYCODE` | `docs/index.html` | your GoatCounter code (goatcounter.com signup, free) |
| `REPLACE` | `docs/index.html` | your Kit form endpoint (Kit form → Embed → raw HTML) |
| repo URL | (jaysrinivasan.dev) | your actual `github.com/<you>/sigint` path |

`userAgent` in `internal/fetch/arxiv.go` already points at jaysrinivasan.dev — leave it, or swap to sigint.wtf once that resolves.

## Deploy runbook

Do these one at a time. Debugging DNS and Pages simultaneously is misery.

1. **Local green first.** `go vet ./...`, then `go run ./cmd/edition`. Confirm it writes `docs/edition.json` and the page renders at `cd docs && python3 -m http.server 8000`.
2. **Public repo** named `sigint`. Confirm no key leaked: `git grep sk-ant` returns nothing.
3. **Push.** The archive (`data/scored.json`) commits too — that is deliberate, it is the seen-set.
4. **Settings → Pages** → deploy from `main`, folder `/docs`. Verify it loads at `https://<you>.github.io/sigint/` before touching the domain.
5. **Settings → Secrets → Actions** → add `ANTHROPIC_API_KEY`. Then Actions tab → run the `edition` workflow manually once. Watch it go green.
6. **Buy sigint.wtf** (Porkbun; confirm flat renewal). Add an ALIAS record on the apex → `<you>.github.io`, and a `www` CNAME → same.
7. **Settings → Pages → Custom domain** → `sigint.wtf`. Wait for the check, then tick **Enforce HTTPS**.
8. **Link both ways** — add SIGINT to jaysrinivasan.dev (projects block + footer), and the colophon already names you.

## Cost control

- `MaxScorePerRun = 40` in config is the fuse: a dedup bug can never bill you for more than ~40 items in one run. The Console spend cap ($20/mo while testing) is the backstop.
- Steady state on three sources is ~$4/month. First run is the only spike.
- To cut further: change the cron in `.github/workflows/edition.yml` from `*/30` to `0 */2 * * *` (every 2 hours). Quarters the cost; a newspaper does not need 30-minute freshness.

## The scoring loop (optional but worth an hour)

- **Sat** — `-label eval.csv`, score 30-50 items yourself by gut in one sitting,
  `go run ./cmd/edition`, then `-compare eval.csv`. Aim for 6-7 of 10 top-ten
  overlap. Edit the anchors in `internal/score/score.go` for whichever level is
  wrong, re-run against the same file. Two or three rounds.
- KEV items are correct by construction, so the rubric only has to carry arXiv
  and HN. You can ship without labelling and the page still holds up.

## Etiquette

This audience will check. RSS and public APIs only, never scraping.
arXiv gets one request per 3 seconds and a contactable User-Agent
(set in `internal/fetch/arxiv.go` — put your real address there).
Headlines, links and our own one-line summary; never anyone's full text.

## Costs

Hosting on GitHub Pages, £0. Scoring runs to single-digit dollars a month at
ten items per call — check current Haiku pricing rather than trusting this line.
The domain is the only fixed cost.
