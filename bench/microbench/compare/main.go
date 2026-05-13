// compare runs the Go and Python Presidio benchmarks and prints a side-by-side
// comparison table with speedup ratios and MB/s throughput where available.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type pyResult struct {
	Name      string   `json:"name"`
	MeanNS    float64  `json:"mean_ns"`
	MinNS     float64  `json:"min_ns"`
	MaxNS     float64  `json:"max_ns"`
	OpsPerSec float64  `json:"ops_per_sec"`
	MBPerSec  *float64 `json:"mb_per_sec,omitempty"`
	ByteSize  *int64   `json:"byte_size,omitempty"`
}

// Go benchmark output: "BenchmarkFoo-14   123   456789 ns/op   7.35 MB/s"
var goBenchRE = regexp.MustCompile(`^(Benchmark[\w/]+)-\d+\s+\d+\s+([\d.]+)\s+ns/op(?:\s+([\d.]+)\s+MB/s)?`)

func main() {
	python := flag.String("python", "/tmp/presidio-venv312/bin/python3.12", "Python with Presidio")
	iterations := flag.Int("iterations", 20, "Python small-bench iterations")
	bigIterations := flag.Int("big-iterations", 5, "Python large-bench iterations")
	benchTime := flag.String("benchtime", "3s", "Go benchmark duration per test")
	flag.Parse()

	root := os.Getenv("ANONDE_ROOT")
	if root == "" {
		exe, _ := os.Executable()
		root = filepath.Join(filepath.Dir(exe), "..", "..", "..")
	}
	benchDir := filepath.Join(root, "bench", "microbench")

	sep := strings.Repeat("=", 78)
	fmt.Println(sep)
	fmt.Println(" Presidio: Go vs Python  —  big-data benchmark")
	fmt.Println(sep)

	// ── Go ────────────────────────────────────────────────────────────────────
	fmt.Println("\nRunning Go benchmarks...")
	goCmd := exec.Command("go", "test", "./bench/microbench/",
		"-bench=.", "-benchtime="+*benchTime, "-benchmem", "-count=1")
	goCmd.Dir = root
	goOut, err := goCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Go benchmark failed:\n%s\n", goOut)
		os.Exit(1)
	}
	goNS, goMB := parseGoOutput(string(goOut))

	// ── Python ────────────────────────────────────────────────────────────────
	fmt.Println("Running Python benchmarks...")
	pyCmd := exec.Command(*python, filepath.Join(benchDir, "bench_python.py"),
		"--iterations", strconv.Itoa(*iterations),
		"--big-iterations", strconv.Itoa(*bigIterations),
		"--json",
	)
	pyCmd.Dir = root
	pyOut, err := pyCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Python benchmark failed:\n%s\n", pyOut)
		os.Exit(1)
	}
	var pyResults []pyResult
	if err := json.Unmarshal(pyOut, &pyResults); err != nil {
		fmt.Fprintf(os.Stderr, "parse Python output: %v\n%s\n", err, pyOut)
		os.Exit(1)
	}

	// ── Table ─────────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println(sep)
	printSection("Small / latency benchmarks", pyResults[:6], goNS, goMB)
	fmt.Println()
	printSection("Large / throughput benchmarks", pyResults[6:], goNS, goMB)
	fmt.Println()
	fmt.Println("Speedup = Python mean / Go mean  (higher = Go is faster).")
	fmt.Println("MB/s    = bytes processed per second (higher = better throughput).")
	fmt.Println()
	fmt.Println("Note: Python Presidio uses spaCy NER (higher recall, higher latency).")
	fmt.Println("      Go anonde uses pure regex/rule-based recognizers.")
}

func printSection(title string, rows []pyResult, goNS, goMB map[string]float64) {
	fmt.Printf("── %s ──\n", title)
	fmt.Printf("%-42s %12s %10s %12s %10s %10s\n",
		"Benchmark", "Go ns/op", "Go MB/s", "Py ns/op", "Py MB/s", "Speedup")
	fmt.Println(strings.Repeat("─", 100))
	for _, py := range rows {
		gns := goNS[py.Name]
		gmb := goMB[py.Name]
		goNSOp := fmtNS(gns)
		goMBOp := fmtMB(gmb)
		pyNSOp := fmtNS(py.MeanNS)
		pyMBOp := "-"
		if py.MBPerSec != nil {
			pyMBOp = fmt.Sprintf("%.2f", *py.MBPerSec)
		}
		speedup := "-"
		if gns > 0 {
			r := py.MeanNS / gns
			speedup = fmt.Sprintf("%.1fx", r)
			if r >= 10 {
				speedup += " 🚀"
			}
		}
		fmt.Printf("%-42s %12s %10s %12s %10s %10s\n",
			py.Name, goNSOp, goMBOp, pyNSOp, pyMBOp, speedup)
	}
}

func parseGoOutput(out string) (nsMap, mbMap map[string]float64) {
	nsMap = make(map[string]float64)
	mbMap = make(map[string]float64)
	for _, line := range strings.Split(out, "\n") {
		m := goBenchRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		name := m[1]
		ns, _ := strconv.ParseFloat(m[2], 64)
		nsMap[name] = ns
		if m[3] != "" {
			mb, _ := strconv.ParseFloat(m[3], 64)
			mbMap[name] = mb
		}
	}
	return
}

func fmtNS(ns float64) string {
	if ns == 0 {
		return "N/A"
	}
	switch {
	case ns >= 1e9:
		return fmt.Sprintf("%.3fs", ns/1e9)
	case ns >= 1e6:
		return fmt.Sprintf("%.2fms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.2fµs", ns/1e3)
	default:
		return fmt.Sprintf("%.0fns", ns)
	}
}

func fmtMB(mb float64) string {
	if mb == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", mb)
}
