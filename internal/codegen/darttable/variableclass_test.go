package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableClassBeyondCardConstructsDart is the variable-class card's
// red-first anchor. It pins the acceptance boundary the card must reach: the
// Dart table backend carries the FULL variable class on form byte 1 — the
// remaining scalar kinds (uint8..uint64, int8, int16, float32, float64, bits,
// bytes(N)) as codecs, never silence; blobs (*bytes beside the carried
// *string); unbounded arrays with scalar, string and enum elements; arrays of
// pointers ([]*T); and the 64-bit id-table wire with escape kind 31 — byte for
// byte against the Go oracle (internal/tablewire, internal/tabletext), through
// the corpus units tables/pointers (graphdemo), tables/blobs (blobdemo) and
// tables/lists (listdemo), and the scalar kinds through a declaration written
// in this test (docs/SPEC-TABLES.md §2.5, §2.9, §3, §15, §16.7).
func TestVariableClassBeyondCardConstructsDart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		unit   string
		root   string
		wire   string
		oracle string
	}{
		{
			name:   "pointers",
			unit:   "tables/pointers",
			root:   "Scene",
			wire:   "GraphdemoWire.dart",
			oracle: filepath.Join("..", "..", "..", "testdata", "wire", "tables", "graph_tree.bin"),
		},
		{
			name:   "blobs",
			unit:   "tables/blobs",
			root:   "Catalog",
			wire:   "BlobdemoWire.dart",
			oracle: filepath.Join("..", "..", "..", "testdata", "wire", "tables", "blob_small.bin"),
		},
		{
			name:   "lists",
			unit:   "tables/lists",
			root:   "Save",
			wire:   "ListdemoWire.dart",
			oracle: filepath.Join("..", "..", "..", "testdata", "wire", "tables", "list_tables.bin"),
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			schemas, err := filepath.Glob(filepath.Join("..", "..", "..", c.unit, "*.schema"))
			if err != nil || len(schemas) == 0 {
				t.Fatalf("no schema files under %s: %v", c.unit, err)
			}
			u := loadUnit(t, schemas...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Dart must carry %s on the variable wire, never refuse it: %v", c.name, err)
			}
			wire, ok := files[c.wire]
			if !ok || len(wire) == 0 {
				t.Fatalf("Generate emitted no %s for %s — the variable class is not carried yet", c.wire, c.unit)
			}
			roundTripOracle(t, c.root, c.wire, wire, c.oracle)
		})
	}

	t.Run("scalar kinds", func(t *testing.T) {
		t.Parallel()
		u := buildUnit(t, scalarKindsDecl)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("the remaining scalar kinds must be carried as codecs, never refused: %v", err)
		}
		wire, ok := files["ScalarWire.dart"]
		if !ok || len(wire) == 0 {
			t.Fatal("Generate emitted no ScalarWire.dart — the scalar kinds get no codec (silently kind 0)")
		}
		scalarRoundTrip(t, wire)
	})

	t.Run("bytes blob", func(t *testing.T) {
		t.Parallel()
		u := buildUnit(t, bytesBlobDecl)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("*bytes must be carried as a blob, never refused: %v", err)
		}
		wire, ok := files["BlobWire.dart"]
		if !ok || len(wire) == 0 {
			t.Fatal("Generate emitted no BlobWire.dart — *bytes gets no codec")
		}
		bytesBlobRoundTrip(t, wire)
	})

	t.Run("scalar arrays", func(t *testing.T) {
		t.Parallel()
		u := buildUnit(t, scalarArraysDecl)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("unbounded scalar/string/enum arrays must be carried, never refused: %v", err)
		}
		wire, ok := files["SarraysWire.dart"]
		if !ok || len(wire) == 0 {
			t.Fatal("Generate emitted no SarraysWire.dart — unbounded scalar arrays get no codec")
		}
		scalarArrayRoundTrip(t, wire)
	})
}

