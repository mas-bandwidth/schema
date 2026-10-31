package darttable

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCyclicWireIsDamageDart holds docs/SPEC-TABLES.md §3.1's ruling on the
// Dart leg: a cyclic wire is damage, and every read refuses the whole load
// (Glenn, 2026-10-31). The framing vectors node_cycle_self, node_cycle_to_root
// and node_cycle_mid are each read through graphdemo's ListNode WireLoad and
// WireLoadRetain, and each must report malformed and hand back the type's
// defaults; node_chain_two (a plain chain) and node_diamond_chain (Scene, 82
// shared nodes and 2^40 paths) must load clean, the diamond inside a deadline
// an exponential walk cannot meet.
//
// Its negative control, TestCyclicWireIsDamageDartNegativeControl, makes the
// generated runtime's cyclic() answer false and requires the probe red on the
// three cyclic rows alone.
func TestCyclicWireIsDamageDart(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	out, err := runCycleProbe(t, nil)
	if err != nil {
		t.Fatalf("the cycle probe failed: %v\n%s", err, out)
	}
}

// TestCyclicWireIsDamageDartNegativeControl is the gate's reversed witness
// (tables-dart-cycle-negative-control).
func TestCyclicWireIsDamageDartNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	const sig = "  bool cyclic() {\n"
	out, err := runCycleProbe(t, func(f map[string][]byte) int {
		n := 0
		for name, src := range f {
			if s := string(src); strings.Contains(s, sig) {
				f[name] = []byte(strings.Replace(s, sig, sig+"    if (edges.isNotEmpty) return false; // SABOTAGED\n", 1))
				n++
			}
		}
		return n
	})
	if err == nil {
		t.Fatalf("the probe stayed green with cyclic() answering false: the gate is blind\n%s", out)
	}
	for _, row := range []string{"node_cycle_self", "node_cycle_to_root", "node_cycle_mid"} {
		if !strings.Contains(string(out), "FAIL: "+row) {
			t.Errorf("the reversed witness did not fail on %s:\n%s", row, out)
		}
	}
	for _, row := range []string{"node_chain_two", "node_diamond_chain"} {
		if strings.Contains(string(out), "FAIL: "+row) {
			t.Errorf("the reversed witness failed on the acyclic %s too:\n%s", row, out)
		}
	}
}

func runCycleProbe(t *testing.T, edit func(map[string][]byte) int) ([]byte, error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
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
	if !strings.Contains(string(set["GraphdemoWire.dart"]), "listNodeWireLoadRetain(") {
		t.Fatal("GraphdemoWire.dart carries no listNodeWireLoadRetain: the retaining entry point moved")
	}
	if edit != nil && edit(set) == 0 {
		t.Fatal("the sabotage matched nothing: cyclic() moved in the generated runtime")
	}
	dir := t.TempDir()
	writeFiles(t, dir, set)
	if err := os.WriteFile(filepath.Join(dir, "cycle_probe.dart"), []byte(cycleProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "cycle_probe.dart", filepath.Join(root, "testdata", "wire", "tables", "framing"))
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const cycleProbe = `import 'dart:io';
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
  for (final row in ['node_cycle_self', 'node_cycle_to_root', 'node_cycle_mid']) {
    final r = g.TableWireReport();
    final v = g.listNodeWireLoad(wire(row), r);
    check(r.malformed, '$row: the load did not report the cycle as damage');
    check(v.next == null, '$row: a refused load handed back a value');
    final rr = g.TableWireReport();
    final retain = g.TableRetain(1 << 16, List<int>.filled(1024, 0));
    final rv = g.listNodeWireLoadRetain(wire(row), retain, rr);
    check(rr.malformed, '$row: the retaining load did not report the cycle as damage');
    check(rv.next == null, '$row: a refused retaining load handed back a value');
  }
  {
    final r = g.TableWireReport();
    final v = g.listNodeWireLoad(wire('node_chain_two'), r);
    check(!r.malformed && v.next != null, 'node_chain_two: a chain did not load clean');
    final rr = g.TableWireReport();
    final rv = g.listNodeWireLoadRetain(wire('node_chain_two'), g.TableRetain(1 << 16, List<int>.filled(1024, 0)), rr);
    check(!rr.malformed && rv.next != null, 'node_chain_two: a chain did not load clean through the retaining load');
  }
  {
    final watch = Stopwatch()..start();
    final r = g.TableWireReport();
    g.sceneWireLoad(wire('node_diamond_chain'), r);
    check(!r.malformed, 'node_diamond_chain: a diamond chain of shared nodes reported damage');
    check(watch.elapsedMilliseconds < 10000, 'node_diamond_chain: the load took ${watch.elapsedMilliseconds} ms, past the 10 s a linear pass meets');
  }
  if (failed) exit(1);
  stdout.writeln('ok: three cyclic rows refused through both loads, a chain and a diamond chain loaded');
}
`
