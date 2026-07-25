// Package model defines the one shape every source is normalized into.
// Source differences stop existing past the fetch layer.
package model

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Item is a single story, at every stage of its life. Fields tagged "-" are
// used for scoring but never shipped to the browser, which keeps edition.json
// small and avoids republishing other people's text.
type Item struct {
	ID        string    `json:"id"`     // "arxiv:2607.18063v1" or "hn:48982535"
	Source    string    `json:"source"` // "arxiv" | "hn"
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Text      string    `json:"-"` // abstract or context, for scoring only
	Published time.Time `json:"published"`

	// Populated by the fetch layer where the source provides it.
	Authors string `json:"authors,omitempty"`
	Points  int    `json:"points,omitempty"`

	// Populated by the scoring layer.
	Novelty       int    `json:"novelty"`
	Actionability int    `json:"actionability"`
	Score         int    `json:"score"`
	Quadrant      string `json:"quadrant"`
	Dek           string `json:"dek"`
	Unproven      string `json:"unproven,omitempty"`

	// CVEs found in the title or body at fetch time. Persisted deliberately:
	// it is what lets a KEV addition find the story we ran days earlier.
	CVEs []string `json:"cves,omitempty"`

	// Prior coverage, set on a KEV item that matches something already in the
	// archive. This is the memory no other aggregator has.
	PriorTitle string `json:"prior_title,omitempty"`
	PriorURL   string `json:"prior_url,omitempty"`
	PriorDays  int    `json:"prior_days,omitempty"`

	// Bookkeeping.
	ScoredAt time.Time `json:"scored_at"`
}

// cvePattern matches CVE-YYYY-NNNN with the usual 4-to-7 digit sequence.
var cvePattern = regexp.MustCompile(`(?i)CVE-\d{4}-\d{4,7}`)

// ExtractCVEs pulls every distinct CVE identifier out of a blob of text,
// uppercased so comparisons are stable.
func ExtractCVEs(text string) []string {
	found := cvePattern.FindAllString(text, -1)
	if len(found) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(found))
	var out []string
	for _, raw := range found {
		id := strings.ToUpper(raw)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// MentionsCVE reports whether this item references the given CVE.
func (i Item) MentionsCVE(cve string) bool {
	for _, c := range i.CVEs {
		if c == cve {
			return true
		}
	}
	return false
}

// Scored reports whether this item has been through the model already.
func (i Item) Scored() bool { return i.Novelty > 0 && i.Actionability > 0 }

// CanonicalURL strips the noise that makes the same article look like two
// different ones: scheme case, host case, tracking params, trailing slash.
// Run this before deduping, or you will pay to score the same story twice.
func CanonicalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Host = strings.TrimPrefix(u.Host, "www.")
	u.Fragment = ""

	q := u.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") ||
			lower == "ref" || lower == "source" ||
			lower == "fbclid" || lower == "gclid" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()

	if u.Path != "/" {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	return u.String()
}
