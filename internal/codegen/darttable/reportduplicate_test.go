package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestWireReportCountsDuplicateKeysDart pins docs/SPEC-TABLES.md §2.8 (EQUAL)
// and §4's `duplicate` counter on the variable-class Dart wire reader: a map
// root whose wire holds one key twice — built with the Go reference leg — is
// reported by the Dart load with duplicate equal to Go's report.Duplicate,
// keeps the LAST value whole, and its entry count equals Go's. The reference
// rule is internal/tablewire/decode.go (~line 700-716): an entry whose key
// EQUALS the previous landed key is a duplicate, the earlier entry's slot is
// replaced, and the map's count excludes it.
func TestWireReportCountsDuplicateKeysDart(t *testing.T) {
	t.Parallel()

	const src = `package probe
table Root {
	entries map[int32]int32
}
`
	u := buildUnit(t, src)

	// Build the duplicate-key wire with the GO leg: a tabletext instance whose
	// map field holds two entries sharing one key, encoded by tablewire.Encode.
	// The entries are built through the text reader (which initialises them the
	// way the encoder expects) and then joined, so the wire carries the key
	// twice.
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	rootA := m.New(m.Lookup("Root"))
	if !m.Read(rootA, []byte(`{"entries":{"7":100}}`), &rep) {
		t.Fatal("the Go leg read the first entry")
	}
	rootB := m.New(m.Lookup("Root"))
	if !m.Read(rootB, []byte(`{"entries":{"7":200}}`), &rep) {
		t.Fatal("the Go leg read the second entry")
	}
	rootA.Fields[0].Entries = append(rootA.Fields[0].Entries, rootB.Fields[0].Entries[0])

	wire, err := tablewire.Encode(m, rootA)
	if err != nil {
		t.Fatalf("the Go leg encodes the duplicate-key map: %v", err)
	}

	// The Go oracle.
	var ref tabletext.Report
	got := m.New(m.Lookup("Root"))
	if _, err := tablewire.Decode(m, got, wire, &ref); err != nil {
		t.Fatalf("Go reference decode: %v", err)
	}
	if ref.Duplicate != 1 {
		t.Fatalf("Go reference duplicate = %d, want 1", ref.Duplicate)
	}
	refEntries := got.Fields[0].Entries
	if len(refEntries) != 1 {
		t.Fatalf("Go reference entry count = %d, want 1", len(refEntries))
	}
	if refEntries[0].Tab.Fields[1].Cell.U != 200 {
		t.Fatalf("Go reference last value = %d, want 200", refEntries[0].Tab.Fields[1].Cell.U)
	}

	// Generate the Dart wire codec for the same unit and load the same wire.
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart must carry the map root: %v", err)
	}
	var wireName string
	for k := range files {
		if strings.HasSuffix(k, "Wire.dart") {
			wireName = k
		}
	}
	if wireName == "" {
		t.Fatal("Generate emitted no Wire.dart for the map root")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wireName), files[wireName], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dup.bin"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';

import '` + wireName + `';

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final r = TableWireReport();
  final root = rootWireLoad(wire, r);
  final n = root.entries.length;
  final last = n > 0 ? root.entries[n - 1].value : 0;
  stdout.writeln('${r.duplicate} $n $last');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", "dup.bin")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Dart load of the duplicate-key wire: %v\n%s", err, out)
	}
	want := "1 1 200\n"
	if string(out) != want {
		t.Fatalf("Dart reported %q, want %q", string(out), want)
	}
}
