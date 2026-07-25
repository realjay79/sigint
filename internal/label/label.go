// Package label handles the hand-labelling loop.
//
// You cannot tune a rubric against nothing. If you run the model first and
// read its output, you are not evaluating it — you are anchoring on the answer
// you were meant to be checking. Ground truth has to exist before you see what
// the model said.
//
//	go run ./cmd/edition -label eval.csv    write blank score columns
//	  ... fill it in yourself, 20s per item, one sitting ...
//	go run ./cmd/edition                    score for real
//	go run ./cmd/edition -compare eval.csv  see where you disagree
package label

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"sigint/internal/model"
	"sigint/internal/store"
)

// Write emits a CSV with two blank columns for your own scores.
func Write(path string, items []model.Item) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{
		"id", "source", "my_novelty", "my_actionability", "title",
	}); err != nil {
		return err
	}

	for _, item := range items {
		if err := w.Write([]string{
			item.ID, item.Source, "", "", item.Title,
		}); err != nil {
			return err
		}
	}

	fmt.Printf("wrote %d rows to %s\n", len(items), path)
	fmt.Println("Fill my_novelty and my_actionability (1-5). Gut call, 20 seconds")
	fmt.Println("each, one sitting. Split it over two days and your scale drifts.")
	return nil
}

type row struct {
	id            string
	title         string
	novelty       int
	actionability int
}

// Compare reads your labels, matches them against the scored archive, and
// reports the only two things that matter: whether the top ten agrees, and
// which disagreements share a pattern.
func Compare(path string, archive store.Archive) error {
	rows, err := read(path)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no filled-in rows found in %s", path)
	}

	type pair struct {
		row  row
		item model.Item
	}

	var pairs []pair
	var missing int
	for _, r := range rows {
		item, ok := archive[r.id]
		if !ok || !item.Scored() {
			missing++
			continue
		}
		pairs = append(pairs, pair{row: r, item: item})
	}

	if len(pairs) == 0 {
		return fmt.Errorf("none of your labelled items have been scored yet — " +
			"run `go run ./cmd/edition` first")
	}
	if missing > 0 {
		fmt.Printf("note: %d labelled items are not scored yet, skipping them\n\n", missing)
	}

	// --- the number that matters: does the top ten agree? ------------------

	mine := make([]pair, len(pairs))
	copy(mine, pairs)
	sort.Slice(mine, func(a, b int) bool {
		return mine[a].row.novelty+mine[a].row.actionability >
			mine[b].row.novelty+mine[b].row.actionability
	})

	theirs := make([]pair, len(pairs))
	copy(theirs, pairs)
	sort.Slice(theirs, func(a, b int) bool {
		return theirs[a].item.Score > theirs[b].item.Score
	})

	n := 10
	if len(pairs) < n {
		n = len(pairs)
	}

	inMine := make(map[string]bool, n)
	for _, p := range mine[:n] {
		inMine[p.row.id] = true
	}
	overlap := 0
	for _, p := range theirs[:n] {
		if inMine[p.item.ID] {
			overlap++
		}
	}

	fmt.Printf("TOP %d OVERLAP: %d of %d\n", n, overlap, n)
	switch {
	case overlap >= 7:
		fmt.Println("Good enough. Ship it and stop tuning.")
	case overlap >= 5:
		fmt.Println("Close. One more pass on the anchors.")
	default:
		fmt.Println("The rubric is not encoding your judgement yet. Read the")
		fmt.Println("disagreements below and look for the pattern, not the outliers.")
	}

	// --- systematic bias ---------------------------------------------------

	var novDrift, actDrift float64
	for _, p := range pairs {
		novDrift += float64(p.item.Novelty - p.row.novelty)
		actDrift += float64(p.item.Actionability - p.row.actionability)
	}
	novDrift /= float64(len(pairs))
	actDrift /= float64(len(pairs))

	fmt.Printf("\nMEAN DRIFT (model minus you)\n")
	fmt.Printf("  novelty        %+.2f%s\n", novDrift, hint(novDrift))
	fmt.Printf("  actionability  %+.2f%s\n", actDrift, hint(actDrift))

	// --- the biggest disagreements ------------------------------------------

	sort.Slice(pairs, func(a, b int) bool {
		return gap(pairs[a].row, pairs[a].item) > gap(pairs[b].row, pairs[b].item)
	})

	fmt.Printf("\nBIGGEST DISAGREEMENTS\n")
	shown := 0
	for _, p := range pairs {
		if gap(p.row, p.item) < 2 || shown >= 8 {
			break
		}
		fmt.Printf("  you %d/%d  model %d/%d  [%s] %s\n",
			p.row.novelty, p.row.actionability,
			p.item.Novelty, p.item.Actionability,
			p.item.Source, truncate(p.item.Title, 62))
		shown++
	}
	if shown == 0 {
		fmt.Println("  none — nothing differs by 2 or more on either axis.")
	}

	return nil
}

func hint(drift float64) string {
	switch {
	case drift > 0.6:
		return "  <- model inflates; tighten the high anchors"
	case drift < -0.6:
		return "  <- model is harsher than you; loosen the low anchors"
	default:
		return ""
	}
}

func gap(r row, item model.Item) int {
	return abs(item.Novelty-r.novelty) + abs(item.Actionability-r.actionability)
}

func read(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}

	var out []row
	for n, rec := range records {
		if n == 0 || len(rec) < 5 {
			continue // header, or a malformed line
		}
		nov, err1 := strconv.Atoi(strings.TrimSpace(rec[2]))
		act, err2 := strconv.Atoi(strings.TrimSpace(rec[3]))
		if err1 != nil || err2 != nil {
			continue // not filled in; skip quietly
		}
		out = append(out, row{
			id: rec[0], title: rec[4], novelty: nov, actionability: act,
		})
	}
	return out, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
