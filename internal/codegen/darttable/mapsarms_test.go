package darttable

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
			{"arms_chain", "Chain", "testdata/wire/tables/arms_chain.bin"},
			{"arms_gate", "Gate", "testdata/wire/tables/arms_gate.bin"},
			{"arms_gate_text", "Gate", "testdata/wire/tables/arms_gate_text.bin"},
			{"arms_hand", "Hand", "testdata/wire/tables/arms_hand.bin"},
			{"arms_holder", "Holder", "testdata/wire/tables/arms_holder.bin"},
			{"arms_nest", "Nest", "testdata/wire/tables/arms_nest.bin"},
			{"arms_rack", "Rack", "testdata/wire/tables/arms_rack.bin"},
			{"arms_ring", "Ring", "testdata/wire/tables/arms_ring.bin"},
			{"arms_tray", "Tray", "testdata/wire/tables/arms_tray.bin"},
		}},
		{name: "messages", unit: "tables/messages", wire: "MessagedemoWire.dart", rows: manifestRows(t, repo, "messagedemo")},
		{name: "scalars", unit: "tables/scalars", wire: "ScalardemoWire.dart", rows: manifestRows(t, repo, "scalars")},
		{name: "wide", unit: "examples-wide", wire: "WideWire.dart", rows: manifestRows(t, repo, "widedemo")},
	}

	for _, c := range cases {
		c := c
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
			wire, ok := files[c.wire]
			if !ok || len(wire) == 0 {
				t.Fatalf("Generate emitted no %s for %s — the form-1 codec is not carried yet", c.wire, c.unit)
			}
			resaveOracleRows(t, c.wire, wire, repo, c.rows)
		})
	}
}

// oracleRow is one committed oracle wire and the root it is an instance of.
type oracleRow struct {
	name string
	root string
	path string // repo-relative
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
		rows = append(rows, oracleRow{name: fields[1], root: fields[3], path: fields[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return rows
}

// resaveOracleRows writes the generated library and one probe that loads every
// row's oracle wire through <root>WireLoad, saves it through <root>WireSave,
// and requires the bytes to be identical, naming every row that is not.
func resaveOracleRows(t *testing.T, wireName string, wire []byte, repo string, rows []oracleRow) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wireName), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	roots := map[string]bool{}
	for _, r := range rows {
		roots[r.root] = true
	}
	var names []string
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	var sw strings.Builder
	for _, r := range names {
		fn := strings.ToLower(r[:1]) + r[1:]
		sw.WriteString("    case '" + r + "':\n      return " + fn + "WireSave(" + fn + "WireLoad(wire));\n")
	}
	probe := `import 'dart:io';
import 'dart:typed_data';
import '` + wireName + `';

Uint8List resave(String root, Uint8List wire) {
  switch (root) {
` + sw.String() + `    default:
      throw StateError('no codec for root $root');
  }
}

void main(List<String> args) {
  var bad = 0;
  for (var i = 0; i + 2 < args.length; i += 3) {
    final name = args[i];
    final wire = File(args[i + 2]).readAsBytesSync();
    final Uint8List saved;
    try {
      saved = resave(args[i + 1], wire);
    } catch (e) {
      stderr.writeln('FAIL $name: $e');
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
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--enable-asserts", "probe.dart"}
	for _, r := range rows {
		abs, err := filepath.Abs(filepath.Join(repo, r.path))
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, r.name, r.root, abs)
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
