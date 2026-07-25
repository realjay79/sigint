package fetch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"sigint/internal/config"
	"sigint/internal/model"
)

// KEV entries are the only items in this pipeline that arrive pre-scored.
// "Confirmed exploited in the wild" is a fact, not a judgement, so there is
// nothing for the model to weigh and no reason to pay it. They go straight
// into the archive already carrying novelty, actionability and a dek.

type kevCatalog struct {
	CatalogVersion  string     `json:"catalogVersion"`
	DateReleased    string     `json:"dateReleased"`
	Count           int        `json:"count"`
	Vulnerabilities []kevEntry `json:"vulnerabilities"`
}

type kevEntry struct {
	CveID                      string `json:"cveID"`
	VendorProject              string `json:"vendorProject"`
	Product                    string `json:"product"`
	VulnerabilityName          string `json:"vulnerabilityName"`
	DateAdded                  string `json:"dateAdded"` // YYYY-MM-DD
	ShortDescription           string `json:"shortDescription"`
	RequiredAction             string `json:"requiredAction"`
	DueDate                    string `json:"dueDate"`
	KnownRansomwareCampaignUse string `json:"knownRansomwareCampaignUse"`
}

// KEV fetches CISA's catalog and returns only entries added recently enough to
// count as news. Without the lookback bound the first run would ingest the
// entire historical catalog and bury everything else.
func KEV(client *http.Client) ([]model.Item, error) {
	catalog, err := fetchCatalog(client, config.KEVFeed)
	if err != nil {
		// cisa.gov has blocked CI runner IPs before. The GitHub mirror is
		// maintained by CISA and stays in sync within minutes.
		fmt.Printf("  kev       primary failed (%v), trying mirror\n", err)
		catalog, err = fetchCatalog(client, config.KEVMirror)
		if err != nil {
			return nil, err
		}
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -config.KEVLookbackDays)

	items := make([]model.Item, 0, 16)
	for _, entry := range catalog.Vulnerabilities {
		added, err := time.Parse("2006-01-02", strings.TrimSpace(entry.DateAdded))
		if err != nil || added.Before(cutoff) {
			continue
		}

		ransomware := strings.EqualFold(entry.KnownRansomwareCampaignUse, "known")

		novelty := config.KEVNovelty
		if ransomware {
			novelty = config.KEVNoveltyRansomware
		}

		item := model.Item{
			ID:     "kev:" + entry.CveID,
			Source: "kev",
			Title:  titleFor(entry),
			// NVD is the stable destination. The notes field carries vendor
			// advisories but is inconsistent across entries.
			URL:           "https://nvd.nist.gov/vuln/detail/" + entry.CveID,
			Published:     added.UTC(),
			CVEs:          []string{strings.ToUpper(entry.CveID)},
			Novelty:       novelty,
			Actionability: config.KEVAction,
			Score:         novelty + config.KEVAction,
			Quadrant:      config.Quadrant(novelty, config.KEVAction),
			Dek:           dekFor(entry, ransomware),
			// No caveat line. There is nothing unproven about a confirmed
			// exploitation record, and inventing one would cheapen the field.
			Unproven: "",
			ScoredAt: time.Now().UTC(),
		}

		items = append(items, item)
	}

	fmt.Printf("  kev       %d entries added in the last %d days (of %d total)\n",
		len(items), config.KEVLookbackDays, len(catalog.Vulnerabilities))

	return items, nil
}

func fetchCatalog(client *http.Client, endpoint string) (*kevCatalog, error) {
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
		return nil, fmt.Errorf("returned %s", resp.Status)
	}

	var catalog kevCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode kev json: %w", err)
	}
	if len(catalog.Vulnerabilities) == 0 {
		return nil, fmt.Errorf("catalog parsed but contained no entries")
	}
	return &catalog, nil
}

func titleFor(e kevEntry) string {
	name := squashWhitespace(e.VulnerabilityName)
	if name == "" {
		name = fmt.Sprintf("%s %s vulnerability", e.VendorProject, e.Product)
	}
	return fmt.Sprintf("%s — %s", e.CveID, name)
}

// dekFor writes the standfirst deterministically. No model call, so this text
// costs nothing and never drifts into hype.
func dekFor(e kevEntry, ransomware bool) string {
	var b strings.Builder

	product := strings.TrimSpace(e.VendorProject + " " + e.Product)
	if product != "" {
		b.WriteString(product)
		b.WriteString(". ")
	}

	b.WriteString("Confirmed exploited in the wild")
	if ransomware {
		b.WriteString(", with known ransomware campaign use")
	}
	b.WriteString(".")

	if due, err := time.Parse("2006-01-02", strings.TrimSpace(e.DueDate)); err == nil {
		b.WriteString(" Federal remediation due ")
		b.WriteString(due.Format("2 January"))
		b.WriteString(".")
	}

	return b.String()
}
