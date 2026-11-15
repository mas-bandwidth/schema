package darttable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestDartGraphCheckedOverEveryPointer holds docs/SPEC-TABLES.md §6.5 (THE
// GRAPH, DEFINED ONCE) and §3.1 on the Dart leg: the graph conditions are
// checked over EVERY kind-17 payload in the input, in fields and records this
// reader declares and in those it only skips. A pointer naming a node whose
// descent is still open is a cycle, a record no pointer reaches is
// unreachable, a blob record named from two slots is blob_shared, and a string
// blob record is well-formed text wherever it is reached from; each refuses
// the whole load. A record of a type this reader cannot name, reached through
// any pointer, reads null and counts unknown: that stays a read.
//
// Each graph_* framing vector runs through graphdemo Scene's strict load and
// its retaining load, and the verdict alone is compared (docs/rulings.json
// refusal-verdict-only); node_chain_two through ListNode is the control that
// reads.
func TestDartGraphCheckedOverEveryPointer(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	refused := []string{
		"graph_unknown_type_self",
		"graph_unknown_type_pair",
		"graph_listnode_unknown_self",
		"graph_unknown_to_root",
		"graph_blob_two_unknown_fields",
		"graph_blob_two_unknown_elements",
		"graph_string_blob_ill_formed",
		"graph_string_blob_zero_byte",
		"graph_unknown_type_unreached",
		"graph_string_blob_unreached",
	}
	read := []string{
		"graph_reach_unknown_field",
		"graph_reach_unknown_nested",
		"graph_reach_unknown_union_arm",
		"graph_reach_unknown_keyed_slot",
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	for _, name := range append(append([]string{"node_chain_two"}, refused...), read...) {
		if _, err := os.Stat(filepath.Join(framing, name+".bin")); err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", name, err)
		}
	}
	u := loadDirUnit(t, filepath.Join(root, "tables", "pointers"))
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
	for _, entry := range []string{"sceneWireLoad(", "sceneWireLoadRetain(", "listNodeWireLoad(", "listNodeWireLoadRetain("} {
		if !strings.Contains(string(set["GraphdemoWire.dart"]), entry) {
			t.Fatalf("GraphdemoWire.dart carries no %s: the entry point moved", entry)
		}
	}
	var rows strings.Builder
	for _, name := range refused {
		fmt.Fprintf(&rows, "  ('%s', false),\n", name)
	}
	for _, name := range read {
		fmt.Fprintf(&rows, "  ('%s', true),\n", name)
	}
	set["graph_probe.dart"] = []byte(strings.Replace(graphEveryPointerProbe, "ROWS\n", rows.String(), 1))
	dir := t.TempDir()
	writeFiles(t, dir, set)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "graph_probe.dart", framing)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the graph probe failed: %v\n%s", err, out)
	}
}

const graphEveryPointerProbe = `import 'dart:io';
import 'dart:typed_data';
import 'GraphdemoWire.dart' as g;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stdout.writeln('FAIL: $what');
    failed = true;
  }
}

void main(List<String> args) {
  Uint8List wire(String name) => File('${args[0]}/$name.bin').readAsBytesSync();
  for (final (row, reads) in [
ROWS
  ]) {
    final r = g.TableWireReport();
    g.sceneWireLoad(wire(row), r);
    check(r.refused != reads, '$row: the load ${reads ? "refused" : "read"} (want ${reads ? "read" : "refused"})');
    final rr = g.TableWireReport();
    final retain = g.TableRetain(1 << 16, List<int>.filled(1024, 0));
    g.sceneWireLoadRetain(wire(row), retain, rr);
    check(rr.refused != reads, '$row: the retaining load ${reads ? "refused" : "read"} (want ${reads ? "read" : "refused"})');
    if (!reads) {
      check(retain.used == 0 && retain.idUsed == 0, '$row: a refused retaining load left retained bytes or ids');
    }
  }
  {
    final r = g.TableWireReport();
    final v = g.listNodeWireLoad(wire('node_chain_two'), r);
    check(!r.refused && v.next != null, 'node_chain_two: a chain did not load clean');
    final rr = g.TableWireReport();
    final rv = g.listNodeWireLoadRetain(wire('node_chain_two'), g.TableRetain(1 << 16, List<int>.filled(1024, 0)), rr);
    check(!rr.refused && rv.next != null, 'node_chain_two: a chain did not load clean through the retaining load');
  }
  if (failed) exit(1);
  stdout.writeln('ok: every graph vector gave its verdict through both loads');
}
`
