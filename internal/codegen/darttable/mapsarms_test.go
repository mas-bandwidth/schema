package darttable

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireDart is the maps-and-arms card's red-first
// anchor. The Dart table backend carries, on form byte 1 and byte for byte
// against the Go oracle (internal/tablewire), the constructs
// dart-carries-the-variable-class-beyond-the-card-constructs left refused:
// maps (docs/SPEC-TABLES.md §2.8), unions whose arms are tables, arrays of
// unions and union arms of any field type (§2.6, §3), optional fields and
// optional arrays (§2.3), enum-keyed arrays (§2.4), the fixed-point and
// 128-bit scalars (§3, §15) and wstring(N) (§3, kind 33).
//
// Each case is one corpus unit. The unit must generate (never a refusal), the
// generated <Home>Wire.dart must hold a codec for every root the oracle rows
// name, and every oracle wire must survive Load then Save byte for byte. The
// rows come from the conformance manifest for the four units it registers
// (mapdemo, messagedemo, scalars, widedemo) and from the committed arms_*.bin
// oracles for tables/arms, so a row added to the data is a row this test reads.
func TestMapsAndUnionArmsOnTheTableWireDart(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..", "..")
	cases := []struct {
		name string
		unit string // repo-relative corpus directory
		wire string // the generated form-1 library
		rows []oracleRow
	}{
		{name: "maps", unit: "tables/maps", wire: "MapdemoWire.dart", rows: manifestRows(t, repo, "mapdemo")},
		{name: "arms", unit: "tables/arms", wire: "ArmdemoWire.dart", rows: []oracleRow{
			{"arms_chain", "Chain", "testdata/wire/tables/arms_chain.bin", ""},
			{"arms_gate", "Gate", "testdata/wire/tables/arms_gate.bin", ""},
			{"arms_gate_text", "Gate", "testdata/wire/tables/arms_gate_text.bin", ""},
			{"arms_hand", "Hand", "testdata/wire/tables/arms_hand.bin", ""},
			{"arms_holder", "Holder", "testdata/wire/tables/arms_holder.bin", ""},
			{"arms_nest", "Nest", "testdata/wire/tables/arms_nest.bin", ""},
			{"arms_rack", "Rack", "testdata/wire/tables/arms_rack.bin", ""},
			{"arms_ring", "Ring", "testdata/wire/tables/arms_ring.bin", ""},
			{"arms_tray", "Tray", "testdata/wire/tables/arms_tray.bin", ""},
		}},
		{name: "messages", unit: "tables/messages", wire: "MessagedemoWire.dart", rows: manifestRows(t, repo, "messagedemo")},
		{name: "scalars", unit: "tables/scalars", wire: "ScalardemoWire.dart", rows: manifestRows(t, repo, "scalars")},
		{name: "wide", unit: "examples-wide", wire: "WideWire.dart", rows: manifestRows(t, repo, "widedemo")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if len(c.rows) == 0 {
				t.Fatalf("no oracle rows for %s: the corpus lost its expectation", c.unit)
			}
			schemas, err := filepath.Glob(filepath.Join(repo, c.unit, "*.schema"))
			if err != nil || len(schemas) == 0 {
				t.Fatalf("no schema files under %s: %v", c.unit, err)
			}
			u := loadUnit(t, schemas...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Dart must carry %s on the variable wire, never refuse it: %v", c.unit, err)
			}
			if len(files[c.wire]) == 0 {
				t.Fatalf("Generate emitted no %s for %s — the form-1 codec is not carried yet", c.wire, c.unit)
			}
			resaveOracleRows(t, c.wire, files, repo, u, c.rows)
		})
	}
}

// oracleRow is one committed oracle wire and the root it is an instance of.
type oracleRow struct {
	name string
	root string
	path string // repo-relative
	// refusal is the manifest's `go-refusal=<name>`: a writer refuses the
	// value by that name (docs/SPEC-TABLES.md §2.5, a blob two slots name), so
	// the save must throw it and no bytes are compared
	refusal string
}

