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
| fetch | `internal/fetch` | arXiv Atom (`cs.CR`), HN via Algolia, CISA KEV JSON |
| normalize | `internal/model` | everything becomes one `Item`; URLs canonicalized |
| skip-if-seen | `internal/store` | anything already in the archive costs nothing |
| score | `internal/score` | batches of 10 → scoring model → two 1–5 scores, dek, caveat |
| write | `internal/store` | `docs/edition.json`, the only file the robot touches |

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

## Running & deploying

Setup, local run, and deploy notes live in [DEPLOY.md](DEPLOY.md).

## License

Personal project. Ask before reuse.
