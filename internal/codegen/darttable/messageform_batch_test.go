package darttable

// THE MESSAGE FORM'S BATCH ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// The message form is "A BATCH OF BODIES ... READ AND WRITTEN", and "a single
// message is the batch of one": `SaveMessages` takes an array of bodies of ONE
// root and writes ONE buffer, `LoadMessages` reads one buffer into the caller's
// storage and allocates nothing, and `MeasureMessages` sizes the buffer. The
// announcement that precedes the batch landed on this leg already
// (internal/codegen/darttable/messageform_announce.go); this test owes the
// batch surface itself.
//
// The bytes are the C++ reference's, pinned by testdata/conformance/tables/
// MANIFEST.txt's `message` rows. Each subtest's assertion QUOTES the sentence
// of §3.3 it implements, drives the emitted Dart with the leg's own toolchain,
// and compares against the manifest's message bytes — never against a substring
// of the source.
//
// The Dart leg carries NO form-`1` table wire (schema#514), so the round trip
// this test pins is the message form's own: `LoadMessages` reads the manifest's
// message bytes into a value, `SaveMessages` of that value reproduces those
// bytes exactly, and `MeasureMessages` equals their length. The cross-check
// against the form-`1` instance the manifest also pins is not reachable on this
// leg and is reported, not decided here.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// batchRow is one `message` row of the manifest, resolved to its connection's
// unit and announcement: the root, the message-form bytes and the announcement
// the connection carries, all the reference's.
type batchRow struct {
	name     string
	root     string
	message  []byte
	announce []byte
	unit     string
	path     string
}

// batchManifest reads testdata/conformance/tables/MANIFEST.txt's `message`
// rows, joined to their `connection` (the announcement wire) and `unit` (the
// schema directory) rows. The row is `message <name> <connection> <root>
// <form-1 wire> <message wire>`; the form-1 wire is not opened because this leg
// reads no form 1 (schema#514).
func batchManifest(t *testing.T) []batchRow {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/conformance/tables/MANIFEST.txt")
	if err != nil {
		t.Fatalf("the manifest is not readable: %v", err)
	}
	unitPath := map[string]string{}
	connUnit := map[string]string{}
	connWire := map[string]string{}
	type msgLine struct{ name, conn, root, form1, msg string }
	var msgs []msgLine
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 3 && fields[0] == "unit":
			unitPath[fields[1]] = fields[2]
		case len(fields) == 5 && fields[0] == "connection":
			connUnit[fields[1]] = fields[2]
			connWire[fields[1]] = fields[4]
		case len(fields) == 6 && fields[0] == "message":
			msgs = append(msgs, msgLine{fields[1], fields[2], fields[3], fields[4], fields[5]})
		}
	}
	var out []batchRow
	for _, m := range msgs {
		unit, ok := connUnit[m.conn]
		if !ok {
			t.Fatalf("the manifest carries no connection row for %s", m.conn)
		}
		path, ok := unitPath[unit]
		if !ok {
			t.Fatalf("the manifest carries no unit row for %s", unit)
		}
		msg, err := os.ReadFile(filepath.Join("../../..", m.msg))
		if err != nil {
			t.Fatalf("the manifest's message file %s is unreadable: %v", m.msg, err)
		}
		ann, err := os.ReadFile(filepath.Join("../../..", connWire[m.conn]))
		if err != nil {
			t.Fatalf("the manifest's announcement file %s is unreadable: %v", connWire[m.conn], err)
		}
		out = append(out, batchRow{name: m.name, root: m.root, message: msg, announce: ann, unit: unit, path: path})
	}
	if len(out) == 0 {
		t.Fatal("the manifest carries no message row")
	}
	return out
}

// fixedTableBase answers the library basename of the file that declares a table
// root, the root library its batch verbs land in.
func fixedTableBase(u *ir.Unit, name string) string {
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return f.Base
			}
		}
	}
	return ""
}

// batchUnit loads the manifest unit's schema files, the same files the
// reference's constants were emitted from. A unit row names a directory, so
// every *.schema in it is loaded.
func batchUnit(t *testing.T, path string) *ir.Unit {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join("../../..", path, "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under %s: %v", path, err)
	}
	return loadUnit(t, schemas...)
}

