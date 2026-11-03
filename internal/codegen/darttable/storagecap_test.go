package darttable

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §11, §6.5) live in
// testdata/wire/tables/storage-cap beside the unit they are read under. Big's
// storage is 2^27 bytes and one element or node record of it costs two wire
// bytes, so 8191 of them measure under the 2^40-byte cap and 8192 measure past
// it: the list vectors reach the cap through the list term, the node vectors
// through the node records.
const storageCapDir = "../../../testdata/wire/tables/storage-cap"

// Big's constructor throws in every probe below, so a load that built even one
// Big before refusing fails the probe instead of allocating 2^27 bytes a
// record: the refusal is held to come before anything is materialized.
const (
	storageCapBigField    = "List<int> a = List<int>.generate(16777216, (_) => 0);"
	storageCapBigSentinel = "List<int> a = throw StateError('Big materialized');"
)

// TestStorageCapRefusedDart: rootWireMeasureReason answers a size in
// (0, 2^40] for each at vector and -1 with storage_over_cap for each over
// vector; rootWireLoad and rootWireLoadRetain refuse each over vector as
// storage_over_cap, with no malformed event and no Big built. A load is never
// run on an at vector, which would build 2^40 bytes.
func TestStorageCapRefusedDart(t *testing.T) {
	slowtest.Gate(t, "dart")
	if out, err := runStorageCapDart(t, nil); err != nil {
		t.Fatalf("storage cap probe: %v\n%s", err, out)
	}
}

// TestStorageCapDartNegativeControl removes each check in the generated unit
// and requires the probe to go red where that check alone holds the vectors.
// Each sabotaged probe stops before a load once its measure has failed, and
// Big's sentinel stops a load that passes the gate, so no control allocates.
func TestStorageCapDartNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	const (
		term  = "count > (_kTableStorageCap - base - at) ~/ f.size"
		node  = "if (total + nodeTotal > _kTableStorageCap) return (-1, 'storage_over_cap');"
		final = "if (size > _kTableStorageCap) return (-1, 'storage_over_cap');"
		gate  = "_rootWireMeasureFramed(data).$2 == 'storage_over_cap'"
	)
	for _, c := range []struct {
		name  string
		edits [][2]string
		red   []string // labels the probe must print
		green []string // labels it must not print
	}{
		// the totals: the shared vectors' list term is exactly the cap, so
		// only a total refuses them; term_over.bin stays refused by its term
		{"totals", [][2]string{{node, ""}, {final, ""}}, []string{"list_over.bin measure", "nodes_over.bin measure"}, []string{"term_over.bin measure"}},
		// the term check as well: nothing refuses term_over.bin
		{"term", [][2]string{{term, "false"}, {node, ""}, {final, ""}}, []string{"term_over.bin measure"}, nil},
		// the load's gate: the load builds a Big
		{"gate", [][2]string{{gate, "false"}}, []string{"Big materialized"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := runStorageCapDart(t, func(src string) string {
				for _, e := range c.edits {
					if !strings.Contains(src, e[0]) {
						t.Fatalf("sabotage matched nothing: %q", e[0])
					}
					src = strings.ReplaceAll(src, e[0], e[1])
				}
				return src
			})
			if err == nil {
				t.Fatalf("sabotage %s stayed green:\n%s", c.name, out)
			}
			for _, label := range c.red {
				if !strings.Contains(string(out), label) {
					t.Errorf("control %s missed %q:\n%s", c.name, label, out)
				}
			}
			for _, label := range c.green {
				if strings.Contains(string(out), label) {
					t.Errorf("control %s also broke %q:\n%s", c.name, label, out)
				}
			}
		})
	}
}

// storageCapTermWire is a Root whose index map (one empty entry) comes before
// its items list of 8192 empty Big bodies: the list term starts past 0, so the
// term alone passes the cap and the term check refuses it before any total.
func storageCapTermWire(u *ir.Unit) []byte {
	root := u.Tables["Root"]
	ids := []uint64{ir.TableFieldWireId(root.Fields[2]), ir.TableFieldWireId(root.Fields[0])}
	framed := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	wire := append([]byte{1, 1, byte(ir.TableKindArray)}, framed([]byte{byte(ir.TableKindTable), 1, 1, 0})...)
	list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, 8192)
	for range 8192 {
		list = append(list, 1, 0)
	}
	wire = append(append(wire, 2, byte(ir.TableKindArray)), framed(list)...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func runStorageCapDart(t *testing.T, edit func(string) string) ([]byte, error) {
	t.Helper()
	dir, err := filepath.Abs(storageCapDir)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := buildUnit(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const wire = "CapcheckWire.dart"
	text := string(files[wire])
	if !strings.Contains(text, storageCapBigField) {
		t.Fatalf("%s holds no Big field to guard", wire)
	}
	text = strings.Replace(text, storageCapBigField, storageCapBigSentinel, 1)
	if edit != nil {
		text = edit(text)
	}
	files[wire] = []byte(text)
	for name, b := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			files[name] = b
		}
	}
	work := t.TempDir()
	files["term_over.bin"] = storageCapTermWire(u)
	writeFiles(t, work, files)
	probe := strings.ReplaceAll(storageCapDartProbe, "DIR", dir)
	if err := os.WriteFile(filepath.Join(work, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = work
	return cmd.CombinedOutput()
}

const storageCapDartProbe = `import 'dart:io';
import 'dart:typed_data';
import 'CapcheckWire.dart' as w;
bool failed = false;
void check(bool ok, String label) { if (!ok) { stderr.writeln('FAIL: $label'); failed = true; } }
Uint8List read(String name) => File(name.startsWith('term') ? name : 'DIR/$name').readAsBytesSync();
const capBytes = 1 << 40;
void main() {
  for (final name in ['list_at.bin', 'nodes_at.bin']) {
    final wire = read(name);
    final (size, why) = w.rootWireMeasureReason(wire);
    check(size > 0 && size <= capBytes && why == 'ok' && w.rootWireMeasure(wire) == size, '$name measure $size $why');
  }
  for (final name in ['list_over.bin', 'nodes_over.bin', 'term_over.bin']) {
    final wire = read(name);
    final (size, why) = w.rootWireMeasureReason(wire);
    final refused = size == -1 && why == 'storage_over_cap' && w.rootWireMeasure(wire) == -1;
    check(refused, '$name measure $size $why');
    if (!refused) continue;
    final report = w.TableWireReport()..unknown = 3;
    final v = w.rootWireLoad(wire, report);
    check(report.refused && report.reason == 'storage_over_cap' && !report.malformed && report.unknown == 3, '$name load ${report.reason}');
    check(v.items.isEmpty && v.next == null && v.index.isEmpty, '$name load lands nothing');
    final store = w.TableRetain(4096, List<int>.filled(512, 0));
    final retained = w.TableWireReport();
    final rv = w.rootWireLoadRetain(wire, store, retained);
    check(retained.refused && retained.reason == 'storage_over_cap' && !retained.malformed, '$name retained load ${retained.reason}');
    check(rv.items.isEmpty && rv.next == null && store.used == 0 && store.records.isEmpty, '$name retained load lands nothing');
    // a refusal does not outlive its read
    final clean = w.rootWireLoad(Uint8List.fromList([1, 0, 0, 0, 0, 0, 0, 0, 0, 0]), report);
    check(!report.refused && report.reason == 'ok' && clean.items.isEmpty, '$name refusal cleared by the next read ${report.reason}');
  }
  if (failed) exit(1);
  stdout.writeln('storage cap: measure, load and retained load hold the cap');
}
`