// manifestRows reads the `instance` rows of one unit out of the committed
// conformance manifest (testdata/conformance/tables/MANIFEST.txt, whose shape
// FORMAT.md beside it states): `instance <name> <unit> <root> <wire> [flags]`.
func manifestRows(t *testing.T, repo, unit string) []oracleRow {
	t.Helper()
	f, err := os.Open(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer f.Close()
	var rows []oracleRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "instance" || fields[2] != unit {
			continue
		}
		r := oracleRow{name: fields[1], root: fields[3], path: fields[4]}
		for _, a := range fields[5:] {
			if name, ok := strings.CutPrefix(a, "go-refusal="); ok {
				r.refusal = name
			}
			if a == "wire-refused" {
				// A wire-refused row names one blob from two slots; every
				// writer refuses it as blob_shared (SPEC §2.5).
				r.refusal = "blob_shared"
			}
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return rows
}

// resaveOracleRows writes the generated libraries and one probe that loads every
// row's oracle wire through its root's codec, saves it back, and requires the
// bytes to be identical, naming every row that is not. A FIXED ROOT IS FORM 3
// (docs/SPEC-TABLES.md §3.4; internal/tablewire/fixedroot_test.go): its oracle
// is a form-3 file, so its resave rides <root>FixedLoad/<root>FixedSave and
// never the form-1 wire codec. A variable root rides <root>WireLoad/
// <root>WireSave. A row with a refusal must instead throw the refusal's name.
func resaveOracleRows(t *testing.T, wireName string, files map[string][]byte, repo string, u *ir.Unit, rows []oracleRow) {
	slowtest.Gate(t, "an external toolchain")

	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, packetFiles(t, u))
	wire := files[wireName]
	if len(wire) == 0 {
		t.Fatalf("no %s form-1 codec", wireName)
	}
	if err := os.WriteFile(filepath.Join(dir, wireName), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	// THE FIXED LIBRARIES (form 3) BESIDE THE VARIABLE ONE: the fixed roots'
	// oracle wires are form 3, so their resave runs through the Fixed codec.
	for name, src := range files {
		if strings.HasSuffix(name, "Fixed.dart") {
			if err := os.WriteFile(filepath.Join(dir, name), src, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	probe := resaveOracleProbe(wireName, files, rows)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--enable-asserts", "probe.dart"}
	for _, r := range rows {
		abs, err := filepath.Abs(filepath.Join(repo, r.path))
		if err != nil {
			t.Fatal(err)
		}
		refusal := r.refusal
		if refusal == "" {
			refusal = "-"
		}
		args = append(args, r.name, r.root, abs, refusal)
	}
	cmd := exec.Command(dartBinary(t), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("oracle round trip over the generated %s: %v\n%s", wireName, err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe did not report ok: %s", out)
	}
}

// resaveOracleProbe is the probe resaveOracleRows writes. A root whose
// artifact contains `int <name>FixedLoad(` resaves with FixedLoad/FixedSave (one read: the records or a
// refusal) and never WireLoad/WireSave. That artifact is a form-3 file. A root with no Fixed
// codec stays on the form-1 wire. A fixed table past the form's bounds keeps
// form 1 and has no FixedLoad.
func resaveOracleProbe(wireName string, files map[string][]byte, rows []oracleRow) string {
	var fixedNames []string
	for name := range files {
		if strings.HasSuffix(name, "Fixed.dart") {
			fixedNames = append(fixedNames, name)
		}
	}
	sort.Strings(fixedNames)
	fixedRoots := map[string]bool{}
	for _, r := range rows {
		if r.root == "" {
			continue
		}
		fn := strings.ToLower(r.root[:1]) + r.root[1:]
		for _, name := range fixedNames {
			if strings.Contains(string(files[name]), "int "+fn+"FixedLoad(") {
				fixedRoots[r.root] = true
			}
		}
	}
	roots := map[string]bool{}
	for _, r := range rows {
		if r.root != "" {
			roots[r.root] = true
		}
	}
	var names []string
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	var imports strings.Builder
	imports.WriteString("import '" + wireName + "';\n")
	for _, name := range fixedNames {
		// Prefixed: the Fixed and Wire libraries define the same table
		// classes, so an unprefixed import of both is ambiguous.
		imports.WriteString("import '" + name + "' as f;\n")
	}
	var sw strings.Builder
	for _, r := range names {
		fn := strings.ToLower(r[:1]) + r[1:]
		if fixedRoots[r] {
			// A FIXED ROOT IS FORM 3, BOTH WAYS (§3.4): its oracle is a form-3
			// file and rides the Fixed codec — one record, one whole read or a
			// refusal — never form 1.
			sw.WriteString("    case '" + r + "':\n      {\n")
			sw.WriteString("        final values = List<f." + r + ">.generate(1, (_) => f." + r + "());\n")
			sw.WriteString("        final plan = f." + fn + "FixedNewPlan();\n")
			sw.WriteString("        final report = f.TableFixedReport();\n")
			sw.WriteString("        if (f." + fn + "FixedLoad(values, 1, wire, wire.length, plan, report) != 1 ||\n")
			sw.WriteString("            report.refused != 0) {\n")
			sw.WriteString("          throw StateError('fixed load refused');\n")
			sw.WriteString("        }\n")
			sw.WriteString("        final out = Uint8List(f." + fn + "FixedMeasure(1));\n")
			sw.WriteString("        if (f." + fn + "FixedSave(values, 1, out) < 0) {\n")
			sw.WriteString("          throw StateError('fixed save refused');\n")
			sw.WriteString("        }\n")
			sw.WriteString("        return out;\n")
			sw.WriteString("      }\n")
		} else {
			// The strict read is one read: the whole value or a refusal
			// named in the report, never a default resaved as form 1.
			sw.WriteString("    case '" + r + "':\n      {\n")
			sw.WriteString("        final report = TableWireReport();\n")
			sw.WriteString("        final v = " + fn + "WireLoad(wire, report);\n")
			sw.WriteString("        if (report.refused) throw StateError(report.refusal);\n")
			sw.WriteString("        return " + fn + "WireSave(v);\n")
			sw.WriteString("      }\n")
		}
	}
	return `import 'dart:io';
import 'dart:typed_data';
` + imports.String() + `
Uint8List resave(String root, Uint8List wire) {
  switch (root) {
` + sw.String() + `    default:
      throw StateError('no codec for root $root');
  }
}

void main(List<String> args) {
  var bad = 0;
  for (var i = 0; i + 3 < args.length; i += 4) {
    final name = args[i];
    final wire = File(args[i + 2]).readAsBytesSync();
    final refusal = args[i + 3];
    final Uint8List saved;
    try {
      saved = resave(args[i + 1], wire);
    } catch (e) {
      if (refusal != '-' && e.toString().contains(refusal)) continue;
      stderr.writeln('FAIL $name: $e');
      bad++;
      continue;
    }
    if (refusal != '-') {
      stderr.writeln('FAIL $name: saved ${saved.length} bytes of a value every writer refuses ($refusal)');
      bad++;
      continue;
    }
    var at = -1;
    final n = saved.length < wire.length ? saved.length : wire.length;
    for (var k = 0; k < n; k++) {
      if (saved[k] != wire[k]) {
        at = k;
        break;
      }
    }
    if (at < 0 && saved.length != wire.length) at = n;
    if (at >= 0) {
      stderr.writeln('FAIL $name: resaved ${saved.length} bytes, oracle ${wire.length}, first difference at byte $at');
      bad++;
    }
  }
  if (bad != 0) exit(1);
  stdout.writeln('ok');
}
`
}

// packetFiles is the unit's packet libraries (internal/codegen/dart): the
// table libraries import the packet emitter's enum extension types, so a
// probe directory carries them beside the table library under test.
func packetFiles(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// writeFiles writes every file of files into dir.
func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
