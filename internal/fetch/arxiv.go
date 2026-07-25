package fetch

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sigint/internal/config"
	"sigint/internal/model"
)

// userAgent identifies us to arXiv. They ask for something descriptive and
// contactable, and they are within their rights to block anything that is not.
const userAgent = "SIGINT/0.1 (+https://jaysrinivasan.dev; hello@jaysrinivasan.dev)"

// arXiv asks for no more than one request every three seconds.
const arxivPolitenessDelay = 3 * time.Second

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string `xml:"id"`
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	Published string `xml:"published"`
	Authors   []struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

// Arxiv fetches the most recent submissions for each configured category.
// Categories are fetched sequentially with a delay, deliberately.
func Arxiv(client *http.Client) ([]model.Item, error) {
	var out []model.Item

	for n, cat := range config.ArxivCategories {
		if n > 0 {
			time.Sleep(arxivPolitenessDelay)
		}

		items, err := arxivCategory(client, cat)
		if err != nil {
			// One bad category should not sink the run.
			fmt.Printf("  arxiv %-9s error: %v\n", cat, err)
			continue
		}
		fmt.Printf("  arxiv %-9s %d entries\n", cat, len(items))
		out = append(out, items...)
	}

	return out, nil
}

func arxivCategory(client *http.Client, category string) ([]model.Item, error) {
	q := url.Values{}
	q.Set("search_query", "cat:"+category)
	q.Set("sortBy", "submittedDate")
	q.Set("sortOrder", "descending")
	q.Set("max_results", fmt.Sprint(config.ArxivMaxResults))

	endpoint := "http://export.arxiv.org/api/query?" + q.Encode()

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
		return nil, fmt.Errorf("arxiv returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse atom: %w", err)
	}

	items := make([]model.Item, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		// e.ID looks like http://arxiv.org/abs/2607.18063v1
		absURL := strings.TrimSpace(e.ID)
		shortID := absURL
		if idx := strings.Index(absURL, "/abs/"); idx != -1 {
			shortID = absURL[idx+len("/abs/"):]
		}
		if shortID == "" {
			continue
		}

		published, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Published))
		if err != nil {
			published = time.Now().UTC()
		}

		names := make([]string, 0, len(e.Authors))
		for _, a := range e.Authors {
			names = append(names, strings.TrimSpace(a.Name))
		}

		items = append(items, model.Item{
			ID:        "arxiv:" + shortID,
			Source:    "arxiv",
			Title:     squashWhitespace(e.Title),
			URL:       model.CanonicalURL(absURL),
			Text:      squashWhitespace(e.Summary),
			Authors:   joinAuthors(names),
			CVEs:      model.ExtractCVEs(e.Title + " " + e.Summary),
			Published: published.UTC(),
		})
	}

	return items, nil
}

// squashWhitespace collapses the newlines arXiv wraps titles and abstracts in.
func squashWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func joinAuthors(names []string) string {
	switch {
	case len(names) == 0:
		return ""
	case len(names) == 1:
		return names[0]
	default:
		return names[0] + " et al."
	}
}
