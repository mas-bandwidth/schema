package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseLedger(t *testing.T) {
	held, notes, err := parseLedger(strings.NewReader("# why\n\nF4\towed: x\nR8\towed: y\tthe row is out of date: SPEC-TABLES.md:8576\n"), "l.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held["R8"] != "owed: y" || held["F4"] != "owed: x" {
		t.Fatalf("got %v", held)
	}
	if len(notes) != 1 || notes["R8"] != "the row is out of date: SPEC-TABLES.md:8576" {
		t.Fatalf("notes = %v", notes)
	}
	for _, bad := range []string{
		"F4\n",                // no reason
		"F4\t \n",             // empty reason
		"F4\ta\nF4\tb\n",      // duplicate
		"# nothing\n",         // empty ledger
		"rows/F4\towed\n",     // a path, not a row name
		"F 4\towed\n",         // space in the name
		"F4\towed: x\t\n",     // an empty note
		"F4\towed: x\tn\tm\n", // a fourth column
	} {
		if _, _, err := parseLedger(strings.NewReader(bad), "l.tsv"); err == nil {
			t.Errorf("ledger %q parsed", bad)
		}
	}
}

// An owed row's ledger line states the cause the run printed, exactly. The
// reversed witness: a line whose cause the row no longer prints is red, and
// the message names the row and both causes.
func TestOwedCauseMustMatchThePrintedCause(t *testing.T) {
	ran := []string{"A", "B", "C"}
	failed := map[string]bool{"A": true, "B": true}
	causes := map[string]string{"A": "fails: FAIL x", "B": "does not build: B.c:3:1: error: y"}
	ledger := map[string]string{"A": "owed: fails: FAIL x", "B": "owed: does not build: B.c:3:1: error: y"}
	if mis := causeMismatches(ran, failed, ledger, causes, "l.tsv"); len(mis) != 0 {
		t.Fatalf("the matching ledger is red: %v", mis)
	}
	// Reversed: B's failure moved on (a new first error), its line did not.
	stale := map[string]string{"A": "owed: fails: FAIL x", "B": "owed: does not build: B.c:1:1: error: #error locale_t needs POSIX.1-2008"}
	mis := causeMismatches(ran, failed, stale, causes, "l.tsv")
	if len(mis) != 1 || !strings.Contains(mis[0], "B: l.tsv says") ||
		!strings.Contains(mis[0], `"owed: does not build: B.c:1:1: error: #error locale_t needs POSIX.1-2008"`) ||
		!strings.Contains(mis[0], `"owed: does not build: B.c:3:1: error: y"`) {
		t.Fatalf("a stale cause was not named with both causes: %v", mis)
	}
	// A line without the `owed: ` prefix is a mismatch too.
	if mis := causeMismatches(ran, failed, map[string]string{"A": "fails: FAIL x", "B": ledger["B"]}, causes, "l.tsv"); len(mis) != 1 || !strings.HasPrefix(mis[0], "A: ") {
		t.Fatalf("an unprefixed line passed: %v", mis)
	}
	// Rows that passed or are not owed are the exact set's business, not this.
	if mis := causeMismatches(ran, map[string]bool{"A": true}, map[string]string{"A": "owed: fails: FAIL x", "C": "owed: anything"}, causes, "l.tsv"); len(mis) != 0 {
		t.Fatalf("a row that did not fail was compared: %v", mis)
	}
}

func TestVerdictIsAnExactSet(t *testing.T) {
	all := []string{"A", "B", "C"}
	failed := map[string]bool{"B": true}
	// An unledgered failure is red and named.
	if err := verdict(all, all, failed, nil, ""); err == nil || !strings.Contains(err.Error(), "B") {
		t.Fatalf("an unledgered failing row passed: %v", err)
	}
	// The exact ledger is green.
	if err := verdict(all, all, failed, map[string]string{"B": "owed"}, "l.tsv"); err != nil {
		t.Fatalf("the exact ledger is red: %v", err)
	}
	// A ledgered row that passes is red (stale), and the failing row is still red.
	err := verdict(all, all, failed, map[string]string{"A": "owed"}, "l.tsv")
	if err == nil || !strings.Contains(err.Error(), "(not in l.tsv): B") || !strings.Contains(err.Error(), "now pass; remove them: A") {
		t.Fatalf("a swapped ledger: %v", err)
	}
	// A ledger line naming no row file is red.
	err = verdict(all, all, failed, map[string]string{"B": "owed", "Z": "owed"}, "l.tsv")
	if err == nil || !strings.Contains(err.Error(), "no row file: Z") {
		t.Fatalf("a ledger line for a missing row passed: %v", err)
	}
	// A shard judges only the rows it ran: B's line is not stale in a shard
	// that did not run B.
	if err := verdict(all, []string{"A", "C"}, nil, map[string]string{"B": "owed"}, "l.tsv"); err != nil {
		t.Fatalf("a shard judged a row it did not run: %v", err)
	}
}