const batchProbeTemplate = `import 'dart:typed_data';

import './@HOME@' as h;
import './@ROOT@' as r;

void check(bool ok, String what) {
  if (!ok) {
    throw StateError(what);
  }
}

void main() {
@BODY@
  print('ok');
}
`

// runBatchProbe emits a unit's whole surface — the packet library and the fixed
// home, verbatim from this package's emitters — beside a generated probe and
// runs it with the leg's own toolchain. The home carries the storage class and
// the shared runtime, the root library carries the batch verbs, and the probe
// imports both, so a defect in either is a compile error, never a green.
func runBatchProbe(t *testing.T, u *ir.Unit, root, body string) (string, error) {
	t.Helper()
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate table: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := runtimeHome(u) + "Fixed.dart"
	rootLib := fixedTableBase(u, root) + "Fixed.dart"
	probe := strings.NewReplacer("@HOME@", home, "@ROOT@", rootLib, "@BODY@", body).Replace(batchProbeTemplate)
	if err := os.WriteFile(filepath.Join(dir, "batch_probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(announceDartBinary(t), "--enable-asserts", "batch_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// roundTripBody drives one message row's three verbs against the manifest's
// pinned bytes (docs/SPEC-TABLES.md §3.3): "SaveMessages takes an array of
// bodies of ONE root and writes ONE buffer. LoadMessages reads one buffer into
// the caller's storage and allocates nothing. MeasureMessages sizes the buffer."
// — LoadMessages reads the reference's message bytes, SaveMessages reproduces
// them exactly, and MeasureMessages equals their length.
func roundTripBody(root string, announce, message []byte) string {
	lower := lowerFirst(root)
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final value = h.%s();
  h.init%s(value);
  final values = <h.%s>[value];
  final loadRep = h.TableReport();
  final count = r.%sLoadMessages(values, msg, vocab, loadRep);
  check(count == 1 && !loadRep.refused && !loadRep.malformed,
      'LoadMessages count=$count refused=${loadRep.refused} malformed=${loadRep.malformed}');
  final measureRep = h.TableReport();
  final measure = r.%sMeasureMessages(values, measureRep);
  check(measure == msg.length, 'MeasureMessages=$measure want=${msg.length}');
  final buffer = Uint8List(measure);
  final saveRep = h.TableReport();
  final wrote = r.%sSaveMessages(values, buffer, saveRep);
  check(wrote == msg.length, 'SaveMessages=$wrote want=${msg.length}');
  var same = true;
  for (var i = 0; i < msg.length; i++) {
    if (buffer[i] != msg[i]) { same = false; }
  }
  check(same, 'the batch bytes differ from the reference');
`, dartBytes(announce), dartBytes(message), root, root, root, lower, lower, lower)
}

// TestMessageFormBatchDart asserts, one subtest per clause of STEP 2, the batch
// surface the Dart leg owes §3.3, against the emitted Dart run with the leg's
// own toolchain.
func TestMessageFormBatchDart(t *testing.T) {
	t.Parallel()

	// 2.1 every manifest `message` row: SaveMessages writes the row's message
	// bytes exactly, LoadMessages gives the batch's value, and MeasureMessages
	// equals the length (docs/SPEC-TABLES.md §3.3).
	t.Run("batch_round_trip", func(t *testing.T) {
		t.Parallel()
		for _, row := range batchManifest(t) {
			t.Run(row.unit+"/"+row.name, func(t *testing.T) {
				t.Parallel()
				u := batchUnit(t, row.path)
				st := findTable(t, u, row.root)
				if !st.IsTable || !st.FixedDeclared {
					// "THE FORM IS A FIXED TABLE'S ... a plain `table T` may
					// not" (docs/SPEC-TABLES.md §3.3): the manifest carries a
					// `message` row for the variable root Scene, and what the
					// Dart leg owes a variable root is reported, not decided.
					t.Skipf("§3.3 is a fixed table's; %s is a plain table", row.root)
				}
				out, err := runBatchProbe(t, u, row.root, roundTripBody(row.root, row.announce, row.message))
				if err != nil {
					t.Fatalf("%s/%s round trip did not run green: %v\n%s", row.unit, row.name, err, out)
				}
			})
		}
	})

	// 2.2 "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME" and "`M` ABOVE
	// THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... The reader
	// sets the returned count to the wire's `M`", and "A BODY FROM AN ENDPOINT THAT
	// NEVER ANNOUNCED IS REFUSED BY NAME" (`no_vocabulary`).
	t.Run("batch_refusals", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

fixed table Root {
    a int32
    b bool
    c uint8
}
`)
		out, err := runBatchProbe(t, u, "Root", batchRefusalsBody)
		if err != nil {
			t.Fatalf("the batch refusals did not run green: %v\n%s", err, out)
		}
	})

	// 2.3 "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`" (one
	// `malformed`), "BYTES AFTER THE PAD ARE MALFORMED", the form byte's own
	// refusal "newer_form", and "message_form_as_file" from the file reader.
	t.Run("batch_damage", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

fixed table Root {
    a int32
    b bool
    c uint8
}
`)
		out, err := runBatchProbe(t, u, "Root", batchDamageBody)
		if err != nil {
			t.Fatalf("the batch damage rules did not run green: %v\n%s", err, out)
		}
	})
}

// batchRefusalsBody drives the two named refusals on the synthetic fixed root
// (docs/SPEC-TABLES.md §3.3). The reference width is four bits (E = 8), so a
// batch of two empty bodies is two zero references.
const batchRefusalsBody = `
  final entries = List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0));
  // "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
  // decoded, no counter moves and 'malformed' does not fire."
  final v = h.Root();
  h.initRoot(v);
  final values = <h.Root>[v];
  final m = r.rootMeasureMessages(values, h.TableReport());
  final buf = Uint8List(m);
  check(r.rootSaveMessages(values, buf, h.TableReport()) == m, 'save');
  final fresh = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final freshRep = h.TableReport();
  final freshCount = r.rootLoadMessages(<h.Root>[h.Root()], buf, fresh, freshRep);
  check(freshCount == -1 && freshRep.refused && freshRep.reason == 'no_vocabulary' && !freshRep.malformed,
      'no_vocabulary: count=$freshCount refused=${freshRep.refused} reason=${freshRep.reason}');
  // "'M' ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... 'MeasureMessages'
  // and 'SaveMessages' both refuse and return '-1'."
  final huge = <h.Root>[for (var i = 0; i < 257; i++) h.Root()];
  final hugeMeasureRep = h.TableReport();
  check(r.rootMeasureMessages(huge, hugeMeasureRep) == -1 &&
      hugeMeasureRep.refused && hugeMeasureRep.reason == 'batch_too_large',
      'measure over 256 bodies did not refuse');
  final hugeSaveRep = h.TableReport();
  check(r.rootSaveMessages(huge, Uint8List(8), hugeSaveRep) == -1 &&
      hugeSaveRep.refused && hugeSaveRep.reason == 'batch_too_large',
      'save over 256 bodies did not refuse');
  // "'M' ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ...
  // The reader sets the returned count to the wire's 'M' before it returns the
  // refusal."
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final two = <h.Root>[h.Root(), h.Root()];
  final twoM = r.rootMeasureMessages(two, h.TableReport());
  final twoBuf = Uint8List(twoM);
  check(r.rootSaveMessages(two, twoBuf, h.TableReport()) == twoM, 'save two bodies');
  final oneStorage = <h.Root>[h.Root()];
  final readRep = h.TableReport();
  final readCount = r.rootLoadMessages(oneStorage, twoBuf, vocab, readRep);
  check(readCount == 2 && readRep.refused && readRep.reason == 'batch_too_large' && !readRep.malformed,
      'read over capacity: count=$readCount refused=${readRep.refused} reason=${readRep.reason}');
