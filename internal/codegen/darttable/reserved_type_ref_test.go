package darttable

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestDartNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
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
		if filepath.Base(name) == name {
			set[name] = src
		}
	}
	for name, src := range files {
		if filepath.Base(name) == name {
			set[name] = src
		}
	}
	dir := t.TempDir()
	writeFiles(t, dir, set)
	if err := os.WriteFile(filepath.Join(dir, "reserved_type_probe.dart"), []byte(reservedTypeProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, dartBinary(t), "--enable-asserts", "reserved_type_probe.dart", filepath.Join(root, "testdata", "wire", "tables", "framing"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reserved type probe failed: %v\n%s", err, out)
	}
}

const reservedTypeProbe = `import 'dart:io';
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
  for (final row in [
    'node_type_ref_zero',
    'node_type_node_table_id',
    'node_type_build_version_id',
    'node_type_vocabulary_id',
  ]) {
    final report = g.TableWireReport();
    g.sceneWireLoad(wire(row), report);
    check(report.refused, '$row: the load did not refuse a reserved node type id');
  }
  final control = g.TableWireReport();
  g.sceneWireLoad(wire('node_chain_two'), control);
  check(!control.refused, 'node_chain_two: a valid chain was refused');
  if (failed) exit(1);
  stdout.writeln('ok: reserved node type ids refused and node_chain_two loaded');
}
`
