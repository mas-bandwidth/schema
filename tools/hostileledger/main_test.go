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

const whole = "PASS t-one\nhostile-ledger ran t-one\nhostile-ledger ran t-two\nhostile-ledger ran w-one\nhostile-ledger end\n3 cases: 3 pass\n"

func ran(names ...string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString("hostile-ledger ran " + n + "\n")
	}
	return b.String() + "hostile-ledger end\n3 cases: 3 pass\n"
}

// TestAWholeRunIsGreen is the control every red below stands beside.
func TestAWholeRunIsGreen(t *testing.T) {
	out, err := check("zz", corpus(t), write(t, whole), 1, "", "", "")
	if err != nil {
		t.Fatalf("CONTROL FAILED: a whole run is red: %v", err)
	}
	for _, s := range []string{
		"zz hostile-corpus ledger: ran 3 of 3 registered, 0 held by name (text 2/2, wire 1/1), 1 run(s)",
		"zz hostile-corpus ledger negative controls: t-one dropped is red by name; t-one run under an unregistered name (the same count) is red by name",
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
		{"one case short", ran("t-one", "w-one"), 1, "t-two (text) is registered and was not run"},
		{"a carrier never run", ran("t-one", "t-two"), 1, "w-one (wire) is registered and was not run"},
		{"nothing run", ran(), 1, "no case ran"},
		{"no ledger at all", "3 cases: 3 pass\n", 1, "printed 0 ledger block(s) and owes 1"},
		{"stopped before the end line", "hostile-ledger ran t-one\nhostile-ledger ran t-two\nhostile-ledger ran w-one\n", 1, "has no `hostile-ledger end`"},
		{"one profile of two", whole, 2, "printed 1 ledger block(s) and owes 2"},
		{"a case twice", ran("t-one", "t-one", "t-two", "w-one"), 1, "the row t-one ran 2 time(s) and owes 1"},
		// THE RIGHT COUNT AND THE WRONG CASES: three names ran, three are
		// registered, and one of the three is not the corpus's
		{"the same count under another name", ran("t-one", "t-three", "w-one"), 1, "t-three ran and the corpus does not register it"},
		{"the same count, one case twice for another", ran("t-one", "t-one", "w-one"), 1, "t-two (text) is registered and was not run"},
		{"a line that is not a ledger line", "hostile-ledger ran text 2\nhostile-ledger end\n", 1, "is not a ledger line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := check("zz", corpus(t), write(t, tc.log), tc.runs, "", "", "")
			if err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Errorf("want red saying %q, got %v", tc.says, err)
			}
		})
	}
}