`

// batchDamageBody drives the damage and form-byte rules on the synthetic fixed
// root (docs/SPEC-TABLES.md §3.3). M is three: body 1 empty, body 2's first
// reference 0xF above the announced entry count E = 8, body 3 empty. A
// reference is four bits, low bit first, so byte 0 holds body 1's low nibble and
// body 2's high nibble.
const batchDamageBody = `
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  // "DAMAGE INSIDE BODY 'k' DELIVERS BODIES '1' TO 'k - 1'. The returned count
  // states 'k - 1', ONE 'malformed' counts, and nothing at or after body 'k' is
  // read."
  final wire = Uint8List.fromList([2, 2, 0xF0, 0x00]);
  final damageRep = h.TableReport();
  final damageCount = r.rootLoadMessages(<h.Root>[h.Root(), h.Root(), h.Root()], wire, vocab, damageRep);
  check(damageCount == 1 && damageRep.malformed && !damageRep.refused,
      'damage in body 2: count=$damageCount malformed=${damageRep.malformed}');
  // "BYTES AFTER THE PAD ARE MALFORMED ... a buffer with bytes left over
  // describes no batch this reader can name."
  final v = h.Root();
  h.initRoot(v);
  final m = r.rootMeasureMessages(<h.Root>[v], h.TableReport());
  final buf = Uint8List(m);
  check(r.rootSaveMessages(<h.Root>[v], buf, h.TableReport()) == m, 'save');
  final padded = Uint8List(m + 1);
  padded.setRange(0, m, buf);
  final padRep = h.TableReport();
  final padCount = r.rootLoadMessages(<h.Root>[h.Root()], padded, vocab, padRep);
  check(padCount == 1 && padRep.malformed && !padRep.refused,
      'bytes after the pad: count=$padCount malformed=${padRep.malformed}');
  // "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does
  // not know is a REFUSAL by name and never damage."
  for (final form in [1, 6]) {
    final formRep = h.TableReport();
    final formCount = r.rootLoadMessages(<h.Root>[h.Root()], Uint8List.fromList([form]), vocab, formRep);
    check(formCount == -1 && formRep.refused && formRep.reason == 'newer_form' && !formRep.malformed,
        'form $form: count=$formCount reason=${formRep.reason}');
  }
  // "a reader handed a form-'2' wire where a file was expected refuses by name."
  final fileRep = h.TableReport();
  final fileOk = h.announceRead(h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0))),
      Uint8List.fromList([2, 0, 0, 0]), fileRep);
  check(!fileOk && fileRep.refused && fileRep.reason == 'message_form_as_file' && !fileRep.malformed,
      'a form-2 batch as a file: reason=${fileRep.reason}');
