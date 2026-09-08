package main

import (
	wirebinary "encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPairedCorpusIndex(t *testing.T) {
	index, err := os.ReadFile("corpus/bench_table.lengths")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("corpus/bench_table.variants.bin")
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile("corpus/bench_table.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 64*4 {
		t.Fatal("index must contain exactly 64 uint32 lengths")
	}
	total := 0
	for k := range 64 {
		n := int(wirebinary.LittleEndian.Uint32(index[k*4:]))
		if n <= 0 || n > 65536 || n > len(data)-total {
			t.Fatalf("invalid record %d length", k)
		}
		if k == 0 && (len(first) != n || string(data[:n]) != string(first)) {
			t.Fatal("first record differs from golden")
		}
		total += n
	}
	if total != len(data) {
		t.Fatal("index does not cover corpus exactly")
	}
}

func fixtureRow(lang, wire, path string, rate int) string {
	bench, family, id, n, iters := "bench_mixed", "gen", "packet", "320", 4000000
	if wire == "table" {
		bench, family, id, n, iters = "bench_table", "table", "table", "2069.84375", 400000
	}
	return fmt.Sprintf("%s,%s,%s,%d,%s,1,%d,%d,%d,0,0,%s,%s,hdr,contract,O3,unknown\n", lang, bench, path, iters, n, rate, rate, rate, id, family)
}

func makePass(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	info := buildInfo{Rounds: 7, CorpusIDs: map[string]string{"packet": "packet", "table": "table"}}
	b, _ := json.Marshal(info)
	if e := os.WriteFile(filepath.Join(dir, "build.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	table := map[string]int{"cpp": 1000000, "c": 980000, "go": 500000, "cs": 400000}
	packet := map[string]int{"cpp": 4000000, "c": 4000000, "go": 1800000, "cs": 1600000}
	for _, wire := range []string{"packet", "table"} {
		rate := packet["cpp"]
		if wire == "table" {
			rate = table["cpp"]
		}
		data := header + "\n" + fixtureRow("cpp", wire, "write", rate) + fixtureRow("cpp", wire, "round_trip", rate)
		for _, label := range []string{"start", "end"} {
			if err := os.WriteFile(filepath.Join(dir, "control-"+label+"-"+wire+".csv"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for round := range 7 {
		for _, lang := range languages {
			for _, wire := range []string{"packet", "table"} {
				rate := packet[lang]
				if wire == "table" {
					rate = table[lang]
				}
				data := header + "\n" + fixtureRow(lang, wire, "write", rate) + fixtureRow(lang, wire, "round_trip", rate)
				if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%d-%s-%s.csv", round, lang, wire)), []byte(data), 0600); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	return dir
}

func TestPairedPercentages(t *testing.T) {
	dir := makePass(t)
	if e := render(dir); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "README.md"))
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range []string{"| C++ | 100% | 400% |", "| C | 102% | 408% |", "| Go | 200% | 360% |", "| C# | 250% | 400% |"} {
		if !strings.Contains(string(b), line) {
			t.Fatalf("missing %s in %s", line, b)
		}
	}
	if strings.Contains(string(b), "µs") {
		t.Fatal("raw details leaked onto compact page")
	}
}

func TestPairedRendererRefusesMissingOrChangedEvidence(t *testing.T) {
	for _, kind := range []string{"missing", "identity", "fast"} {
		t.Run(kind, func(t *testing.T) {
			dir := makePass(t)
			p := filepath.Join(dir, "round-2-c-table.csv")
			if kind == "missing" {
				if e := os.Remove(p); e != nil {
					t.Fatal(e)
				}
			} else {
				b, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				s := string(b)
				if kind == "identity" {
					s = strings.ReplaceAll(s, "2069.84375", "2070")
				} else {
					s = strings.ReplaceAll(s, "980000", "98000000")
				}
				if e = os.WriteFile(p, []byte(s), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e := render(dir); e == nil {
				t.Fatal("published incompatible or incomplete evidence")
			}
			if _, e := os.Stat(filepath.Join(dir, "README.md")); !os.IsNotExist(e) {
				t.Fatal("failure wrote headline")
			}
		})
	}
}

func TestEvenRoundMedian(t *testing.T) {
	dir := makePass(t)
	metadata, err := os.ReadFile(filepath.Join(dir, "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info buildInfo
	if err = json.Unmarshal(metadata, &info); err != nil {
		t.Fatal(err)
	}
	info.Rounds = 8
	metadata, _ = json.Marshal(info)
	if err = os.WriteFile(filepath.Join(dir, "build.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	for _, lang := range languages {
		for _, wire := range []string{"packet", "table"} {
			b, err := os.ReadFile(filepath.Join(dir, "round-0-"+lang+"-"+wire+".csv"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "round-7-"+lang+"-"+wire+".csv"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for round := range 4 {
		p := filepath.Join(dir, fmt.Sprintf("round-%d-cpp-table.csv", round))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b = []byte(strings.ReplaceAll(string(b), "1000000", "900000"))
		if err = os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := render(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "results.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "cpp,bench_table,round_trip,400000,2069.84375,8,950000,900000,1000000,") {
		t.Fatal("even-round median did not average both central values")
	}
}

// Explicit opt-in exercises real, already-built runners without starting a
// benchmark. No fixture mutation is needed: a missing golden must refuse.
func TestBuiltGateOnly(t *testing.T) {
	langs := os.Getenv("SCHEMA_PAIRED_TEST_BUILT")
	if langs == "" {
		t.Skip("set SCHEMA_PAIRED_TEST_BUILT=cpp,c,go,cs after building")
	}
	t.Chdir("../..")
	for lang := range strings.SplitSeq(langs, ",") {
		for _, wire := range []string{"packet", "table"} {
			out, e := capture(runner(wire, lang, "--gate"))
			if e != nil || len(out) != 0 {
				t.Fatalf("%s/%s gate: %v stdout=%q", lang, wire, e, out)
			}
			out, e = capture(runner(wire, lang, "--gate", "--wire-dir", t.TempDir()))
			if e == nil || len(out) != 0 {
				t.Fatalf("%s/%s missing golden was accepted or emitted rows", lang, wire)
			}
		}
	}
}
