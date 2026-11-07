package darttable

import (
	"fmt"
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

// TestWireMapKeyScanMatchesGoDart pins the KEY SCAN of docs/SPEC-TABLES.md
// §2.8 on the Dart wire reader against the Go reference,
// internal/tablewire (*wireReader).mapField and mapKey: every one-byte
// mutation of the map payload of a duplicate-key map wire (built with the Go leg) is loaded by
// both, and wherever Go reads the wire cleanly, Dart's duplicate,
// kind_mismatch and malformed answers and its landed entry count equal Go's.
// A body the scan cannot frame is damage, and DAMAGE DISCARDS THE WHOLE LOAD
// (§4): there Dart owes malformed and nothing else is compared. A key under a
// foreign kind empties the map with ONE kind_mismatch.
func TestWireMapKeyScanMatchesGoDart(t *testing.T) {
	t.Parallel()

	const src = `package probe
table Root {
	entries map[int32]int32
}
`
	u := buildUnit(t, src)
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

	// The mutants and the Go oracle's answer for each one Go reads. Only the
	// MAP FIELD's payload is mutated (form byte, field reference, kind 14 and
	// its one-byte L lead the wire): damage to the root's own framing or to
	// the trailer is the opener's business, not the key scan's.
	if len(wire) < 4 || wire[2] != 14 || wire[3] >= 0x80 {
		t.Fatalf("the map field does not lead the wire as the test expects: %x", wire)
	}
	var names, wants []string
	var damaged []bool
	for i := 4; i < 4+int(wire[3]); i++ {
		for _, b := range []byte{0x00, 0x01, 0x02, 0x0c, 0x0e, 0xff} {
			if wire[i] == b {
				continue
			}
			mut := append([]byte(nil), wire...)
			mut[i] = b
			var ref tabletext.Report
			got := m.New(m.Lookup("Root"))
			if _, err := tablewire.Decode(m, got, mut, &ref); err != nil || ref.Refused {
				continue
			}
			name := fmt.Sprintf("m%d_%02x.bin", i, b)
			if err := os.WriteFile(filepath.Join(dir, name), mut, 0o600); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
			wants = append(wants, fmt.Sprintf("%s %d %d %t %d", name, ref.Duplicate, ref.KindMismatch, ref.Malformed, len(got.Fields[0].Entries)))
			damaged = append(damaged, ref.Malformed)
		}
	}
	if len(names) == 0 {
		t.Fatal("no mutant the Go leg reads")
	}

	probe := `import 'dart:io';

import '` + wireName + `';

void main(List<String> args) {
  for (final name in args) {
    final r = TableWireReport();
    final root = rootWireLoad(File(name).readAsBytesSync(), r);
    stdout.writeln('$name ${r.duplicate} ${r.kindMismatch} ${r.malformed} ${root.entries.length}');
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, names...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Dart load of the mutants: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != len(wants) {
		t.Fatalf("Dart answered %d mutants, want %d\n%s", len(got), len(wants), out)
	}
	for i := range wants {
		if damaged[i] {
			if fields := strings.Fields(got[i]); len(fields) != 5 || fields[3] != "true" {
				t.Errorf("mutant %s is damaged and Dart did not refuse it: %q", names[i], got[i])
			}
			continue
		}
		if got[i] != wants[i] {
			t.Errorf("mutant (name duplicate kind_mismatch malformed entries): Dart %q, Go %q", got[i], wants[i])
		}
	}
}
