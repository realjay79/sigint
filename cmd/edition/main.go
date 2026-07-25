// Command edition runs one pass of the pipeline:
//
//	fetch -> normalize -> skip-if-seen -> score -> write JSON
//
// It is a cron job, not an agent. It does the same five things in the same
// order every run, which makes it cheap to reason about when something breaks.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"sigint/internal/config"
	"sigint/internal/fetch"
	"sigint/internal/label"
	"sigint/internal/model"
	"sigint/internal/score"
	"sigint/internal/store"
)

// firstRun is the publication's birthday, used for the edition number.
// Set this to the day you actually launch.
var firstRun = time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

func main() {
	dry := flag.Bool("dry", false,
		"fetch and dedupe only; print what would be scored, spend nothing")
	requadrant := flag.Bool("requadrant", false,
		"recompute quadrants from existing scores after changing AxisHigh; no API calls")
	labelPath := flag.String("label", "",
		"write fresh items to this CSV with blank columns for your own scores")
	comparePath := flag.String("compare", "",
		"read your filled-in CSV and report where the rubric disagrees with you")
	flag.Parse()

	client := &http.Client{Timeout: 45 * time.Second}

	archive, err := store.LoadArchive(config.ArchivePath)
	if err != nil {
		die("load archive: %v", err)
	}
	fmt.Printf("archive: %d items\n", len(archive))

	// -compare reads the archive only. No fetching, no spending.
	if *comparePath != "" {
		if err := label.Compare(*comparePath, archive); err != nil {
			die("%v", err)
		}
		return
	}

	// -requadrant: AxisHigh changed, scores did not. Free.
	if *requadrant {
		for id, item := range archive {
			item.Quadrant = config.Quadrant(item.Novelty, item.Actionability)
			archive[id] = item
		}
		finish(archive)
		return
	}

	// 1 + 2. Fetch and normalize.
	fmt.Println("fetching:")
	var incoming []model.Item

	if items, err := fetch.Arxiv(client); err != nil {
		fmt.Printf("  arxiv failed entirely: %v\n", err)
	} else {
		incoming = append(incoming, items...)
	}

	if items, err := fetch.HN(client); err != nil {
		fmt.Printf("  hn failed entirely: %v\n", err)
	} else {
		incoming = append(incoming, items...)
	}

	if items, err := fetch.KEV(client); err != nil {
		fmt.Printf("  kev failed entirely: %v\n", err)
	} else {
		// Link each KEV addition to our own earlier coverage, if any.
		incoming = append(incoming, store.LinkPriorCoverage(archive, items)...)
	}
	fmt.Printf("fetched %d items\n", len(incoming))

	// 3. Skip anything already paid for.
	fresh := store.Unseen(archive, incoming)
	fmt.Printf("new since last run: %d\n", len(fresh))

	// -label writes the eval set and stops. Nothing is scored.
	if *labelPath != "" {
		if err := label.Write(*labelPath, fresh); err != nil {
			die("write labels: %v", err)
		}
		return
	}

	if *dry {
		fmt.Println("\n-dry: would score these, and nothing else:")
		for _, item := range fresh {
			fmt.Printf("  [%-5s] %s\n", item.Source, item.Title)
		}
		return
	}

	// 4. Score what is new — minus anything that arrived pre-scored.
	// KEV entries carry their own scores and bypass the model entirely,
	// which means the highest-signal items on the page are free.
	var needScoring []model.Item
	preScored := 0
	for _, item := range fresh {
		if item.Scored() {
			archive[item.ID] = item
			preScored++
		} else {
			needScoring = append(needScoring, item)
		}
	}
	if preScored > 0 {
		fmt.Printf("pre-scored (no API cost): %d\n", preScored)
	}

	// Cost fuse: never score more than the per-run cap, whatever slipped
	// through. This is what stops a dedup bug from becoming a surprise bill.
	if len(needScoring) > config.MaxScorePerRun {
		fmt.Printf("capping scoring at %d (had %d fresh — check dedup if this is normal)\n",
			config.MaxScorePerRun, len(needScoring))
		needScoring = needScoring[:config.MaxScorePerRun]
	}

	if len(needScoring) > 0 {
		fmt.Println("scoring:")
		for _, item := range score.Batch(client, needScoring) {
			archive[item.ID] = item
		}
	}

	// 5. Write.
	finish(archive)
}

func finish(archive store.Archive) {
	if err := store.SaveArchive(config.ArchivePath, archive); err != nil {
		die("save archive: %v", err)
	}

	edition := store.BuildEdition(archive, store.EditionNumber(firstRun))
	if err := store.SaveEdition(config.EditionPath, edition); err != nil {
		die("save edition: %v", err)
	}

	fmt.Printf("\n%s No. %d — %d stories on the front page\n",
		config.Name, edition.Edition, len(edition.Items))
	for _, quadrant := range config.Quadrants {
		fmt.Printf("  %-16s %d\n", quadrant, edition.Counts.ByQuadrant[quadrant])
	}
	if n := len(edition.Noise); n > 0 {
		fmt.Printf("  filed under noise %d\n", n)
	}
	fmt.Printf("wrote %s\n", config.EditionPath)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