func TestShardsCoverEveryRowOnce(t *testing.T) {
	rows := []string{"W1", "C3", "F8", "R4", "E6", "P3", "C10"}
	seen := map[string]int{}
	for i := 0; i < 3; i++ {
		for _, r := range shardOf(rows, i, 3) {
			seen[r]++
		}
	}
	for _, r := range rows {
		if seen[r] != 1 {
			t.Errorf("row %s is in %d shards", r, seen[r])
		}
	}
	if len(seen) != len(rows) {
		t.Errorf("shards hold %d rows, want %d", len(seen), len(rows))
	}
}

// The rows a leg runs are its rows directory: a file added is a row run.
func TestListRowsIsTheDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"F4.c", "R8.c", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "W14"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := listRows(dir, ".c")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"F4", "R8"}) {
		t.Fatalf("c rows = %v", got)
	}
	got, err = listRows(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"W14"}) {
		t.Fatalf("cs rows = %v", got)
	}
}

func TestParseShard(t *testing.T) {
	if i, n, err := parseShard("1/2"); err != nil || i != 1 || n != 2 {
		t.Fatalf("1/2 = %d %d %v", i, n, err)
	}
	for _, bad := range []string{"2/2", "-1/2", "0/0", "x", "1"} {
		if _, _, err := parseShard(bad); err == nil {
			t.Errorf("shard %q parsed", bad)
		}
	}
}

func TestPickRows(t *testing.T) {
	all := []string{"E8", "F4", "R13", "R8"}
	got, err := pickRows(all, "R8, E8 R13")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"E8", "R13", "R8"}) {
		t.Fatalf("picked %v", got)
	}
	for list, want := range map[string]string{
		"E8 Z9":  "Z9 (no row file)",
		"E8 E8":  "E8 (named twice)",
		" , ":    "names no row",
		"":       "names no row",
		"F4,R99": "R99 (no row file)",
	} {
		if _, err := pickRows(all, list); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("-rows %q: got %v, want an error naming %q", list, err, want)
		}
	}
}

// A narrow run (-rows, the per-PR verify targets' run) is held to the same
// ledger and the same cause match as the whole leg's: an owed row it runs
// that fails with another cause is red, naming the row; a row it does not run
// is not judged. The toolchain is a shell script standing in for dart: no
// time, socket or network.
func TestNarrowRunKeepsTheLedgerAndTheCauseMatch(t *testing.T) {
	root := t.TempDir()
	rows := filepath.Join(root, "test", "conformance", "dart", "rows")
	if err := os.MkdirAll(rows, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"A", "B", "C"} {
		if err := os.WriteFile(filepath.Join(rows, r+".dart"), []byte("void main() { if (false) throw 'x'; }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := filepath.Join(root, "fake-dart")
	script := "case \"$3\" in *B.dart) echo 'B: the dead byte was not refused' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DART", "sh "+fake)
	ledger := filepath.Join(root, "test", "conformance", "dart", "rows-owed.tsv")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(ledger, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o := opts{root: root, leg: "dart", shard: "0/1", jobs: 2, work: filepath.Join(root, "work")}

	// B fails with a cause its ledger line does not state: red, by name.
	write("B\towed: B: some other failure\n")
	o.rows = "A,B"
	err := run(o)
	if err == nil || !strings.Contains(err.Error(), "cause their ledger line does not state") || !strings.Contains(err.Error(), "B: ") {
		t.Fatalf("a narrow run passed an owed row's changed cause: %v", err)
	}
	// The line set to what the run printed: green.
	write("B\towed: fails: B: the dead byte was not refused\n")
	if err := run(o); err != nil {
		t.Fatalf("the owed row with its printed cause: %v", err)
	}
	// B unledgered and run: red.
	if err := os.Remove(ledger); err != nil {
		t.Fatal(err)
	}
	if err := run(o); err == nil || !strings.Contains(err.Error(), "B") {
		t.Fatalf("a narrow run passed an unledgered failing row: %v", err)
	}
	// A run that names only A does not judge B.
	o.rows = "A"
	if err := run(o); err != nil {
		t.Fatalf("a narrow run judged a row it did not run: %v", err)
	}
	// -rows with a shard is refused: shard 1/2 of A and C would run C alone,
	// pass, and drop A without a word.
	o.rows, o.shard = "A,C", "1/2"
	if err := run(o); err == nil || !strings.Contains(err.Error(), "-rows with -shard 1/2") {
		t.Fatalf("-rows with -shard 1/2 ran: %v", err)
	}
	o.shard = "0/1"
	// A name with no row file is refused before anything runs.
	o.rows = "A,Z"
	if err := run(o); err == nil || !strings.Contains(err.Error(), "Z (no row file)") {
		t.Fatalf("-rows with a missing row ran: %v", err)
	}
	// -gen is refused on a leg whose rows name their tree themselves.
	o.rows, o.gen = "A", "work"
	if err := run(o); err == nil || !strings.Contains(err.Error(), "-gen") {
		t.Fatalf("-gen on dart ran: %v", err)
	}
}
