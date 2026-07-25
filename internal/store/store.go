// Package store handles the two files this project persists.
//
// data/scored.json is the archive: every item ever scored, keyed by ID. It
// doubles as the seen-set, which is why there is no separate seen.json — an
// item is "seen" precisely when it is already in here. This is the mechanism
// that makes the whole thing cheap: each story is paid for exactly once.
//
// docs/edition.json is the rolling front page the browser reads.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"sigint/internal/config"
	"sigint/internal/model"
)

// Archive maps item ID to the scored item.
type Archive map[string]model.Item

// Edition is the payload the browser fetches.
type Edition struct {
	Generated  time.Time    `json:"generated"`
	Edition    int          `json:"edition"`
	Name       string       `json:"name"`
	Mark       string       `json:"mark"`
	Standfirst string       `json:"standfirst"`
	Colophon   string       `json:"colophon"`
	Counts     Counts       `json:"counts"`
	Items      []model.Item `json:"items"`

	// Noise is the "filed under noise" footer: stories with real traffic
	// that scored badly. Everyone else buries these; we print them.
	Noise []model.Item `json:"noise"`
}

// Counts feed the masthead line and the filter chips.
type Counts struct {
	Total      int            `json:"total"`
	BySource   map[string]int `json:"by_source"`
	ByQuadrant map[string]int `json:"by_quadrant"`
}

// LoadArchive reads the archive, returning an empty one on first run.
func LoadArchive(path string) (Archive, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Archive{}, nil
	}
	if err != nil {
		return nil, err
	}

	archive := Archive{}
	if len(raw) == 0 {
		return archive, nil
	}
	if err := json.Unmarshal(raw, &archive); err != nil {
		return nil, fmt.Errorf("parse archive: %w", err)
	}
	return archive, nil
}

// SaveArchive prunes anything past the retention window and writes atomically.
func SaveArchive(path string, archive Archive) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -config.ArchiveRetentionDays)
	for id, item := range archive {
		if item.Published.Before(cutoff) {
			delete(archive, id)
		}
	}

	raw, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, raw)
}

// Unseen returns the items not already present in the archive.
// Deduping is by ID and by canonical URL, so the same story arriving from
// both arXiv and HN is only paid for once.
func Unseen(archive Archive, items []model.Item) []model.Item {
	knownURLs := make(map[string]bool, len(archive))
	for _, item := range archive {
		knownURLs[item.URL] = true
	}

	seenThisRun := make(map[string]bool)
	var out []model.Item

	for _, item := range items {
		if _, ok := archive[item.ID]; ok {
			continue
		}
		if knownURLs[item.URL] || seenThisRun[item.URL] {
			continue
		}
		seenThisRun[item.URL] = true
		out = append(out, item)
	}
	return out
}

// LinkPriorCoverage is the payoff for keeping an archive.
//
// When a CVE lands on KEV, we already know whether we ran the story earlier —
// as a vendor advisory, an HN thread, a paper. If we did, this is not another
// feed item. It is a state change: advisory to actively exploited. Every other
// aggregator treats each item as independent because none of them remember
// what they published last week. We do.
func LinkPriorCoverage(archive Archive, items []model.Item) []model.Item {
	for n, item := range items {
		if len(item.CVEs) == 0 {
			continue
		}

		var best model.Item
		var found bool

		for _, prior := range archive {
			if prior.ID == item.ID || prior.Source == "kev" {
				continue
			}
			matches := false
			for _, cve := range item.CVEs {
				if prior.MentionsCVE(cve) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			// Earliest coverage is the interesting one: how long we were
			// ahead of the exploitation confirmation.
			if !found || prior.Published.Before(best.Published) {
				best = prior
				found = true
			}
		}

		if !found {
			continue
		}

		days := int(item.Published.Sub(best.Published).Hours() / 24)
		if days < 0 {
			days = 0
		}

		items[n].PriorTitle = best.Title
		items[n].PriorURL = best.URL
		items[n].PriorDays = days
	}

	return items
}

// BuildEdition selects the current front page from the archive: everything
// inside the window, best first, capped.
func BuildEdition(archive Archive, editionNo int) Edition {
	cutoff := time.Now().UTC().Add(-time.Duration(config.EditionWindowHours) * time.Hour)

	var items []model.Item
	for _, item := range archive {
		if item.Scored() && item.Published.After(cutoff) {
			items = append(items, item)
		}
	}

	// Best first; ties broken by recency so the page churns.
	sort.Slice(items, func(a, b int) bool {
		if items[a].Score != items[b].Score {
			return items[a].Score > items[b].Score
		}
		return items[a].Published.After(items[b].Published)
	})

	if len(items) > config.EditionMaxItems {
		items = items[:config.EditionMaxItems]
	}

	counts := Counts{
		Total:      len(items),
		BySource:   map[string]int{},
		ByQuadrant: map[string]int{},
	}
	for _, item := range items {
		counts.BySource[item.Source]++
		counts.ByQuadrant[item.Quadrant]++
	}

	return Edition{
		Generated:  time.Now().UTC(),
		Edition:    editionNo,
		Name:       config.Name,
		Mark:       config.Mark,
		Standfirst: config.Standfirst,
		Colophon:   config.Colophon,
		Counts:     counts,
		Items:      items,
		Noise:      pickNoise(archive, items),
	}
}

// pickNoise finds the stories the crowd liked and the rubric did not: real
// traffic, low score, kept off the front page. Sorted by traffic descending,
// so the most embarrassing omission leads.
func pickNoise(archive Archive, frontPage []model.Item) []model.Item {
	onFront := make(map[string]bool, len(frontPage))
	for _, item := range frontPage {
		onFront[item.ID] = true
	}

	cutoff := time.Now().UTC().Add(-time.Duration(config.EditionWindowHours) * time.Hour)

	var candidates []model.Item
	for _, item := range archive {
		if onFront[item.ID] || !item.Scored() || item.Published.Before(cutoff) {
			continue
		}
		if item.Points == 0 {
			continue // no traffic signal, so nothing to contradict
		}
		candidates = append(candidates, item)
	}

	sort.Slice(candidates, func(a, b int) bool {
		return candidates[a].Points > candidates[b].Points
	})

	if len(candidates) > config.NoiseCount {
		candidates = candidates[:config.NoiseCount]
	}
	return candidates
}

// SaveEdition writes the front page the browser polls.
func SaveEdition(path string, edition Edition) error {
	raw, err := json.MarshalIndent(edition, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, raw)
}

// EditionNumber counts days since first publication, so the masthead can carry
// a real edition number rather than a fabricated one.
func EditionNumber(firstRun time.Time) int {
	return int(time.Since(firstRun).Hours()/24) + 1
}

// writeAtomic avoids leaving a half-written file if the process dies mid-write,
// which matters because the browser may be polling edition.json at any moment.
func writeAtomic(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
