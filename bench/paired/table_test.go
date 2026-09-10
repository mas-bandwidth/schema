package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryAnswersFromTheTree(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bench", "tables", "cpp"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bench", "tables", "rust"), 0755); err != nil {
		t.Fatal(err)
	}
	// A FILE named for a language is not a runner directory.
	if err := os.WriteFile(filepath.Join(root, "bench", "tables", "java"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	present, absent := discoverTableLanguages()
	if strings.Join(present, ",") != "cpp,rust" {
		t.Fatal(present)
	}
	if strings.Join(absent, ",") != "c,go,cs,js,java,dart,elixir" {
		t.Fatal(absent)
	}
	if len(present)+len(absent) != len(nineLanguages) {
		t.Fatal("a language went missing between the two halves")
	}
}

func TestPassShapeIsPairedThenEachTableOnlyLanguageAlone(t *testing.T) {
	passes := ninePasses("out", []string{"cpp", "c", "go", "rust", "cs", "js"})
	if len(passes) != 3 {
		t.Fatal(len(passes))
	}
	if passes[0].name != "paired" || strings.Join(passes[0].langs, ",") != strings.Join(languages, ",") {
		t.Fatal(passes[0])
	}
	for _, p := range passes[1:] {
		if len(p.langs) != 1 || contains(languages, p.langs[0]) {
			t.Fatal("a table-only pass must carry exactly one unpaired language:", p.langs)
		}
		if p.dir != filepath.Join("out", p.name) {
			t.Fatal(p.dir)
		}
	}
}

func TestPassesMayNotMixCountsCorporaOrBuilds(t *testing.T) {
	base := fastEvidence{
		Build:       buildInfo{Revision: "abc", Host: "studio", Arch: "arm64", CorpusIDs: map[string]string{"packet": "p", "table": "t"}},
		Config:      fastConfig{Rounds: 3},
		FinalCounts: map[string]int64{"packet": 2000000, "table": 200000},
	}
	// A table-only pass has no packet count at all; that is not a mismatch.
	only := base
	only.FinalCounts = map[string]int64{"table": 200000}
	if err := agree(base, only); err != nil {
		t.Fatal(err)
	}
	for name, spoil := range map[string]func(*fastEvidence){
		"revision":     func(e *fastEvidence) { e.Build.Revision = "def" },
		"host":         func(e *fastEvidence) { e.Build.Host = "space" },
		"corpus":       func(e *fastEvidence) { e.Build.CorpusIDs = map[string]string{"packet": "p", "table": "other"} },
		"table count":  func(e *fastEvidence) { e.FinalCounts = map[string]int64{"packet": 2000000, "table": 400000} },
		"round count":  func(e *fastEvidence) { e.Config.Rounds = 1 },
		"architecture": func(e *fastEvidence) { e.Build.Arch = "amd64" },
	} {
		next := base
		next.Build.CorpusIDs = map[string]string{"packet": "p", "table": "t"}
		next.FinalCounts = map[string]int64{"packet": 2000000, "table": 200000}
		spoil(&next)
		if err := agree(base, next); err == nil {
			t.Fatalf("a changed %s must refuse a single table", name)
		}
	}
}

func TestLoadOneReadsBothPlatformSpellings(t *testing.T) {
	for sample, want := range map[string]float64{
		"{ 1.20 1.34 1.41 }":        1.20,
		"1.20 1.34 1.41 2/512 9182": 1.20,
		"{ 0.00 0.01 0.05 }":        0.00,
	} {
		got, ok := load1(sample)
		if !ok || got != want {
			t.Fatalf("%q: %v %v", sample, got, ok)
		}
	}
	if _, ok := load1("unavailable"); ok {
		t.Fatal("a load string with no number must not report one")
	}
}

func sampleTable() nineTable {
	return nineTable{
		Host: "studio", OS: "darwin", Arch: "arm64", CPU: "Apple M2 Ultra",
		Revision: "abc123", Rounds: 3, Passes: 2,
		Counts:    map[string]int64{"packet": 2000000, "table": 200000},
		CorpusIDs: map[string]string{"packet": "p0", "table": "t0"},
		Load:      "1.00–2.00", Note: "diagnostic",
		Rows: []tableRow{
			{Language: "cpp", Form: tableForms["bench_fixed"], SaveNs: 100, TripNs: 200, Bytes: 64, PacketTripNs: 400},
			{Language: "go", Form: tableForms["bench_table"], SaveNs: 300, TripNs: 600, Bytes: 70, PacketTripNs: 600},
			{Language: "rust", Form: tableForms["bench_fixed"], SaveNs: 150, TripNs: 300, Bytes: 64},
		},
		Absent: []string{"java", "dart"},
	}
}

func TestTableRendersBothRatiosAndRefusesToInventTheMissingOne(t *testing.T) {
	got := sampleTable().markdown()
	for _, want := range []string{
		"| C++ | form 3 (fixed) | 100.0 | 200.0 | 64.0 | 50% | 100% |",
		"| Go | form 2 (tolerant) | 300.0 | 600.0 | 70.0 | 100% | 300% |",
		// Rust has no packet leg: the own-packet column says so and no other
		// language's packet stands in for it.
		"| Rust | form 3 (fixed) | 150.0 | 300.0 | 64.0 | — | 150% |",
		"No runner on this tree, so not measured and not estimated: Java, Dart.",
		"`studio` (darwin/arm64, Apple M2 Ultra) · `abc123` · 3 rounds × 2 pass(es) · load1 1.00–2.00",
		"packet `p0`, table `t0`",
		"2,000,000 packet, 200,000 table",
		"Not certified",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "not quiet") {
		t.Fatal("a quiet sitting must not carry the noise warning")
	}
}

func TestTableWithoutACppFixedRowRefusesToRebaseTheColumn(t *testing.T) {
	table := sampleTable()
	table.Rows[0].Form = tableForms["bench_table"]
	if _, ok := table.reference(); ok {
		t.Fatal("a tolerant C++ row is not the fixed-form reference")
	}
	got := table.markdown()
	if !strings.Contains(got, "unavailable rather than re-based") {
		t.Fatal(got)
	}
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "| C") || strings.HasPrefix(line, "| Rust") {
			if !strings.HasSuffix(line, "| — |") {
				t.Fatal("no row may read against a reference that is not there:", line)
			}
		}
	}
}

func TestALoadedHostSaysSoInTheTable(t *testing.T) {
	table := sampleTable()
	table.Noise = []string{"competing process cc (PID 1, CPU 99.0%) during during/round-0-cpp-table"}
	if !strings.Contains(table.markdown(), "This host was not quiet: 1 noise warning(s)") {
		t.Fatal(table.markdown())
	}
}

func TestEveryNineLanguageHasAName(t *testing.T) {
	for _, lang := range nineLanguages {
		if displayName(lang) == lang {
			t.Fatal("no display name for", lang)
		}
	}
	for _, lang := range languages {
		if !contains(nineLanguages, lang) {
			t.Fatal("a paired language is missing from the published row order:", lang)
		}
	}
}