// TestHeldNamesAreIdentities: a case or a carrier a port does not run is held
// by name with a reason. The reviewer's control is the second check here: a
// run that ran the HELD case and dropped an owed one on the same carrier has
// the count the list allows and is red on both names.
func TestHeldNamesAreIdentities(t *testing.T) {
	skipTwo := write(t, "# held\nt-two\tno reader for it yet\n")
	noWire := write(t, "wire\tthe port reads no wire\n")

	out, err := check("zz", corpus(t), write(t, ran("t-one")), 1, skipTwo, noWire, "")
	if err != nil {
		t.Fatalf("a run short by exactly its held names is red: %v", err)
	}
	if !strings.Contains(out, "ran 1 of 3 registered, 2 held by name (text 1/2, wire 0/1)") {
		t.Errorf("the held names are not counted:\n%s", out)
	}
	_, err = check("zz", corpus(t), write(t, ran("t-two", "w-one")), 1, skipTwo, "", "")
	if err == nil || !strings.Contains(err.Error(), "t-one (text) is registered and was not run") || !strings.Contains(err.Error(), "t-two (text) is held by name and ran") {
		t.Errorf("the held case run in place of an owed one (the same count) is not red on both names: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, skipTwo, "", ""); err == nil || !strings.Contains(err.Error(), "t-two (text) is held by name and ran") {
		t.Errorf("a held case that ran is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, "", noWire, ""); err == nil || !strings.Contains(err.Error(), "w-one (wire) is held by name and ran") {
		t.Errorf("a held carrier that ran is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, write(t, "gone\twhy\n"), "", ""); err == nil || !strings.Contains(err.Error(), "the held case gone is not in the corpus") {
		t.Errorf("a held case the corpus dropped is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, "", write(t, "packet\twhy\n"), ""); err == nil || !strings.Contains(err.Error(), "the held carrier packet has no case in the corpus") {
		t.Errorf("a held carrier the corpus dropped is not stale: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, write(t, "t-two\n"), "", ""); err == nil {
		t.Errorf("a held line with no reason was read")
	}
}

// TestRowsAreTrackedByTheirOwnId: a runner that reads one case as several
// rows (the JavaScript runner's packet tiers and modes) owes every one of
// them BY ID. Two of the reviewer's controls are here. F-062: an enumerator
// that drops a case's only job printed "1 cases, 0 rows: 0 pass" and exited 0;
// with no row run the case is red by name. F-071: four runs of the runtime
// tier, and none of the flat tier, are four occurrences of the case's name
// and passed a count of four; each row id is owed once.
func TestRowsAreTrackedByTheirOwnId(t *testing.T) {
	const spec = "wire=runtime-dev,flat-dev"
	if _, err := check("zz", corpus(t), write(t, ran("t-one", "t-two", "w-one@runtime-dev", "w-one@flat-dev")), 1, "", "", spec); err != nil {
		t.Fatalf("CONTROL FAILED: every row of every case: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, ran("t-one", "t-two", "w-one@runtime-dev")), 1, "", "", spec); err == nil || !strings.Contains(err.Error(), "the row w-one@flat-dev ran 0 time(s) and owes 1") {
		t.Errorf("a case one row short is not red: %v", err)
	}
	_, err := check("zz", corpus(t), write(t, ran("t-one", "t-two", "w-one@runtime-dev", "w-one@runtime-dev")), 1, "", "", spec)
	if err == nil || !strings.Contains(err.Error(), "the row w-one@flat-dev ran 0 time(s) and owes 1") || !strings.Contains(err.Error(), "the row w-one@runtime-dev ran 2 time(s) and owes 1") {
		t.Errorf("one tier run twice in place of the other is not red: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, ran("t-one", "t-two", "w-one", "w-one")), 1, "", "", spec); err == nil || !strings.Contains(err.Error(), "w-one ran and the corpus does not register it") {
		t.Errorf("the bare case name in place of its row ids is not red: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, ran("t-one", "t-two")+"1 cases, 0 rows: 0 pass\n"), 1, "", "", spec); err == nil || !strings.Contains(err.Error(), "w-one (wire) is registered and was not run") {
		t.Errorf("a case whose enumerator produced no row is not red: %v", err)
	}
	if _, err := check("zz", corpus(t), write(t, whole), 1, "", "", "packet=a,b"); err == nil || !strings.Contains(err.Error(), "rows are owed for the carrier packet") {
		t.Errorf("rows owed for a carrier the corpus has not: %v", err)
	}
	for _, bad := range []string{"wire", "wire=", "wire=a,a", "=a"} {
		if _, err := check("zz", corpus(t), write(t, whole), 1, "", "", bad); err == nil {
			t.Errorf("the malformed -rows %q was read", bad)
		}
	}
}

// TestACRLFLogIsReadAndControlled is F-073: the same records with CRLF line
// endings were parsed and counted, and then failed the tool's own negative
// control, which looked for a name followed by LF in the raw bytes.
func TestACRLFLogIsReadAndControlled(t *testing.T) {
	skipTwo := write(t, "t-two\tno reader for it yet\n")
	lf := ran("t-one", "w-one")
	for name, log := range map[string]string{"LF": lf, "CRLF": strings.ReplaceAll(lf, "\n", "\r\n")} {
		out, err := check("zz", corpus(t), write(t, log), 1, skipTwo, "", "")
		if err != nil || !strings.Contains(out, "ran 2 of 3 registered, 1 held by name") || !strings.Contains(out, "negative controls: t-one dropped is red by name") {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
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
	if _, err := check("zz", dir, write(t, ran("t-one", "t-two")), 1, "", "", ""); err == nil || !strings.Contains(err.Error(), "no manifest row names (w-one.bin)") {
		t.Errorf("a manifest that lost a row agrees with a runner that lost it too: %v", err)
	}
	if err := os.WriteFile(path, []byte(lines[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := check("zz", dir, write(t, ran()), 1, "", "", ""); err == nil || !strings.Contains(err.Error(), "registers no case") {
		t.Errorf("a header-only manifest is not red: %v", err)
	}
}