`

// TestMessageFormFloatScalarsCrossTheBitLaneDart pins the message form's float
// scalar payload (docs/SPEC-TABLES.md §3.3): "A FLOAT CROSSES THE BIT LANE AS
// ITS IEEE-754 BITS" — float32 as its 32 bits, float64 as its 64, written from
// and read back into a double through a little-endian ByteData view, and never
// widened. It was the scalar-defaults/dart/valid-data red: the emitter handed a
// `double` to the int-only `w.put(value.b, 32)` and read `r.bits.get(32)`
// straight into a double, which does not compile. This test is pure Go over the
// generated text, so it is red at BASE on every machine, toolchain or not.
func TestMessageFormFloatScalarsCrossTheBitLaneDart(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, `package probe

enum Grade
{
    Bronze
    Silver
    Gold
}

fixed table T
{
    a int32 = 7
    b float32 = 0.0
    d float64 = 0.0
    g Grade = Silver
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	src := all.String()

	// The bits, both ways, at each kind's own width (docs/SPEC-TABLES.md §3.3).
	want := []string{
		"final f = value.b;",
		"w.put(TableBitWriter._float32Words[0], 32);",
		"final f = value.d;",
		"w.put(TableBitWriter._float64Words[0], 64);",
		"value.b = TableBitWriter._float32Cell[0];",
		"TableBitWriter._float64Words[0] = b;",
		"value.d = TableBitWriter._float64Cell[0];",
		// a NaN or an infinity is refused both ways (docs/SPEC-TABLES.md §3.3)
		"if (!f.isFinite) return false;",
		"if ((b & 0x7f800000) == 0x7f800000) return r.report.RefuseNonFiniteFloat(",
		"if ((b & 0x7ff0000000000000) == 0x7ff0000000000000) return r.report.RefuseNonFiniteFloat(",
	}
	for _, w := range want {
		if !strings.Contains(src, w) {
			t.Fatalf("the message form's float scalar payload does not cross the bit lane through %q (docs/SPEC-TABLES.md §3.3)", w)
		}
	}

	// Never a double in the int bit lane: the exact red the probe hit. The
	// widened pair pins that a float32 is never widened to 64 bits and that a
	// float64 is never truncated to 32 (docs/SPEC-TABLES.md §3.3, kind 10's
	// message width is 32).
	for _, bad := range []string{
		"w.put(value.b, 32)",
		"w.put(value.d, 64)",
		"w.put(value.b, 64)",
		"w.put(value.d, 32)",
		"value.b = r.bits.get(32)",
		"value.d = r.bits.get(64)",
	} {
		if strings.Contains(src, bad) {
			t.Fatalf("the message form hands a float to the int bit lane: %q (docs/SPEC-TABLES.md §3.3)", bad)
		}
	}
}

