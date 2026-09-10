package main

import (
	wirebinary "encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	checks := "removed"
	if lang == "go" {
		checks = "always"
	}
	if wire == "table" {
		checks = "contract"
	}
	return fmt.Sprintf("%s,%s,%s,%d,%s,1,%d,%d,%d,0,0,%s,%s,hdr,%s,O3,unknown\n", lang, bench, path, iters, n, rate, rate, rate, id, family, checks)
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
	writeTestWindow(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "load.json"), []byte("[]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sealPassWithVerifier(dir, func(buildInfo) error { return nil }); err != nil {
		t.Fatal(err)
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
	if !strings.Contains(string(b), "Checks differ: packet C/C++/C# removes bounds/range checks; packet Go always checks; table keeps wire/API validation.") {
		t.Fatal("compact page omitted checks semantics")
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
	if err := os.Remove(filepath.Join(dir, completionFile)); err != nil {
		t.Fatal(err)
	}
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
	writeTestWindow(t, dir, 8)
	if err := sealPassWithVerifier(dir, func(buildInfo) error { return nil }); err != nil {
		t.Fatal(err)
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

func TestPairedRowsRefuseChangedChecks(t *testing.T) {
	for _, lang := range languages {
		for _, wire := range []string{"packet", "table"} {
			data := fixtureRow(lang, wire, "write", 1000000) + fixtureRow(lang, wire, "round_trip", 1000000)
			if _, err := parseRows([]byte(data), lang, wire, wire); err != nil {
				t.Fatalf("expected %s/%s row refused: %v", lang, wire, err)
			}
			for _, axis := range []string{"removed", "always", "contract", "unknown"} {
				if axis == expectedChecks(lang, wire) {
					continue
				}
				changed := strings.ReplaceAll(data, ","+expectedChecks(lang, wire)+",", ","+axis+",")
				if _, err := parseRows([]byte(changed), lang, wire, wire); err == nil {
					t.Fatalf("accepted %s/%s with uncaptioned %s checks", lang, wire, axis)
				}
			}
		}
	}
}

func TestReuseRequiresRecordedHEAD(t *testing.T) {
	if err := checkBuildRevision("aabb", "aabb"); err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{"ccdd", "", "unavailable"} {
		if err := checkBuildRevision("aabb", current); err == nil {
			t.Fatalf("reused stale HEAD against %q", current)
		}
	}
}

// A TABLE-ONLY LANGUAGE IS A TABLE LEG AND NOTHING ELSE. It parses as one
// wire, it is invoked as the driver's own build product, and it cannot reach
// the confirmation pass, whose every row is half of a ratio.
func TestTableOnlyLanguageIsTableWireOnly(t *testing.T) {
	for _, lang := range tableOnlyLanguages {
		if names[lang] == "" {
			t.Fatal("a language on the board needs a printed name:", lang)
		}
		if got := wiresFor(lang); len(got) != 1 || got[0] != "table" {
			t.Fatal(lang, got)
		}
		if contains(languages, lang) {
			t.Fatal("a table-only language must not be a paired language:", lang)
		}
		if !onlyTableLanguages([]string{lang}) || onlyTableLanguages(append([]string{lang}, languages...)) {
			t.Fatal("mixed request accepted:", lang)
		}
		if expectedChecks(lang, "table") != "contract" {
			t.Fatal("a table leg keeps wire/API validation:", lang)
		}
		args := runner("table", lang, "--gate").args
		if args[0] != binary("table", lang) {
			t.Fatal(args)
		}
		if !contains(args, "--indexed") || !contains(args, "bench/paired/corpus") {
			t.Fatal(args)
		}
	}
	for _, lang := range languages {
		if onlyTableLanguages([]string{lang}) {
			t.Fatal("a paired language read as table-only:", lang)
		}
	}
}

// The FIXED form's row name rides the same corpus id and the same family as
// the tolerant one, and a rust leg's seventeen columns are the driver's
// seventeen: the type board's rust rows carry a codec column and these do not.
func TestFixedFormRowFromATableOnlyLanguage(t *testing.T) {
	// THE COUNT IS THE RAISED ONE, because this leg is fast enough that the
	// standard 400,000 table iterations do not clear §2.1's 200 ms floor — the
	// driver's own escalation is what produces a row like this, and the parser
	// is asked about the count it actually requested.
	const raised = 6400000
	row := "rust,bench_fixed,%s," + strconv.Itoa(raised) + ",1264.0625,1,%d,%d,%d,0,0,table,table,crate,contract,O3,unknown\n"
	data := header + "\n" + fmt.Sprintf(row, "write", 21333333, 21333333, 21333333) + fmt.Sprintf(row, "round_trip", 6411220, 6411220, 6411220)
	rows, err := parseRowsForIterations([]byte(data), "rust", "table", "table", raised, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].cols[1] != "bench_fixed" || rows[1].cols[2] != "round_trip" {
		t.Fatal(rows)
	}
	// The same row with the type board's appended codec column is eighteen
	// wide and must be refused rather than truncated.
	if _, err := parseRowsForIterations([]byte(strings.ReplaceAll(data, ",unknown\n", ",unknown,flat\n")), "rust", "table", "table", raised, 0.2); err == nil {
		t.Fatal("an eighteen-column row was accepted")
	}
}
