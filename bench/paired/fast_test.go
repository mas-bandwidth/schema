package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFastCommandsKeepCompleteCorpusAndWarmup(t *testing.T) {
	config := fastConfig{Rounds: 1, PacketIterations: 2000000, TableIterations: 200000, Timeout: time.Minute, Noise: "ordinary diagnostic"}
	if err := config.validate(); err != nil {
		t.Fatal(err)
	}
	for _, lang := range languages {
		for _, wire := range []string{"packet", "table"} {
			n := config.PacketIterations
			if wire == "table" {
				n = config.TableIterations
			}
			got := fastCommand(wire, lang, 0, n)
			if !strings.Contains(strings.Join(got.args, " "), "--round 0 --iterations ") {
				t.Fatal(got.args)
			}
			if contains(got.args, "--quick") != (wire == "packet") || contains(got.args, "--indexed") != (wire == "table") {
				t.Fatal(got.args)
			}
			if n%64 != 0 {
				t.Fatal("partial corpus rotation")
			}
		}
	}
	for index := range languages {
		first, second := fastWires(0, index), fastWires(1, index)
		if first[0] != second[1] || first[1] != second[0] {
			t.Fatal("wire order did not alternate")
		}
	}
}

func TestFastCountPlanningRetainsAdequateSamples(t *testing.T) {
	// Ordinary CSV accounting values: the fast path retains the same identity
	// and checks parser while allowing an explicitly requested whole rotation.
	data := fixtureRow("c", "packet", "write", 4000000) + fixtureRow("c", "packet", "round_trip", 2000000)
	data = strings.ReplaceAll(data, ",4000000,320,", ",2000000,320,")
	rows, err := parseRowsForIterations([]byte(data), "c", "packet", "packet", 2000000, 0)
	if err != nil {
		t.Fatal(err)
	}
	n, adequate, err := fastNextIterations(rows, 2000000)
	if err != nil || !adequate || n != 2000000 {
		t.Fatalf("adequate sample changed: %d %v %v", n, adequate, err)
	}
	rows[0].rate = 16000000 // 125ms at the same valid count; increase to250ms
	n, adequate, err = fastNextIterations(rows, 2000000)
	if err != nil || adequate || n != 4000000 || n%64 != 0 {
		t.Fatalf("count plan: %d %v %v", n, adequate, err)
	}
}

func TestFastSummaryUsesOnlyCompleteAdequatePairs(t *testing.T) {
	e := fastEvidence{Config: fastConfig{Rounds: 1, Noise: "background recorded"}, Qualification: "Non-certified diagnostic"}
	for _, lang := range languages {
		for _, wire := range []string{"packet", "table"} {
			cost := float64(1)
			if wire == "table" {
				cost = 2
			}
			e.Attempts = append(e.Attempts, fastAttempt{Language: lang, Wire: wire, Adequate: false, Metrics: []fastMetric{{Path: "write", Rate: 1e9}}})
			e.Attempts = append(e.Attempts, fastAttempt{Language: lang, Wire: wire, Adequate: true, Metrics: []fastMetric{{Path: "write", Rate: 1e6 / cost}, {Path: "round_trip", Rate: 1e6 / cost}}})
		}
	}
	dir := t.TempDir()
	if err := writeFastSummary(dir, e); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "| C++ | 100.0% | 200.0% |") || !strings.Contains(string(b), "not certified") {
		t.Fatal(string(b))
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}
	if !reflect.DeepEqual(names, []string{"README.md"}) {
		t.Fatal("fast summary created confirmation evidence", names)
	}
}
