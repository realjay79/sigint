// Package score turns unscored items into scored ones.
//
// The whole quality of this publication lives in the rubric below. Everything
// else in this repository is plumbing that anyone could copy. Spend your time
// here.
package score

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"sigint/internal/config"
	"sigint/internal/model"
)

// ---------------------------------------------------------------------------
// The rubric
// ---------------------------------------------------------------------------
//
// v0 — good enough to run this weekend, not good enough to keep.
//
// Before you trust this: label 50 items by hand, run them through, and compare.
// Tune the anchors until the model's ordering roughly matches yours. Without
// anchors a model marks everything a 4 and your ranking is noise; the anchors
// are where twenty years of judgement gets encoded.
//
// Note what is NOT asked for: the quadrant. Two small independent judgements
// are more reliable than one four-level ladder, and deriving the quadrant in Go
// means you can retune AxisHigh without spending a cent on re-scoring.

const rubric = `You are the ranking desk for a security publication. Score each item on two
independent scales. Be strict. Most items are 2s and 3s; that is correct.

THE READER
One reader, and every judgement is about them:
an application security engineer at a mid-size financial services firm.
Twenty years in. Owns the SAST/DAST/SCA tooling and the secure-SDLC process.
Reviews architecture, writes rules, argues with developers about findings.
Does NOT run a SOC, hunt threats, do IR, or manage a red team.
Cares about AI security because agents are entering the SDLC, and about
post-quantum because migration planning is now a real line item.
If a piece would not change how THIS person spends a Tuesday, actionability is low.

NOVELTY 1-5 — is the finding new?
  1  Rehash or explainer. "OWASP Top 10 explained." A news outlet's writeup of
     a CVE already covered elsewhere. Reporting something first is not novelty;
     novelty is about the finding, not the coverage.
  2  Known technique on a new corpus, no new method. Survey and systematization
     papers cap here unless the taxonomy genuinely changes how you would model
     a system.
  3  A real result that lands where you would expect. "Larger models resist
     jailbreaks somewhat better." Solid, unsurprising.
  4  A measurement nobody had run, or a technique not previously described.
     "What fraction of shipped SAST rules ever fire on real code."
  5  First public disclosure, or a result that contradicts standing practice.
     Rare. Several days can pass without one.

ACTIONABILITY 1-5 — does the reader do anything differently?
  1  Nothing to do. Funding rounds, conference recaps, market surveys,
     personnel moves, opinion pieces about regulation.
  2  Vocabulary only. A taxonomy of agent failure modes: useful language,
     no decision attached.
  3  Informs a decision this quarter. A comparison of two SBOM formats when
     tooling is up for renewal.
  4  Changes a concrete choice now. Evidence that a default tool configuration
     misses most of what a tuned run finds: go check your own config.
  5  Do something this week. Active exploitation of software the reader likely
     runs, or a technique directly applicable to work in progress.

SCORING DISCIPLINE
- Score the evidence, not the abstract's confidence. Papers oversell.
- Commercial intent caps actionability at 2, however good the engineering.
  If the piece exists to sell a product, that is marketing with a methodology
  section attached.
- arXiv and Hacker News are scored on ONE scale. A well-measured blog post
  outranks a weak preprint. Do not reward the venue.
- Preprints with no code, no released data and no baseline cap at novelty 3.
- One benchmark, one model family, or one deployment is not a general result.
  Cap novelty at 4 and say so in unproven.
- A CVE with no exploitation evidence and no public PoC is not actionability 5,
  however alarming the CVSS score.
- If an item is outside the reader's remit entirely, score it honestly low
  rather than stretching to justify it.

ALSO WRITE, FOR EACH ITEM

  dek       One sentence, under 30 words. Lead with the finding, and with the
            number that carries it if there is one. Plain and specific.
            BANNED: game-changer, groundbreaking, must-read, revolutionary,
            unlocks, paradigm shift, crucial, essential, staggering,
            game changing, breakthrough, "in today's landscape", "as we all
            know", and any sentence beginning "Imagine".
            Do not write "Why read:" or address the reader as "you".
            Write it as a headline desk would: declarative, no sell.

  unproven  One short clause naming what this does NOT establish. The missing
            control, the untested condition, the population it does not cover,
            the step from lab to production. This is the house style and it is
            not optional on research items. If the piece genuinely overclaims
            nothing, use an empty string, but reach for that rarely.

Return ONLY a JSON array. No prose, no markdown fences, no preamble.
Each element exactly:

  {"id": "<the id given>", "novelty": <1-5>, "actionability": <1-5>,
   "dek": "<one sentence>", "unproven": "<short clause or empty string>"}

ITEMS TO SCORE:
`

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type verdict struct {
	ID            string `json:"id"`
	Novelty       int    `json:"novelty"`
	Actionability int    `json:"actionability"`
	Dek           string `json:"dek"`
	Unproven      string `json:"unproven"`
}

