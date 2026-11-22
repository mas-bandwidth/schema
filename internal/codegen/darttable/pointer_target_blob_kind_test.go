// TestDartPointerTargetBlobKindRefused holds docs/SPEC-TABLES.md §3.1 and §6.5
// for the two reserved blob ids on the Dart leg (docs/rulings.json
// blob-target-outside-closure-refused): every reader of the variable form can
// name a *string or *bytes record, whatever its root's pointer closure
// reaches. A declared slot naming a blob record of the other kind refuses the
// whole read, and so does a blob record no pointer reaches; a blob record
// named only from a field this reader does not declare is a newer writer's
// field, read and counted unknown.
//
// The Go engine is the oracle: every row of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv, and the three
// feature-witness rows that name a record outside the reader root's closure
// (testdata/conformance/feature-witnesses/pointer-targets), is decoded by
// internal/tablewire first, and the Dart leg's plain and retaining loads must
// give the engine's verdict, unknown count and value, in a debug and a
// release run. The leg's own saves are decoded by the engine, which must
// accept each with nothing unknown: the writer emits no unreachable record.
package darttable

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

func TestDartPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := dartPointerTargetRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	for _, row := range rows {
		if row.unit != rows[0].unit {
			t.Fatalf("%s reads unit %s, want the one unit %s", row.name, row.unit, rows[0].unit)
		}
	}
	u := loadDirUnit(t, filepath.Join(root, rows[0].unit))
	m := tabletext.NewModel(u)
	files := dartBlobKindFiles(t, u)
	lib := runtimeHome(u) + "Wire.dart"
	if _, ok := files[lib]; !ok {
		t.Fatalf("the unit generated no %s: the wire entry point moved", lib)
	}

	// THE ORACLE: the engine reads each vector first, through both of its
	// loads, and must answer the manifest; the Dart probe then holds the leg
	// to the engine's verdict, count and value.
	var probe strings.Builder
	fmt.Fprintf(&probe, dartBlobKindPrelude, lib)
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(vectors, row.file))
		if err != nil {
			t.Fatalf("%s: the pinned vector is missing: %v", row.file, err)
		}
		def := u.Tables[row.root]
		if def == nil {
			t.Fatalf("%s: the reader unit declares no %s", row.name, row.root)
		}
		var want string
		for _, retaining := range []bool{false, true} {
			value := m.New(def)
			var r tabletext.Report
			if retaining {
				_, _ = tablewire.DecodeRetain(m, value, wire, &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}, &r)
			} else {
				_, _ = tablewire.Decode(m, value, wire, &r)
			}
			if r.Refused != (row.verdict == "refused") {
				t.Fatalf("%s: the engine (retaining=%v) refused=%v (%v), the manifest says %s", row.name, retaining, r.Refused, err, row.verdict)
			}
			if row.verdict == "read" {
				if row.unknown >= 0 && r.Unknown != row.unknown {
					t.Fatalf("%s: the engine (retaining=%v) counted %d unknown, the manifest pins %d", row.name, retaining, r.Unknown, row.unknown)
				}
				text, err := m.Write(value)
				if err != nil {
					t.Fatal(err)
				}
				want = string(text) + "\n"
			}
		}
		unknown := row.unknown
		if row.verdict != "read" {
			unknown = -1
		}
		fn := dartName(row.root)
		fmt.Fprintf(&probe, "  vector(%q, bytes(%q), %t, %d, %q,\n    (d, r) => w.%sToJson(w.%sWireLoad(d, r)),\n    (d, s, r) => w.%sToJson(w.%sWireLoadRetain(d, s, r)));\n",
			row.name, hex.EncodeToString(wire), row.verdict == "read", unknown, want, fn, fn, fn, fn)
	}
	probe.WriteString(dartBlobKindWriters)
	probe.WriteString("  if (failed) exit(1);\n}\n")
	source := probe.String()

	t.Run("every blobkind vector gives the engine's verdict on both loads", func(t *testing.T) {
		t.Parallel()
		saves := 0
		defer func() {
			if saves != 12 {
				t.Errorf("the probe printed %d saves, want 12 (Text and Bin, null, empty and set, plain and retaining)", saves)
			}
		}()
		for _, line := range dartBlobKindRun(t, files, source) {
			if !strings.HasPrefix(line, "save ") {
				continue
			}
			saves++
			// THE WRITER: each of the leg's saves, plain and retaining, of
			// a Text and a Bin with the blob null, empty and set, must
			// decode in the engine accepted with nothing unknown.
			f := strings.Fields(line)
			if len(f) != 4 {
				t.Fatalf("probe line %q: want save <root> <label> <hex>", line)
			}
			wire, err := hex.DecodeString(f[3])
			if err != nil {
				t.Fatalf("probe line %q: %v", line, err)
			}
			def := u.Tables[f[1]]
			if def == nil {
				t.Fatalf("probe line %q: no table %s", line, f[1])
			}
			for _, retaining := range []bool{false, true} {
				var r tabletext.Report
				var ok bool
				if retaining {
					ok, err = tablewire.DecodeRetain(m, m.New(def), wire, &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}, &r)
				} else {
					ok, err = tablewire.Decode(m, m.New(def), wire, &r)
				}
				if !ok || err != nil || r.Refused || r.Unknown != 0 {
					t.Errorf("%s %s: the leg's save does not read back clean in the engine (retaining=%v): ok=%v err=%v refused=%v unknown=%d", f[1], f[2], retaining, ok, err, r.Refused, r.Unknown)
				}
			}
		}
	})

	// NEGATIVE CONTROL: plant the old rule, "can name" decided from the
	// root's pointer closure for blobs, in a copy of the generated reader.
	// A Text root's closure has no *bytes edge, so the planted reader reads
	// text-names-bytes's pointer as an unknown record and the slot null; the
	// gate must go red on that row by name.
	t.Run("negative control: the root-closure rule reads text-names-bytes", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const fair = "      typeId == _kStringBlobId ||\n      typeId == _kBytesBlobId;\n"
		const old = "      (typeId == _kStringBlobId && stringBlob) ||\n      (typeId == _kBytesBlobId && bytesBlob);\n"
		hits := 0
		for name, src := range planted {
			if n := strings.Count(string(src), fair); n > 0 {
				hits += n
				planted[name] = []byte(strings.Replace(string(src), fair, old, 1))
			}
		}
		if hits != 1 {
			t.Fatalf("the planted fault applied %d times, want 1: _placedNode moved", hits)
		}
		out, err := dartBlobKindExec(t, planted, source, "--enable-asserts")
		if err == nil {
			t.Fatalf("the gate passed a reader that decides blob naming from the root's closure:\n%s", out)
		}
		if !strings.Contains(out, "FAIL: text-names-bytes:") {
			t.Fatalf("the planted reader failed, but not on text-names-bytes:\n%s", out)
		}
	})

	// THE THREE FLIPPED FEATURE-WITNESS ROWS: a declared slot naming a record
	// the reader's root closure does not reach (a table, a *bytes blob, a
	// *string blob) is damage. The engine refuses each; so must both Dart
	// loads.
	t.Run("the flipped feature-witness rows refuse", func(t *testing.T) {
		t.Parallel()
		schema, raw := sharedDartFixture(t, "pointer-targets")
		var cases []struct {
			Name, Writer, Reader string
			Input                json.RawMessage
		}
		if err := json.Unmarshal(raw, &cases); err != nil {
			t.Fatal(err)
		}
		wantNames := []string{"table outside root closure", "bytes outside root closure", "string outside root closure"}
		if len(cases) < 10 {
			t.Fatalf("pointer-targets corpus has %d rows, want rows 8 to 10", len(cases))
		}
		wu := unitFrom(t, schema)
		wm := tabletext.NewModel(wu)
		wfiles := dartBlobKindFiles(t, wu)
		var b strings.Builder
		fmt.Fprintf(&b, dartBlobKindPrelude, runtimeHome(wu)+"Wire.dart")
		for i, name := range wantNames {
			c := cases[7+i]
			if c.Name != name {
				t.Fatalf("witness row %d is %q, want %q", 8+i, c.Name, name)
			}
			writer := wm.New(wu.Tables[c.Writer])
			var wr tabletext.Report
			if !wm.Read(writer, c.Input, &wr) || !wr.Silent() {
				t.Fatalf("%s: writer input %+v", c.Name, wr)
			}
			wire, err := tablewire.Encode(wm, writer)
			if err != nil {
				t.Fatal(err)
			}
			for _, retaining := range []bool{false, true} {
				var r tabletext.Report
				if retaining {
					_, _ = tablewire.DecodeRetain(wm, wm.New(wu.Tables[c.Reader]), wire, &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}, &r)
				} else {
					_, _ = tablewire.Decode(wm, wm.New(wu.Tables[c.Reader]), wire, &r)
				}
				if !r.Refused {
					t.Fatalf("%s: the engine (retaining=%v) read it (%v); the witness row is a refusal", c.Name, retaining, err)
				}
			}
			fn := dartName(c.Reader)
			fmt.Fprintf(&b, "  vector(%q, bytes(%q), false, -1, '',\n    (d, r) => w.%sToJson(w.%sWireLoad(d, r)),\n    (d, s, r) => w.%sToJson(w.%sWireLoadRetain(d, s, r)));\n",
				c.Name, hex.EncodeToString(wire), fn, fn, fn, fn)
		}
		b.WriteString("  if (failed) exit(1);\n}\n")
		dartBlobKindRun(t, wfiles, b.String())
	})

	// WHAT A READER NAMES IS ITS UNIT, WHAT IT PLACES IS ITS ROOT: the same
	// split for table records. A declared table under a slot of a root whose
	// closure holds only a blob edge is a wrong-type target and refuses; a
	// declared table the root cannot place, named only from a field this
	// reader does not declare, reads and counts two unknown (the field and the
	// record), as text-unknown-field-bytes does for a blob. The engine judges
	// each wire first.
	t.Run("declared tables outside the root's closure follow the same split", func(t *testing.T) {
		t.Parallel()
		writer := unitFrom(t, `package probe
table Wrong { body *Bin }
table More { body *string
 extra *Bin }
table RootMore { a *A
 extra *B }
table RootWrong { a *B }
table A { v int32 }
table B { v int32 }
table Bin { data *bytes }
`)
		reader := unitFrom(t, `package probe
table Text { body *string }
table Root { a *A }
table A { v int32 }
table B { v int32 }
table Bin { data *bytes }
`)
		wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
		var b strings.Builder
		fmt.Fprintf(&b, dartBlobKindPrelude, runtimeHome(reader)+"Wire.dart")
		for _, c := range []struct{ name, writer, reader, input string }{
			{"blob-only root, a declared slot names a declared table", "Wrong", "Text", `{"body":{"data":null}}`},
			{"blob-only root, an undeclared field names a declared table", "More", "Text", `{"body":"x","extra":{"data":null}}`},
			{"blob-only root, an undeclared field names a declared table and its blob", "More", "Text", `{"body":"x","extra":{"data":"AQI="}}`},
			{"table root, an undeclared field names a declared table outside its closure", "RootMore", "Root", `{"a":{"v":1},"extra":{"v":2}}`},
			{"table root, a declared slot names a declared table outside its closure", "RootWrong", "Root", `{"a":{"v":2}}`},
		} {
			value := wm.New(writer.Tables[c.writer])
			var wr tabletext.Report
			if !wm.Read(value, []byte(c.input), &wr) || !wr.Silent() {
				t.Fatalf("%s: writer input %+v", c.name, wr)
			}
			wire, err := tablewire.Encode(wm, value)
			if err != nil {
				t.Fatal(err)
			}
			var reads []bool
			var unknown []int
			want := ""
			for _, retaining := range []bool{false, true} {
				out := rm.New(reader.Tables[c.reader])
				var r tabletext.Report
				if retaining {
					_, _ = tablewire.DecodeRetain(rm, out, wire, &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}, &r)
				} else {
					_, _ = tablewire.Decode(rm, out, wire, &r)
				}
				reads = append(reads, !r.Refused)
				unknown = append(unknown, r.Unknown)
				if !r.Refused {
					text, err := rm.Write(out)
					if err != nil {
						t.Fatal(err)
					}
					want = string(text) + "\n"
				}
			}
			if reads[0] != reads[1] || unknown[0] != unknown[1] {
				t.Fatalf("%s: the engine's two loads disagree: read %v, unknown %v", c.name, reads, unknown)
			}
			fn := dartName(c.reader)
			fmt.Fprintf(&b, "  vector(%q, bytes(%q), %t, %d, %q,\n    (d, r) => w.%sToJson(w.%sWireLoad(d, r)),\n    (d, s, r) => w.%sToJson(w.%sWireLoadRetain(d, s, r)));\n",
				c.name, hex.EncodeToString(wire), reads[0], unknown[0], want, fn, fn, fn, fn)
		}
		b.WriteString("  if (failed) exit(1);\n}\n")
		dartBlobKindRun(t, dartBlobKindFiles(t, reader), b.String())
	})
}

