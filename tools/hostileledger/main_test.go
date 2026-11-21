package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpus plants a two-carrier corpus folder: two text cases and one wire.
func corpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	var manifest strings.Builder
	manifest.WriteString("# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n")
	for _, row := range [][2]string{{"t-one", "text"}, {"t-two", "text"}, {"w-one", "wire"}} {
		manifest.WriteString(row[0] + "\t" + row[1] + "\ts\tRoot\tinputs/" + row[0] + ".bin\tp\trefuse\tdamaged\t-\n")
		if err := os.WriteFile(filepath.Join(dir, "inputs", row[0]+".bin"), []byte{1}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.tsv"), []byte(manifest.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const whole = "PASS t-one\nhostile-ledger ran text 2\nhostile-ledger ran wire 1\nhostile-ledger end\n3 cases: 3 pass\n"

// TestAWholeRunIsGreen is the control every red below stands beside.
func TestAWholeRunIsGreen(t *testing.T) {
	out, err := check("zz", corpus(t), write(t, whole), 1, "", "")
	if err != nil {
		t.Fatalf("CONTROL FAILED: a whole run is red: %v", err)
	}
	for _, s := range []string{
		"zz hostile-corpus ledger: ran 3 of 3 registered, 0 held by name (text 2/2, wire 1/1), 1 run(s)",
		"zz hostile-corpus ledger negative control: one text case short of the registry is red",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("the tool does not say %q:\n%s", s, out)
		}
	}
}

// TestAShortOrEmptyRunIsRed: the runner's own summary says "3 cases" in every
// one of these logs, which is what it printed before the ledger existed.
func TestAShortOrEmptyRunIsRed(t *testing.T) {
	for _, tc := range []struct {
		name, log string
		runs      int
		says      string
	}{
		{"one case short", "hostile-ledger ran text 1\nhostile-ledger ran wire 1\nhostile-ledger end\n3 cases\n", 1, "carrier text ran 1 case(s) and owes 2"},
		{"a carrier never run", "hostile-ledger ran text 2\nhostile-ledger end\n3 cases\n", 1, "carrier wire ran 0 case(s) and owes 1"},
		{"nothing run", "hostile-ledger end\n3 cases\n", 1, "no case ran"},
		{"no ledger at all", "3 cases: 3 pass\n", 1, "printed 0 ledger block(s) and owes 1"},
		{"stopped before the end line", "hostile-ledger ran text 2\nhostile-ledger ran wire 1\n", 1, "has no `hostile-ledger end`"},
		{"one profile of two", whole, 2, "printed 1 ledger block(s) and owes 2"},
		{"more than the registry", "hostile-ledger ran text 3\nhostile-ledger ran wire 1\nhostile-ledger end\n", 1, "carrier text ran 3 case(s) and owes 2"},
		{"a carrier the corpus has not", whole + "hostile-ledger ran packet 1\nhostile-ledger end\n", 2, "which the corpus does not register"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := check("zz", corpus(t), write(t, tc.log), tc.runs, "", "")
			if err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Errorf("want red saying %q, got %v", tc.says, err)
			}
		})
	}
}

// TestHeldNamesAreCountedAndGoStale: a case or a carrier a port does not run
// is held by name with a reason, counted, and red once it runs or is gone.
func TestHeldNamesAreCountedAndGoStale(t *testing.T) {
	skipOne := write(t, "# held\nt-two\tno reader for it yet\n")
	noWire := write(t, "wire\tthe port reads no wire\n")

	out, err := check("zz", corpus(t), write(t, "hostile-ledger ran text 1\nhostile-ledger end\n"), 1, skipOne, noWire)
	if err != nil {
		t.Fatalf("a run short by exactly its held names is red: %v", err)
	}
	if !strings.Contains(out, "ran 1 of 3 registered, 2 held by name (text 1/2, wire 0/1)") {
		t.Errorf("the held names are not counted:\n%s", out)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, skipOne, ""); err == nil || !strings.Contains(err.Error(), "carrier text ran 2 case(s) and owes 1") {
		t.Errorf("a held case that ran is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, "", noWire); err == nil || !strings.Contains(err.Error(), "carrier wire ran 1 case(s) and owes 0") {
		t.Errorf("a held carrier that ran is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, write(t, "gone\twhy\n"), ""); err == nil || !strings.Contains(err.Error(), "the held case gone is not in the corpus") {
		t.Errorf("a held case the corpus dropped is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, "", write(t, "packet\twhy\n")); err == nil || !strings.Contains(err.Error(), "the held carrier packet has no case in the corpus") {
		t.Errorf("a held carrier the corpus dropped is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, write(t, "t-two\n"), ""); err == nil {
		t.Errorf("a held line with no reason was read")
	}
}

// TestTheRegistryIsTheFolder: the count is read from the corpus folder, so a
// manifest that lost a row whose input is still there, or every row, is red
// before any runner's count is believed.
func TestTheRegistryIsTheFolder(t *testing.T) {
	dir := corpus(t)
	path := filepath.Join(dir, "manifest.tsv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if err := os.WriteFile(path, []byte(strings.Join(lines[:3], "")), 0o644); err != nil { // drops w-one
		t.Fatal(err)
	}
	if _, err := check("zz", dir, write(t, "hostile-ledger ran text 2\nhostile-ledger end\n"), 1, "", ""); err == nil || !strings.Contains(err.Error(), "no manifest row names (w-one.bin)") {
		t.Errorf("a manifest that lost a row agrees with a runner that lost it too: %v", err)
	}
	if err := os.WriteFile(path, []byte(lines[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := check("zz", dir, write(t, "hostile-ledger end\n"), 1, "", ""); err == nil || !strings.Contains(err.Error(), "registers no case") {
		t.Errorf("a header-only manifest is not red: %v", err)
	}
}