// fixedClosureUnions answers every union a table of the unit's fixed closure
// names, by name, and whether its arms include a TABLE (docs/SPEC-TABLES.md
// §2.6): a union with a table arm lives in ir.Unit.TableUnions, beside Decls,
// so the packet emitter never sees it and the fixed form is its only home.
func fixedClosureUnions(u *ir.Unit) map[string]bool {
	seen := map[string]bool{}
	var closure []*ir.Struct
	for _, f := range u.Files {
		for _, st := range fixedRoots(f.Tables) {
			fixedCollectTypes(st, seen, &closure)
		}
	}
	out := map[string]bool{}
	for _, st := range closure {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			un, ok := f.Type.Ref.(*ir.Union)
			if !ok {
				continue
			}
			tableArm := false
			for _, v := range un.Variants {
				if v.F == nil || v.F.Type.Kind != ir.TNamed {
					continue
				}
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && s.IsTable {
					tableArm = true
				}
			}
			out[un.Name] = out[un.Name] || tableArm
		}
	}
	return out
}

// TestFixedHomeDefinesItsTableArmUnionsDart pins that the fixed home of every
// manifest `message` unit defines each union class it references
// (docs/SPEC-TABLES.md §2.6, §3.4). A union whose arms are TABLES is a
// table-closure construct the packet emitter never sees, so if the fixed form
// does not claim it as local, nothing defines the class the home names and the
// library does not compile — which kept TestMessageFormBatchDart from running
// the backend_conn rows. Pure Go over the generated text: no toolchain.
func TestFixedHomeDefinesItsTableArmUnionsDart(t *testing.T) {
	t.Parallel()
	units := map[string]string{}
	for _, row := range batchManifest(t) {
		units[row.unit] = row.path
	}
	tableArm := 0
	for unit, path := range units {
		u := batchUnit(t, path)
		unions := fixedClosureUnions(u)
		if len(unions) == 0 {
			continue
		}
		files, err := dart.Generate(u)
		if err != nil {
			t.Fatalf("%s: generate packet: %v", unit, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: generate table: %v", unit, err)
		}
		maps.Copy(files, tables)
		var all strings.Builder
		for name, data := range files {
			if !strings.HasSuffix(name, ".dart") {
				continue
			}
			all.Write(data)
			all.WriteString("\n")
		}
		home := string(files[runtimeHome(u)+"Fixed.dart"])
		if home == "" {
			t.Fatalf("%s: no fixed home %sFixed.dart was generated", unit, runtimeHome(u))
		}
		for name, arm := range unions {
			if arm {
				tableArm++
			}
			if !regexp.MustCompile(`\b` + name + `\b`).MatchString(home) {
				continue
			}
			def := regexp.MustCompile(`(?m)^(?:abstract |sealed |base |final )*class ` + name + `\s*\{`)
			// Dart files are separate libraries: a definition in a sibling
			// packet file cannot satisfy an unqualified type in this fixed home.
			// A table-arm union must therefore be defined in the home itself.
			if arm && !def.MatchString(home) {
				t.Errorf("%s: the fixed home %sFixed.dart references table-arm union %s but does not define its class locally (docs/SPEC-TABLES.md §2.6, §3.4)",
					unit, runtimeHome(u), name)
				continue
			}
			if !def.MatchString(all.String()) {
				t.Errorf("%s: the fixed home %sFixed.dart references union %s (table arm: %v) but no generated library defines class %s (docs/SPEC-TABLES.md §2.6, §3.4)",
					unit, runtimeHome(u), name, arm, name)
			}
		}
	}
	if tableArm == 0 {
		t.Fatal("no manifest message unit names a table-arm union, so this test pins nothing (backenddemo's Payload was the one)")
	}
}

// TestMessageFormNestedDefaultElidesDart pins §3.3's elision of a nested
// table/type: "an all-default nested table" is not written at all. A fixed
// table holding a by-value type whose fields are all their defaults writes the
// SAME body as a table with no such field. Before this test the emitter gated
// the nested field on `value.child != 0`, comparing a record to an int: the
// strict Dart analyzer's unrelated_type_equality_checks named it, and the field
// always rode. The comparison is red here because Root's batch is then one
// reference plus a child body larger than Bare's.
func TestMessageFormNestedDefaultElidesDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

type Child
{
    x int32
}

fixed table Root
{
    child Child
    n     int32
}

fixed table Bare
{
    n int32
}
`)
	out, err := runBatchProbe(t, u, "Root", nestedDefaultElidesBody)
	if err != nil {
		t.Fatalf("a nested table's default elision did not run green: %v\n%s", err, out)
	}
}

const nestedDefaultElidesBody = `
  final root = h.Root();
  h.initRoot(root);
  final bare = h.Bare();
  h.initBare(bare);
  final rootMeasure = r.rootMeasureMessages(<h.Root>[root], h.TableReport());
  final bareMeasure = r.bareMeasureMessages(<h.Bare>[bare], h.TableReport());
  check(rootMeasure == bareMeasure,
      'an all-default nested Child must elide: Root=$rootMeasure Bare=$bareMeasure');

  root.child.x = 7;
  final liveMeasure = r.rootMeasureMessages(<h.Root>[root], h.TableReport());
  check(liveMeasure > rootMeasure, 'a set nested field must ride: empty=$rootMeasure live=$liveMeasure');

  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final buffer = Uint8List(liveMeasure);
  check(r.rootSaveMessages(<h.Root>[root], buffer, h.TableReport()) == liveMeasure, 'save');
  final back = <h.Root>[h.Root()];
  final rep = h.TableReport();
  final count = r.rootLoadMessages(back, buffer, vocab, rep);
  check(count == 1 && !rep.refused && !rep.malformed, 'load count=$count reason=${rep.reason}');
  check(back[0].child.x == 7, 'the nested value did not round trip: ${back[0].child.x}');
`

// TestMessageFormSkipDepthDart is the message_skip_depth boundary on the
// generated Dart reader (docs/SPEC-TABLES.md, limit value 128). The typed body
// reads references at tableMessageRefBits, and skip reads vocabulary.refBits,
// so the probe sets those equal. Slots 1..3 are Root's fields; the unknown
// entry rides at slot 8. Verdicts are the spec's. The above-limit rows are
// complete zero-padded batches whose only fault is depth past 128.
func TestMessageFormSkipDepthDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

fixed table Root {
    a int32
    b bool
    c uint8
}
`)
	out, err := runBatchProbe(t, u, "Root", dartSkipDepthBody)
	if err != nil {
		t.Fatalf("skip_depth_above_128: %v\n%s", err, out)
	}
}

