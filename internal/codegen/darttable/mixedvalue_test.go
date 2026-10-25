package darttable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The listdemo unit (tables/lists/SaveGame.schema). The Mixed table carries four
// lists of four element classes — an enum, a flags mask, a union and a ranged
// int32 — under the wire-fuzzer cause D9b.
const listdemoMixedSchema = `package listdemo

fixed table Placement
{
    x     float32
    y     float32
    model uint32
}

fixed table LogEntry { tick uint32 }

table Save
{
    placements []Placement
    log        []*LogEntry
    scores     []int32
}

enum Grade { A, B, C }

flags Perm { Read, Write, Own }

fixed table Point
{
    x int32
    y int32
}

union Hit
{
    point  Point
    damage int32
}

table Mixed
{
    grades []Grade
    perms  []Perm
    hits   []Hit
    bounds []int32 | min = 0, max = 100
}
`

// The wire-fuzzer mutant (run seed 24845619678, listdemo.Mixed, random pass
// #23) that exposed the decoded-value divergence: a repeat under the enum list
// " grades id whose payload declares element kind 9 (u64) instead of 30. The
// Go oracle keeps the first, good, decode; a reader that clears the list
// before checking the element kind wrongly empties it.
const mixedListDecodedMutantHex = "01010e051e03020304010e12090203000000000000000400000010000000060e1a0f03070d2d080401000000090402000000000a040407000000000b0e0e040300000000320000006400000000c599f782764e0ad9ec2202864cfc63af522602864cfa63af052802864cff63afa82e867084edf24abbf00c9bccfb2d73b145c34435abfe73071702864cf563af541502864cf463afc07fb18abe08637f68b7f0ae4c0cf6530b00000000000000"

// The oracle's saved bytes for the mutant above: the Go engine
// (internal/tablewire DecodeRegion+Encode) reads grades as [A, None, C] and
// the other three lists empty, re-encoding 42 bytes.
const mixedListDecodedOracleHex = "01010e051e0302000300c599f782764e0ad9ec2202864cfc63af052802864cff63af0300000000000000"

// TestWireMixedListDecodedValueDart pins cause D9b: a decoded-value divergence
// on listdemo.Mixed. The generated Dart reader must save the oracle's bytes on
// the mutant, byte for byte — the list holder is cleared only after the
// element-kind check passes, so a repeat under the same id whose payload is of
// another element kind leaves the previously-decoded value standing.
func TestWireMixedListDecodedValueDart(t *testing.T) {
	t.Parallel()

	u := buildUnit(t, listdemoMixedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files["ListdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no ListdemoWire.dart for the listdemo unit")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ListdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	mutant, err := hex.DecodeString(mixedListDecodedMutantHex)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), mutant, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'dart:typed_data';

import 'ListdemoWire.dart';

String hexEncode(List<int> bytes) {
  const digits = '0123456789abcdef';
  final sb = StringBuffer();
  for (final b in bytes) {
    sb.write(digits[b >> 4]);
    sb.write(digits[b & 15]);
  }
  return sb.toString();
}

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final v = mixedWireLoad(wire);
  final saved = mixedWireSave(v);
  stdout.writeln(hexEncode(saved));
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", "mutant.bin")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mixed list decoded-value probe: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != mixedListDecodedOracleHex {
		t.Fatalf("generated Dart saved %d bytes (first divergence would be at byte %d), the oracle %d:\n  dart:    %s\n  oracle:   %s\n  mutant:   %s",
			len(got)/2, firstDiff(got, mixedListDecodedOracleHex), len(mixedListDecodedOracleHex)/2,
			got, mixedListDecodedOracleHex, mixedListDecodedMutantHex)
	}
}

func firstDiff(a, b string) int {
	n := min(len(b), len(a))
	for i := range n {
		if a[i] != b[i] {
			return i / 2
		}
	}
	return n / 2
}