// dartBlobKindFiles is the unit's generated table and packet libraries, the
// top-level files a probe directory carries.
func dartBlobKindFiles(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
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
	return set
}

// dartBlobKindRun runs the probe in a debug and a release build (a READ
// refusal holds in every build) and returns the release run's stdout lines.
func dartBlobKindRun(t *testing.T, files map[string][]byte, source string) []string {
	t.Helper()
	var lines []string
	for _, mode := range []string{"--enable-asserts", ""} {
		out, err := dartBlobKindExec(t, files, source, mode)
		if err != nil {
			t.Fatalf("the blob-kind probe failed (%q): %v\n%s", mode, err, out)
		}
		lines = lines[:0]
		sc := bufio.NewScanner(strings.NewReader(out))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	}
	return lines
}

func dartBlobKindExec(t *testing.T, files map[string][]byte, source, mode string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "blob_kind_probe.dart"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"blob_kind_probe.dart"}
	if mode != "" {
		args = append([]string{mode}, args...)
	}
	cmd := testproc.Command(t, dartBinary(t), args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

const dartBlobKindPrelude = `import 'dart:io';
import 'dart:typed_data';
import '%s' as w;

var failed = false;
void check(bool ok, String what) {
  if (!ok) {
    stdout.writeln('FAIL: $what');
    failed = true;
  }
}

String hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
Uint8List bytes(String s) => Uint8List.fromList([for (var i = 0; i < s.length; i += 2) int.parse(s.substring(i, i + 2), radix: 16)]);

void vector(String row, Uint8List data, bool reads, int unknown, String json,
    String Function(Uint8List, w.TableWireReport) load,
    String Function(Uint8List, w.TableRetain, w.TableWireReport) loadRetain) {
  final r = w.TableWireReport();
  final got = load(data, r);
  check(r.refused != reads, '$row: the plain load ${r.refused ? "refused" : "read"} (the engine ${reads ? "reads" : "refuses"})');
  if (reads) {
    check(unknown < 0 || r.unknown == unknown, '$row: the plain load counted ${r.unknown} unknown, the engine $unknown');
    check(got == json, '$row: the plain load read $got, the engine $json');
  }
  final rr = w.TableWireReport();
  final retain = w.TableRetain(1 << 16, List<int>.filled(1024, 0));
  final rgot = loadRetain(data, retain, rr);
  check(rr.refused != reads, '$row: the retaining load ${rr.refused ? "refused" : "read"} (the engine ${reads ? "reads" : "refuses"})');
  if (reads) {
    check(unknown < 0 || rr.unknown == unknown, '$row: the retaining load counted ${rr.unknown} unknown, the engine $unknown');
    check(rgot == json, '$row: the retaining load read $rgot, the engine $json');
  } else {
    check(retain.used == 0 && retain.idUsed == 0, '$row: a refused retaining load left retained bytes or ids');
  }
}

void main() {
`

// dartBlobKindWriters saves a Text and a Bin with the blob null, empty and
// set, through the plain and the retaining save, prints each wire for the
// engine to judge, and reads each back through the leg's own plain load.
const dartBlobKindWriters = `  void saved(String root, String label, Uint8List data, bool Function(Uint8List, w.TableWireReport) readBack) {
    stdout.writeln('save $root $label ${hex(data)}');
    final r = w.TableWireReport();
    check(readBack(data, r) && !r.refused && r.unknown == 0, '$root $label: the leg does not read its own save back clean');
  }
  final texts = <String, w.Text>{
    'null': w.Text(),
    'empty': w.Text()..body = '',
    'set': w.Text()..body = 'hello',
  };
  for (final e in texts.entries) {
    final rr = w.TableWireReport();
    saved('Text', 'plain-${e.key}', w.textWireSave(e.value), (d, r) => w.textWireLoad(d, r).body == e.value.body);
    saved('Text', 'retain-${e.key}', w.textWireSaveRetain(e.value, w.TableRetain(1 << 16, List<int>.filled(1024, 0)), rr),
        (d, r) => w.textWireLoad(d, r).body == e.value.body);
    check(!rr.refused, 'Text retain-${e.key}: the retaining save refused');
  }
  final bins = <String, w.Bin>{
    'null': w.Bin(),
    'empty': w.Bin()..data = Uint8List(0),
    'set': w.Bin()..data = Uint8List.fromList([1, 2, 3]),
  };
  bool same(Uint8List? a, Uint8List? b) => a == null ? b == null : (b != null && hex(a) == hex(b));
  for (final e in bins.entries) {
    final rr = w.TableWireReport();
    saved('Bin', 'plain-${e.key}', w.binWireSave(e.value), (d, r) => same(w.binWireLoad(d, r).data, e.value.data));
    saved('Bin', 'retain-${e.key}', w.binWireSaveRetain(e.value, w.TableRetain(1 << 16, List<int>.filled(1024, 0)), rr),
        (d, r) => same(w.binWireLoad(d, r).data, e.value.data));
    check(!rr.refused, 'Bin retain-${e.key}: the retaining save refused');
  }
`
