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
	e := fastEvidence{Config: fastConfig{Rounds: 1, Noise: "background recorded"}, Qualification: "Non-certified diagnostic", FinalCounts: map[string]int64{"packet": 2000000, "table": 263168}}
	for _, lang := range languages {
		for _, wire := range []string{"packet", "table"} {
			cost := float64(1)
			if wire == "table" {
				cost = 2
			}
			e.Attempts = append(e.Attempts, fastAttempt{Language: lang, Wire: wire, Iterations: e.FinalCounts[wire], Adequate: false, Metrics: []fastMetric{{Path: "write", Rate: 1e9}}})
			// An adequate attempt at a superseded count is evidence of the
			// group's escalation and must not supply a row.
			e.Attempts = append(e.Attempts, fastAttempt{Language: lang, Wire: wire, Iterations: 100, Adequate: true, Metrics: []fastMetric{{Path: "write", Rate: 1e9}, {Path: "round_trip", Rate: 1e9}}})
			e.Attempts = append(e.Attempts, fastAttempt{Language: lang, Wire: wire, Iterations: e.FinalCounts[wire], Adequate: true, Metrics: []fastMetric{{Path: "write", Rate: 1e6 / cost}, {Path: "round_trip", Rate: 1e6 / cost}}})
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
	// The one count per wire is stated once, and the one-round Range column
	// names itself degenerate rather than passing as measured stability.
	if !strings.Contains(string(b), "2,000,000 Packet and 263,168 Table operations") || !strings.Contains(string(b), "Range column is degenerate") {
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

// One slow leg must not give itself a private iteration count. §2.1 requires
// the count to be identical across every language of a cross-language row, so
// the whole group is re-measured and every leg of the finished table reports
// the same number.
func TestFastScheduleEscalatesEveryLegOfTheGroup(t *testing.T) {
	counts := map[string]int64{"packet": 2000000, "table": 200000}
	const adequateTable int64 = 263168
	measured := map[string][]int64{}
	final := map[string]int64{}
	err := fastSchedule(languages, 1, counts, func(pass, round int, lang, wire string, n int64) (int64, bool, error) {
		key := lang + "/" + wire
		measured[key] = append(measured[key], n)
		// C# on the table wire is the slow leg: below the floor until the
		// count reaches adequateTable. Every other leg is already adequate.
		if lang == "cs" && wire == "table" && n < adequateTable {
			return adequateTable, false, nil
		}
		final[key] = n
		return n, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts["table"] != adequateTable || counts["packet"] != 2000000 {
		t.Fatalf("final counts: %v", counts)
	}
	for _, lang := range languages {
		if got := final[lang+"/table"]; got != adequateTable {
			t.Fatalf("%s finished the table group at %d, not the group's %d", lang, got, adequateTable)
		}
		if got := final[lang+"/packet"]; got != 2000000 {
			t.Fatalf("%s finished the packet group at %d", lang, got)
		}
		// The adequate group is not disturbed by the other group's raise.
		if got := measured[lang+"/packet"]; len(got) != 1 {
			t.Fatalf("%s packet was measured %d times: %v", lang, len(got), got)
		}
		if got := measured[lang+"/table"]; len(got) != 2 || got[0] != 200000 || got[1] != adequateTable {
			t.Fatalf("%s table attempts: %v", lang, got)
		}
	}
}

func TestFastScheduleRefusesAPermanentlyShortLeg(t *testing.T) {
	counts := map[string]int64{"packet": 2000000, "table": 200000}
	passes := 0
	err := fastSchedule(languages, 1, counts, func(pass, round int, lang, wire string, n int64) (int64, bool, error) {
		if lang == "go" && wire == "packet" {
			passes = pass + 1
			return n + 64, false, nil
		}
		return n, true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "packet") || passes != fastPasses {
		t.Fatalf("%v after %d passes", err, passes)
	}
}