// roundTripOracle loads a committed oracle .bin through the generated library's
// <root>WireLoad, re-saves it, and requires the bytes to be identical — the
// same contract the Go leg's committed byte oracle holds for the card record.
func roundTripOracle(t *testing.T, root, wireName string, wire []byte, oracle string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wireName), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	fn := strings.ToLower(root[:1]) + root[1:]
	probe := `import 'dart:io';
import '` + wireName + `';

void check(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('FAIL: $msg');
    exit(1);
  }
}

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final v = ` + fn + `WireLoad(wire);
  final resaved = ` + fn + `WireSave(v);
  check(resaved.length == wire.length, 'length ${resaved.length} != ${wire.length}');
  for (var i = 0; i < wire.length; i++) {
    if (resaved[i] != wire[i]) {
      check(false, 'resaved differs from oracle at byte $i');
    }
  }
  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	oracleAbs, err := filepath.Abs(oracle)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", oracleAbs)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s round trip over the generated library: %v\n%s", root, err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// scalarRoundTrip saves and loads the scalar-kind declaration through its own
// generated codec and requires the values to survive, the way the field kinds
// must never be silenced to kind 0.
func scalarRoundTrip(t *testing.T, wire []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ScalarWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'ScalarWire.dart';

void main() {
  final v = ScalarKinds();
  v.u8 = 200;
  v.u64 = -1; // 2^64-1, the two's-complement spelling of uint64's max
  v.i8 = -128;
  v.i16 = -32768;
  v.f32 = 1.5;
  v.f64 = -0.25;
  v.mask = 511;
  v.blob = 'abc';
  final resaved = scalarKindsWireSave(v);
  final w = scalarKindsWireLoad(resaved);
  if (w.u8 != 200 || w.u64 != -1 || w.i8 != -128 ||
      w.i16 != -32768 || w.f32 != 1.5 || w.f64 != -0.25 ||
      w.mask != 511 || w.blob != 'abc') {
    stdout.writeln('FAIL: scalar values did not survive the round trip');
    exit(1);
  }
  final text = scalarKindsToJson(v);
  final t = scalarKindsFromJson(text);
  if (t.u8 != 200 || t.u64 != -1 || t.i8 != -128 ||
      t.i16 != -32768 || t.f32 != 1.5 || t.f64 != -0.25 ||
      t.mask != 511 || t.blob != 'abc') {
    stdout.writeln('FAIL: scalar values did not survive the text round trip');
    exit(1);
  }
  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scalar round trip over the generated library: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// scalarKindsDecl is the remaining scalar-kind declaration written in the test:
// every scalar kind the card must carry as a codec rather than silently encode
// as kind 0.
const scalarKindsDecl = `package scalar

table ScalarKinds {
	u8    uint8
	u16   uint16
	u32   uint32
	u64   uint64
	i8    int8
	i16   int16
	f32   float32
	f64   float64
	mask  bits(9)
	blob  bytes(16)
}
`

// bytesBlobDecl is the *bytes blob declaration written in the test: a byte
// buffer beside a *string, so the two reserved node type ids are told apart on
// the node table (§2.5, §3.1).
const bytesBlobDecl = `package blob

table Blob {
	data  *bytes
	caption *string
}
`

// bytesBlobRoundTrip saves and loads a *bytes blob through its own generated
// codec and requires the bytes to survive beside a *string in the same node
// table, the way the two reserved blob ids must never be confused.
func bytesBlobRoundTrip(t *testing.T, wire []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BlobWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'dart:typed_data';
import 'BlobWire.dart';

void main() {
  final v = Blob();
  v.data = Uint8List.fromList([1, 2, 3, 255]);
  v.caption = 'cap';
  final resaved = blobWireSave(v);
  final w = blobWireLoad(resaved);
  if (w.data == null || w.data!.length != 4 ||
      w.data![0] != 1 || w.data![1] != 2 || w.data![2] != 3 || w.data![3] != 255 ||
      w.caption != 'cap') {
    stdout.writeln('FAIL: bytes blob did not survive the round trip');
    exit(1);
  }
  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bytes blob round trip over the generated library: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// scalarArraysDecl is the unbounded scalar/string/enum array declaration
// written in the test: the five element classes §2.9 names, so a `[]T` with a
// non-table element is carried as its element's own kind rather than mis-encoded
// as a table body.
const scalarArraysDecl = `package sarrays

enum Grade { A, B, C }

table Arrays {
	ints   []int32
	octets []uint8
	flts   []float32
	flags  []bool
	grades []Grade
}
`

// scalarArrayRoundTrip saves and loads the unbounded scalar/string/enum array
// declaration through its own generated codec and requires every element value
// to survive, the way a `[]T` with a non-table element must never be encoded
// as a table body (docs/SPEC-TABLES.md §2.9, §3).
func scalarArrayRoundTrip(t *testing.T, wire []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SarraysWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'SarraysWire.dart';

void main() {
  final v = Arrays();
  v.ints = [1, -2, 300];
  v.octets = [0, 200, 255];
  v.flts = [1.5, -0.25];
  v.flags = [true, false, true];
  v.grades = [1, 3, 0];
  final resaved = arraysWireSave(v);
  final w = arraysWireLoad(resaved);
  var ok = w.ints.length == 3 && w.ints[0] == 1 && w.ints[1] == -2 && w.ints[2] == 300;
  ok = ok && w.octets.length == 3 && w.octets[0] == 0 && w.octets[1] == 200 && w.octets[2] == 255;
  ok = ok && w.flts.length == 2 && w.flts[0] == 1.5 && w.flts[1] == -0.25;
  ok = ok && w.flags.length == 3 && w.flags[0] == true && w.flags[1] == false && w.flags[2] == true;
  ok = ok && w.grades.length == 3 && w.grades[0] == 1 && w.grades[1] == 3 && w.grades[2] == 0;
  if (!ok) {
    stdout.writeln('FAIL: scalar array values did not survive the round trip');
    exit(1);
  }
  stdout.writeln('ok');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scalar array round trip over the generated library: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}