const dartSkipDepthBody = `
  // Slot 8 is the Root name, not a field of this root (a, b, c are 1..3).
  // A reference width below 4 cannot spell it.
  check(h.tableMessageRefBits >= 4, 'skip depth slot needs four reference bits, got ${h.tableMessageRefBits}');
  final width = h.tableMessageRefBits;
  final sig14 = Uint8List(12);
  sig14[9] = 1;
  sig14[10] = 1;
  sig14[11] = 13;

  h.TableVocabulary vocabAt(int kind) {
    final entries = List<h.TableMessageEntry>.generate(8, (i) => h.TableMessageEntry(0, 0));
    entries[7] = h.TableMessageEntry(1, kind, Uint8List(9));
    final vocab = h.TableVocabulary(entries: entries);
    vocab.count = 8;
    vocab.refBits = width;
    vocab.announced = true;
    return vocab;
  }

  final body = vocabAt(13);
  final arm = vocabAt(15);
  final arrayEntries = List<h.TableMessageEntry>.generate(8, (i) => h.TableMessageEntry(0, 0));
  arrayEntries[6] = h.TableMessageEntry(1, 13, Uint8List(9));
  arrayEntries[7] = h.TableMessageEntry(2, 14, sig14);
  final array = h.TableVocabulary(entries: arrayEntries);
  array.count = 8;
  array.refBits = width;
  array.announced = true;

  h.TableVocabulary keyedVocab(int elemKind) {
    final sig16 = Uint8List(12);
    sig16[9] = (1 << width) - 1;
    sig16[10] = elemKind;
    final entries = List<h.TableMessageEntry>.generate(8, (i) => h.TableMessageEntry(0, 0));
    entries[6] = h.TableMessageEntry(1, 13, Uint8List(9));
    entries[7] = h.TableMessageEntry(2, 16, sig16);
    final vocab = h.TableVocabulary(entries: entries);
    vocab.count = 8;
    vocab.refBits = width;
    vocab.announced = true;
    return vocab;
  }

  final keyed6 = keyedVocab(6);
  final keyed32 = keyedVocab(32);
  final keyed13 = keyedVocab(13);

  Uint8List pack(int nlead, int lead, int nmid, int mid, int ntail) {
    final n = nlead + nmid + ntail;
    final bits = 16 + n * width;
    final buf = Uint8List((bits + 7) >> 3);
    buf[0] = 2;
    buf[1] = 0;
    var at = 16;
    for (var i = 0; i < n; i++) {
      final slot = i < nlead ? lead : (i < nlead + nmid ? mid : 0);
      for (var b = 0; b < width; b++) {
        if (((slot >> b) & 1) != 0) {
          buf[at >> 3] |= 1 << (at & 7);
        }
        at++;
      }
    }
    return buf;
  }

  void expectOk(String row, h.TableVocabulary vocab, Uint8List buf) {
    final value = h.Root();
    h.initRoot(value);
    final rep = h.TableReport();
    final count = r.rootLoadMessages(<h.Root>[value], buf, vocab, rep);
    check(count == 1 && !rep.malformed && !rep.refused && rep.unknown == 1 && value.a == 0,
        '$row: count=$count malformed=${rep.malformed} unknown=${rep.unknown} a=${value.a}');
  }

  void expectBad(String row, h.TableVocabulary vocab, Uint8List buf) {
    final value = h.Root();
    h.initRoot(value);
    final rep = h.TableReport();
    final count = r.rootLoadMessages(<h.Root>[value], buf, vocab, rep);
    check(count == 0 && rep.malformed, '$row: count=$count malformed=${rep.malformed}');
  }

  expectOk('skip_depth_body_below_128', body, pack(127, 8, 0, 0, 128));
  final at = pack(128, 8, 0, 0, 129);
  expectOk('skip_depth_body_at_128', body, at);
  expectOk('skip_depth_continuation', body, at);
  final cut = pack(128, 8, 0, 0, 128);
  check(cut.length * 8 == 16 + 256 * width, 'skip_depth_truncation alignment');
  expectBad('skip_depth_truncation', body, cut);
  expectBad('skip_depth_above_128 body', body, pack(129, 8, 0, 0, 130));
  expectOk('skip_depth_union_below_128', arm, pack(128, 8, 0, 0, 2));
  expectOk('skip_depth_union_at_128', arm, pack(129, 8, 0, 0, 2));
  expectBad('skip_depth_above_128 union', arm, pack(130, 8, 0, 0, 2));
  expectOk('skip_depth_array_below_128', array, pack(125, 7, 1, 8, 127));
  expectOk('skip_depth_array_at_128', array, pack(126, 7, 1, 8, 128));
  expectBad('skip_depth_above_128 array', array, pack(127, 7, 1, 8, 129));
  expectOk('skip_depth_keyed_fixed_at_128', keyed6, pack(128, 7, 2, 8, 153));
  expectOk('skip_depth_keyed_nopayload_at_128', keyed32, pack(128, 7, 2, 8, 137));
  expectBad('skip_depth_above_128 keyed', keyed13, pack(127, 7, 2, 8, 137));
`