// promptItem is the trimmed shape the model sees. Abstracts get truncated:
// the first 1200 characters carry the finding, and the rest is related work.
type promptItem struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Title  string `json:"title"`
	Text   string `json:"text"`
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

// Batch scores items in groups of config.BatchSize and returns those that came
// back with a usable verdict. A failed batch is logged and skipped; the items
// stay unscored and will be retried on the next run.
func Batch(client *http.Client, items []model.Item) []model.Item {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Println("  ANTHROPIC_API_KEY not set — skipping scoring")
		return nil
	}

	var scored []model.Item

	for start := 0; start < len(items); start += config.BatchSize {
		end := start + config.BatchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]

		verdicts, err := scoreOne(client, apiKey, batch)
		if err != nil {
			fmt.Printf("  batch %d-%d failed: %v\n", start, end, err)
			continue
		}

		byID := make(map[string]verdict, len(verdicts))
		for _, v := range verdicts {
			byID[v.ID] = v
		}

		for _, item := range batch {
			v, ok := byID[item.ID]
			if !ok || v.Novelty < 1 || v.Actionability < 1 {
				continue
			}
			item.Novelty = clamp(v.Novelty)
			item.Actionability = clamp(v.Actionability)
			item.Score = item.Novelty + item.Actionability
			item.Quadrant = config.Quadrant(item.Novelty, item.Actionability)
			item.Dek = strings.TrimSpace(v.Dek)
			item.Unproven = strings.TrimSpace(v.Unproven)
			item.ScoredAt = time.Now().UTC()
			scored = append(scored, item)
		}

		fmt.Printf("  scored %d-%d of %d\n", start, end, len(items))
	}

	return scored
}

func scoreOne(client *http.Client, apiKey string, batch []model.Item) ([]verdict, error) {
	payload := make([]promptItem, 0, len(batch))
	for _, item := range batch {
		payload = append(payload, promptItem{
			ID:     item.ID,
			Source: item.Source,
			Title:  item.Title,
			Text:   truncate(item.Text, 1200),
		})
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(apiRequest{
		Model:     config.Model,
		MaxTokens: config.MaxTokens,
		Messages: []apiMessage{
			{Role: "user", Content: rubric + string(encoded)},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("api error: %s", parsed.Error.Message)
	}

	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}

	cleaned := stripFences(text.String())

	var verdicts []verdict
	if err := json.Unmarshal([]byte(cleaned), &verdicts); err != nil {
		return nil, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	return verdicts, nil
}

// stripFences removes markdown code fences the model sometimes adds despite
// being told not to, and trims anything outside the outermost JSON array.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	open := strings.Index(s, "[")
	close := strings.LastIndex(s, "]")
	if open != -1 && close > open {
		return s[open : close+1]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func clamp(n int) int {
	if n < 1 {
		return 1
	}
	if n > 5 {
		return 5
	}
	return n
}
