// diff_gliner compares per-doc finding counts between two GLiNER
// runner outputs and reports the average / median relative delta. Used
// to validate that the in-process yalue/onnxruntime_go inference path
// produces results within tolerance of the Python sidecar reference.
//
//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
)

type finding struct {
	Start int     `json:"start"`
	End   int     `json:"end"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

type doc struct {
	ID       string    `json:"id"`
	Engine   string    `json:"engine"`
	Findings []finding `json:"findings"`
}

func load(path string) []doc {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	var out []doc
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for s.Scan() {
		var d doc
		if err := json.Unmarshal(s.Bytes(), &d); err == nil {
			out = append(out, d)
		}
	}
	return out
}

func main() {
	candidate := flag.String("a", "/tmp/gliner_yalue.jsonl", "path to candidate findings jsonl")
	reference := flag.String("b", "bench/corpora/openmed/data/anonde_glinerpii.jsonl", "path to reference findings jsonl")
	gomlx := flag.String("g", "bench/corpora/openmed/data/anonde_gonative_gliner.jsonl", "path to old (broken) gomlx output for sanity check")
	patternsOnly := flag.String("p", "bench/corpora/openmed/data/anonde_patterns.jsonl", "path to patterns-only baseline (subtracted from candidate)")
	flag.Parse()

	a := load(*candidate)
	b := load(*reference)
	bByID := make(map[string]doc, len(b))
	for _, x := range b {
		bByID[x.ID] = x
	}

	// Build patterns-only signature set per doc, so we can subtract
	// pattern-derived findings from the candidate. What remains should
	// approximate GLiNER's contribution.
	patternsDocs := load(*patternsOnly)
	patternsByID := make(map[string]map[string]bool, len(patternsDocs))
	for _, x := range patternsDocs {
		sigs := make(map[string]bool, len(x.Findings))
		for _, f := range x.Findings {
			sigs[fmt.Sprintf("%d:%d:%s", f.Start, f.End, f.Type)] = true
		}
		patternsByID[x.ID] = sigs
	}
	subtractPatterns := func(d doc) int {
		sigs, ok := patternsByID[d.ID]
		if !ok {
			return len(d.Findings)
		}
		n := 0
		for _, f := range d.Findings {
			if !sigs[fmt.Sprintf("%d:%d:%s", f.Start, f.End, f.Type)] {
				n++
			}
		}
		return n
	}
	type row struct {
		id  string
		na  int
		nb  int
		rel float64
	}
	var rows []row
	for _, x := range a {
		bx, ok := bByID[x.ID]
		if !ok {
			continue
		}
		// Subtract pattern findings from candidate so we compare
		// the GLiNER-attributable count vs. the Python sidecar.
		na := subtractPatterns(x)
		nb := len(bx.Findings)
		var rel float64
		if nb == 0 {
			if na == 0 {
				rel = 0
			} else {
				rel = 1
			}
		} else {
			rel = math.Abs(float64(na-nb)) / float64(nb)
		}
		rows = append(rows, row{x.ID, na, nb, rel})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].rel > rows[j].rel })
	var sum float64
	for _, r := range rows {
		sum += r.rel
	}
	fmt.Printf("total docs: %d\n", len(rows))
	fmt.Printf("avg rel delta: %.3f\n", sum/float64(len(rows)))
	mid := rows[len(rows)/2]
	fmt.Printf("median rel delta: %.3f (id=%s a=%d b=%d)\n", mid.rel, mid.id, mid.na, mid.nb)
	fmt.Println()
	fmt.Println("top 10 worst by relative delta:")
	for i := 0; i < 10 && i < len(rows); i++ {
		r := rows[i]
		fmt.Printf("  %-35s a=%3d  b=%3d  rel=%.3f\n", r.id, r.na, r.nb, r.rel)
	}
	fmt.Println()
	w20, w30 := 0, 0
	for _, r := range rows {
		if r.rel <= 0.20 {
			w20++
		}
		if r.rel <= 0.30 {
			w30++
		}
	}
	fmt.Printf("within 20%%: %d/%d (%.1f%%)\n", w20, len(rows), 100*float64(w20)/float64(len(rows)))
	fmt.Printf("within 30%%: %d/%d (%.1f%%)\n", w30, len(rows), 100*float64(w30)/float64(len(rows)))

	if *gomlx != "" {
		gomlxDocs := load(*gomlx)
		gByID := make(map[string]doc, len(gomlxDocs))
		for _, x := range gomlxDocs {
			gByID[x.ID] = x
		}
		identical := 0
		differing := 0
		var totalA, totalG int
		for _, x := range a {
			gx, ok := gByID[x.ID]
			if !ok {
				continue
			}
			if len(x.Findings) == len(gx.Findings) {
				identical++
			} else {
				differing++
			}
			totalA += len(x.Findings)
			totalG += len(gx.Findings)
		}
		fmt.Printf("\ncandidate vs gomlx: %d identical-count, %d differing  (totalA=%d, totalG=%d)\n",
			identical, differing, totalA, totalG)
	}

	// Diagnostic: subtract patterns from each — what's the GLiNER-attributable count distribution?
	var sumA, sumG int
	for _, x := range a {
		sumA += subtractPatterns(x)
	}
	if *gomlx != "" {
		gomlxDocs := load(*gomlx)
		for _, x := range gomlxDocs {
			sumG += subtractPatterns(x)
		}
	}
	fmt.Printf("\nGLiNER-attributable (candidate - patterns) total: %d\n", sumA)
	fmt.Printf("GLiNER-attributable (gomlx - patterns) total:     %d\n", sumG)

	var sumPy int
	for _, x := range b {
		sumPy += len(x.Findings)
	}
	fmt.Printf("Python sidecar (anonde_glinerpii) total:          %d\n", sumPy)

	// Span-level intersection: how many candidate (after patterns
	// subtraction) findings exactly overlap a python finding of any
	// type at the same byte boundary?
	matched, total := 0, 0
	for _, x := range a {
		bx, ok := bByID[x.ID]
		if !ok {
			continue
		}
		pySigs := make(map[string]bool, len(bx.Findings))
		pyTypes := make(map[string]string, len(bx.Findings))
		for _, f := range bx.Findings {
			k := fmt.Sprintf("%d:%d", f.Start, f.End)
			pySigs[k] = true
			pyTypes[k] = f.Type
		}
		patSigs := patternsByID[x.ID]
		for _, f := range x.Findings {
			ps := fmt.Sprintf("%d:%d:%s", f.Start, f.End, f.Type)
			if patSigs[ps] {
				continue
			}
			total++
			if pySigs[fmt.Sprintf("%d:%d", f.Start, f.End)] {
				matched++
			}
		}
	}
	if total > 0 {
		fmt.Printf("Candidate-only GLiNER spans matching python boundary: %d/%d (%.1f%%)\n",
			matched, total, 100*float64(matched)/float64(total))
	}
}
