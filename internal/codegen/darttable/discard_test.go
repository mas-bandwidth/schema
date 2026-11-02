package darttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// discardUnit has a fixed root and a variable root, each carrying a nested
// body between two scalars, so damage inside the nested body leaves the walk
// able to read on past its L: the case where a reader that kept what it
// decoded would hand back a value.
const discardUnit = `package discard

fixed table Inner
{
    n int32 = 0
}

fixed table Root
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
}

table Graph
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

func discardLoadUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "discard.schema")
	if err := os.WriteFile(path, []byte(discardUnit), 0o600); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}

func discardInstance(t *testing.T, m *tabletext.Model, root string) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"a":5,"inner":{"n":3},"b":9}`), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	return inst
}

// discardWireDart encodes the root and then damages the nested body: Inner's
// one field, kind 4 holding 3, gets the kind 37 no reader can skip. The engine
// is asked first, so the vector is damaged by the rule and not by assumption.
func discardWireDart(t *testing.T, u *ir.Unit, root string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	wire, err := tablewire.Encode(m, discardInstance(t, m, root))
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return wire
}

// discardMessages is a batch of one Root body the engine reads whole, then one
// byte past the pad: damage after the last body, which discards the batch.
func discardMessages(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := discardInstance(t, m, "Root")
	data, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, 0x01)
	vocab := &tablewire.Vocabulary{}
	if err := vocab.AnnounceRead(ir.TableAnnouncement(u), &tabletext.Report{}); err != nil {
		t.Fatalf("announce: %v", err)
	}
	var rep tabletext.Report
	if _, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Root"))}, data, vocab, &rep); ok || err != nil || !rep.Malformed {
		t.Fatalf("the engine does not read the batch as damage: ok=%v err=%v %+v", ok, err, rep)
	}
	return data
}

// TestDamageDiscardsTheWholeLoadDart is the Dart leg's gate for DAMAGE
// DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): damage inside a nested
// body, with a whole field before it and after it, makes WireLoad hand back the
// type's defaults for a fixed root and a variable root with the report saying
// malformed, WireLoadRetain hand back defaults with the retention store empty,
// and LoadMessages answer no body with every body at its defaults. Red if any
// read hands back what it decoded.
func TestDamageDiscardsTheWholeLoadDart(t *testing.T) {
	t.Parallel()
	dart := dartBinary(t)
	u := discardLoadUnit(t)
	fixed := discardWireDart(t, u, "Root")
	variable := discardWireDart(t, u, "Graph")

	files, err := generateWireFiles(u)
	if err != nil {
		t.Fatalf("generateWireFiles: %v", err)
	}
	home := runtimeHome(u)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import '%[1]sWire.dart';

void fail(String what) {
  print(what);
  exit(1);
}

void main() {
  final fixed = Uint8List.fromList(%[2]s);
  final rf = TableWireReport();
  final f = rootWireLoad(fixed, rf);
  if (!rf.malformed) fail('fixed: the report does not say malformed');
  if (f.a != 0 || f.b != 0 || f.inner.n != 0) fail('fixed: WireLoad handed back a=${f.a} b=${f.b} n=${f.inner.n} on damage');

  final variable = Uint8List.fromList(%[3]s);
  final rg = TableWireReport();
  final g = graphWireLoad(variable, rg);
  if (!rg.malformed) fail('variable: the report does not say malformed');
  if (g.a != 0 || g.b != 0 || g.inner.n != 0 || g.next != null) fail('variable: WireLoad handed back a=${g.a} b=${g.b} n=${g.inner.n} on damage');

  final retain = TableRetain(1 << 16, List<int>.filled(64, 0));
  final rr = TableWireReport();
  final k = graphWireLoadRetain(variable, retain, rr);
  if (!rr.malformed) fail('retain: the report does not say malformed');
  if (k.a != 0 || k.b != 0 || k.inner.n != 0) fail('retain: WireLoadRetain handed back a=${k.a} b=${k.b} n=${k.inner.n} on damage');
  if (retain.used != 0 || retain.records.isNotEmpty) fail('retain: the store kept used=${retain.used} records=${retain.records.length}');
  print('ok');
}
`, home, dartBytes(fixed), dartBytes(variable))
	if err := os.WriteFile(filepath.Join(dir, "discard_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "--enable-asserts", "discard_probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the Dart wire loads did not discard on damage: %v\n%s", err, out)
	}

	batch := discardMessages(t, u)
	body := fmt.Sprintf(`
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final values = <h.Root>[h.Root()];
  final rep = h.TableReport();
  final count = r.rootLoadMessages(values, Uint8List.fromList(%s), vocab, rep);
  check(rep.malformed, 'messages: the report does not say malformed');
  check(count == 0, 'messages: LoadMessages answered $count bodies on damage');
  check(values[0].a == 0 && values[0].b == 0 && values[0].inner.n == 0,
      'messages: LoadMessages handed back a=${values[0].a} b=${values[0].b} n=${values[0].inner.n} on damage');
`, dartBytes(batch))
	if out, err := runBatchProbe(t, u, "Root", body); err != nil {
		t.Fatalf("the Dart message batch did not discard on damage: %v\n%s", err, out)
	}
}
