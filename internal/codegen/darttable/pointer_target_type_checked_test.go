// TestDartPointerTargetTypeChecked holds docs/SPEC-TABLES.md §3.1's rule for a
// pointer's target type (docs/rulings.json pointer-target-type-checked) on the
// Dart leg: in the variable form, a node record whose type this reader DECLARES
// but which is not the type its pointer slot requires is damage, and the whole
// load refuses. A record whose type this reader cannot name at all — a newer
// writer's type — stays lawful: it is counted `unknown` once and every pointer
// naming it reads null.
//
// Every row of testdata/wire/pointer-target-type/MANIFEST.tsv runs through both
// read entries, the plain load and the retaining load; the verdict alone is
// compared (docs/rulings.json refusal-verdict-only), and the unknown-type row
// also holds that its pointer reads null.
package darttable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestDartPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(root, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := dartPointerTargetRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	for _, row := range rows {
		if _, err := os.Stat(filepath.Join(vectors, row.file)); err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", row.file, err)
		}
		if row.unit != rows[0].unit || row.root != rows[0].root {
			t.Fatalf("%s reads %s at %s, want one unit and root for the probe", row.name, row.unit, row.root)
		}
	}

	u := loadDirUnit(t, filepath.Join(root, rows[0].unit))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string][]byte{}
	for name, src := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	for name, src := range files {
		if !strings.Contains(name, "/") {
			set[name] = src
		}
	}
	lib := runtimeHome(u) + "Wire.dart"
	if _, ok := set[lib]; !ok {
		t.Fatalf("the unit generated no %s: the wire entry point moved", lib)
	}
	prefix := dartName(rows[0].root)
	for _, entry := range []string{prefix + "WireLoad(", prefix + "WireLoadRetain("} {
		if !strings.Contains(string(set[lib]), entry) {
			t.Fatalf("%s carries no %s: the read entry point moved", lib, entry)
		}
	}

	var probeRows strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&probeRows, "  ('%s', %q, %d),\n", row.file, row.verdict, row.unknown)
	}
	probe := strings.Replace(dartPointerTargetProbe, "LIB", lib, 1)
	probe = strings.ReplaceAll(probe, "ROOT", prefix)
	probe = strings.Replace(probe, "ROWS\n", probeRows.String(), 1)
	set["pointer_target_probe.dart"] = []byte(probe)
	dir := t.TempDir()
	writeFiles(t, dir, set)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "pointer_target_probe.dart", vectors)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the pointer-target probe failed: %v\n%s", err, out)
	}
}

type dartPointerTargetRow struct {
	name    string
	unit    string
	root    string
	file    string
	verdict string
	unknown int // -1 means the row does not pin a count
}

func dartPointerTargetRows(t *testing.T, manifest string) []dartPointerTargetRow {
	t.Helper()
	var rows []dartPointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := dartPointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

const dartPointerTargetProbe = `import 'dart:io';
import 'dart:typed_data';
import 'LIB' as w;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stdout.writeln('FAIL: $what');
    failed = true;
  }
}

void main(List<String> args) {
  Uint8List wire(String name) => File('${args[0]}/$name').readAsBytesSync();
  for (final (row, verdict, unknown) in [
ROWS
  ]) {
    final reads = verdict == 'read';
    final r = w.TableWireReport();
    final v = w.ROOTWireLoad(wire(row), r);
    check(r.refused != reads, '$row: the plain load ${r.refused ? "refused" : "read"} (want $verdict)');
    if (reads && unknown >= 0) {
      check(r.unknown == unknown, '$row: the plain load counted ${r.unknown} unknown, want $unknown');
    }
    if (row == 'next-writer-only.bin') {
      check(v.next == null, '$row: the plain load read a non-null pointer for a type it cannot name');
    }
    final rr = w.TableWireReport();
    final retain = w.TableRetain(1 << 16, List<int>.filled(4096, 0));
    final rv = w.ROOTWireLoadRetain(wire(row), retain, rr);
    check(rr.refused != reads, '$row: the retaining load ${rr.refused ? "refused" : "read"} (want $verdict)');
    if (reads && unknown >= 0) {
      check(rr.unknown == unknown, '$row: the retaining load counted ${rr.unknown} unknown, want $unknown');
    }
    if (row == 'next-writer-only.bin') {
      check(rv.next == null, '$row: the retaining load read a non-null pointer for a type it cannot name');
    }
    if (!reads) {
      check(retain.used == 0 && retain.idUsed == 0, '$row: a refused retaining load left retained bytes or ids');
    }
  }
  if (failed) exit(1);
  stdout.writeln('ok: every pointer-target vector gave its verdict through both loads');
}
`
