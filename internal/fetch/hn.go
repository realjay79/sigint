package fetch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sigint/internal/config"
	"sigint/internal/model"
)

type hnResponse struct {
	Hits []hnHit `json:"hits"`
}

type hnHit struct {
	ObjectID    string  `json:"objectID"`
	Title       string  `json:"title"`
	URL         *string `json:"url"` // null on Ask HN and text posts
	Author      string  `json:"author"`
	Points      int     `json:"points"`
	NumComments int     `json:"num_comments"`
	CreatedAtI  int64   `json:"created_at_i"`
	StoryText   string  `json:"story_text"`
}

// HN pulls recent front-page-grade stories and keeps only those whose titles
// match our keyword list. The keyword filter is deliberately crude — it exists
// to hold the scoring bill down, not to make editorial decisions.
func HN(client *http.Client) ([]model.Item, error) {
	since := time.Now().Add(-time.Duration(config.HNLookbackHours) * time.Hour).Unix()

	q := url.Values{}
	q.Set("tags", "story")
	q.Set("numericFilters", fmt.Sprintf("points>%d,created_at_i>%d", config.HNMinPoints, since))
	q.Set("hitsPerPage", "200")

	endpoint := "https://hn.algolia.com/api/v1/search_by_date?" + q.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hn algolia returned %s", resp.Status)
	}

	var payload hnResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode hn json: %w", err)
	}

	items := make([]model.Item, 0, 32)
	for _, h := range payload.Hits {
		if h.Title == "" || !matchesKeyword(h.Title) {
			continue
		}

		link := "https://news.ycombinator.com/item?id=" + h.ObjectID
		if h.URL != nil && *h.URL != "" {
			link = *h.URL
		}

		// v1 scores HN on title, domain and points alone. Fetching and
		// stripping the linked page means paywalls, JS-rendered sites and
		// timeouts — that is a week-two problem.
		context := fmt.Sprintf(
			"Hacker News submission. Domain: %s. Points: %d. Comments: %d.",
			hostOf(link), h.Points, h.NumComments,
		)
		if h.StoryText != "" {
			context += " Text: " + squashWhitespace(h.StoryText)
		}

		items = append(items, model.Item{
			ID:        "hn:" + h.ObjectID,
			Source:    "hn",
			Title:     squashWhitespace(h.Title),
			URL:       model.CanonicalURL(link),
			Text:      context,
			Authors:   h.Author,
			CVEs:      model.ExtractCVEs(h.Title + " " + h.StoryText),
			Points:    h.Points,
			Published: time.Unix(h.CreatedAtI, 0).UTC(),
		})
	}

	fmt.Printf("  hn        %d entries (%d hits before keyword filter)\n",
		len(items), len(payload.Hits))

	return items, nil
}

func matchesKeyword(title string) bool {
	lower := strings.ToLower(title)
	for _, kw := range config.HNKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}
