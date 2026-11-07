package javatable

// messageform_announce — the Java leg's MESSAGE FORM announcement surface
// (docs/SPEC-TABLES.md §3.3, form byte 2), which the leg carries none of today.
// One subtest per clause of the card:
//
//   2.1 Announce and AnnounceMeasure are a compile-time constant byte array and
//       its length, equal byte for byte to the manifest's announcement file for
//       the units backenddemo, vocabdemo and vocab9demo.
//   2.2 AnnounceRead accepts each of those files and fills TableVocabulary with
//       the entries the page names, the build version kept.
//   2.3 the refusals second_announcement (and refusal is terminal) and
//       vocabulary_too_large (both bounds), and the two strict checks answered
//       `malformed`.
//
// The rows are the reference's own bytes, pinned by
// testdata/conformance/tables/MANIFEST.txt's `connection` lines.

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// announceRow is one `connection` line: the unit, and the announcement file its
// generated announce must equal byte for byte.
type announceRow struct {
	unit   string
	dir    string
	golden string
}

func announceRows(t *testing.T) []announceRow {
	t.Helper()
	return []announceRow{
		{unit: "backenddemo", dir: "tables/backend", golden: "testdata/wire/tables/backend_conn.bin"},
		{unit: "vocabdemo", dir: "tables/vocab", golden: "testdata/wire/tables/vocab_conn.bin"},
		{unit: "vocab9demo", dir: "tables/vocab9", golden: "testdata/wire/tables/vocab9_conn.bin"},
	}
}

// loadUnitDir parses one directory's *.schema files into one unit.
func loadUnitDir(t *testing.T, root, dir string) *ir.Unit {
	t.Helper()
	abs := filepath.Join(root, dir)
	entries, err := os.ReadDir(abs)
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") || len(e.Name()) <= len(".schema") {
			continue
		}
		name := filepath.Join(abs, e.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(name, data)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		files = append(files, check.SourceFile{Path: name, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: data, AST: f})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}

// runMessageJava compiles one unit's generated TableVocabulary beside a driver
// and runs the driver, returning its combined stdout.
func runMessageJava(t *testing.T, u *ir.Unit, main string) string {
	t.Helper()
	javac, javaBin := javaTools(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := files[messageAnnouncementClass+".java"]
	if !ok {
		t.Fatalf("%s: no %s.java emitted", u.Package, messageAnnouncementClass)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, messageAnnouncementClass+".java"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := "package " + u.Package + ";\n\n" + driverPrologue + "\n" + main + "}\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Driver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-d", classes,
		filepath.Join(srcDir, messageAnnouncementClass+".java"), filepath.Join(srcDir, "Driver.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	// The JVM prints a `Picked up JAVA_TOOL_OPTIONS: ...` banner on stderr
	// when the variable is set; it is not part of the driver's output.
	var lines []string
	for l := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(l, "Picked up JAVA_TOOL_OPTIONS") {
			continue
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// driverPrologue is shared by every driver: the class header and helpers.
const driverPrologue = `public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static String hex(byte[] b) {
        final StringBuilder out = new StringBuilder();
        for (final byte v : b) { out.append(String.format("%02x", v)); }
        return out.toString();
    }

    static byte[] fromHex(String s) {
        final byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }
`

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	var s strings.Builder
	for _, v := range b {
		s.WriteByte(digits[v>>4])
		s.WriteByte(digits[v&0xf])
	}
	return s.String()
}

// TestMessageFormAnnounceJava pins the announcement surface (docs/SPEC-TABLES.md
// §3.3). Each subtest compiles the generated TableVocabulary and a driver, runs
// it, and compares the result to the reference.
func TestMessageFormAnnounceJava(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("2.1", func(t *testing.T) {
		t.Parallel()
		for _, row := range announceRows(t) {
			t.Run(row.unit, func(t *testing.T) {
				t.Parallel()
				// "Announce and AnnounceMeasure are a compile-time constant byte
				// array and its length, equal byte for byte to the manifest's
				// announcement file." (§3.3)
				u := loadUnitDir(t, root, row.dir)
				want, err := os.ReadFile(filepath.Join(root, row.golden))
				if err != nil {
					t.Fatal(err)
				}
				out := runMessageJava(t, u, announceMain)
				if got := strings.TrimSpace(out); got != hexOf(want) {
					t.Fatalf("%s: announce = %s, want %s", row.unit, got, hexOf(want))
				}
			})
		}
	})

	t.Run("2.2", func(t *testing.T) {
		t.Parallel()
		for _, row := range announceRows(t) {
			t.Run(row.unit, func(t *testing.T) {
				t.Parallel()
				// "AnnounceRead accepts each of those files and fills
				// TableVocabulary with the entries the page names, the build
				// version kept." (§3.3)
				u := loadUnitDir(t, root, row.dir)
				vocab := ir.TableVocabulary(u)
				out := runMessageJava(t, u, announceReadMain(u, vocab))
				if strings.TrimSpace(out) != "ok" {
					t.Fatalf("%s: %s", row.unit, out)
				}
			})
		}
	})

	t.Run("2.3", func(t *testing.T) {
		t.Parallel()
		u := loadUnitDir(t, root, "tables/backend")
		out := runMessageJava(t, u, refusalsMain(u))
		if strings.TrimSpace(out) != "ok" {
			t.Fatalf("refusals: %s", out)
		}
	})
}

const announceMain = `    public static void main(String[] args) {
        final byte[] buffer = new byte[TableVocabulary.announceMeasure()];
        final int n = TableVocabulary.announce(buffer);
        if (n != buffer.length) { fail("length " + n); }
        System.out.println(hex(buffer));
    }
`

// announceReadMain drives the AnnounceRead clause: it announces, reads the
// announcement back into a resolved vocabulary, and asserts the count, the
// build version, the reference width and every entry's id, kind and shape —
// an entry is a triple of id, kind and shape (docs/SPEC-TABLES.md §3.3).
//
// The expected facts ride in data arrays and one compact loop, not one
// assertion per entry, so the driver stays under javac's 64 KiB method limit
// for the wide vocabularies (`vocab9demo` carries hundreds of entries).
func announceReadMain(u *ir.Unit, vocab []ir.TableVocabularyEntry) string {
	a := buildShapeArrays(vocab)
	refBits := ir.TableMessageRefBits(len(vocab))
	return fmt.Sprintf(`    public static void main(String[] args) {
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }
        final TableVocabulary v = new TableVocabulary();
        v.reset(%d);
        final TableVocabulary.Report report = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(v, ann, report)) { fail("announceRead refused"); }
        if (report.malformed || report.refused) { fail("report " + report.reason); }
        if (v.count != %d) { fail("count " + v.count); }
        if (Long.compareUnsigned(v.buildVersion, 0x%016xL) != 0) { fail("version " + Long.toUnsignedString(v.buildVersion, 16)); }
        if (v.refBits != %d) { fail("refBits " + v.refBits); }
        final long[] wantId = {%s};
        final int[] wantKind = {%s};
        final int[] wantPacking = {%s};
        final long[] wantBits = {%s};
        final long[] wantBaseLo = {%s};
        final long[] wantBaseHi = {%s};
        final long[] wantMin = {%s};
        final long[] wantMax = {%s};
        final int[] wantQMin = {%s};
        final int[] wantQMax = {%s};
        final int[] wantQRes = {%s};
        final int[] wantQDelta = {%s};
        final long[] wantSteps = {%s};
        final int[] wantElemKind = {%s};
        final int[] wantElemPacking = {%s};
        final long[] wantElemBits = {%s};
        final long[] wantElemBaseLo = {%s};
        final long[] wantElemBaseHi = {%s};
        if (v.count != wantId.length) { fail("entry count " + v.count); }
        for (int i = 0; i < v.count; i++) {
            if (Long.compareUnsigned(v.entries[i].id, wantId[i]) != 0) {
                fail("id[" + i + "] " + Long.toUnsignedString(v.entries[i].id, 16));
            }
            final TableVocabulary.Shape s = v.entries[i].shape;
            if (v.entries[i].kind != wantKind[i]) { fail("kind[" + i + "] " + v.entries[i].kind); }
            if (s.kind != wantKind[i]) { fail("shape.kind[" + i + "]"); }
            if (s.packing != wantPacking[i]) { fail("shape.packing[" + i + "]"); }
            if (s.bits != wantBits[i]) { fail("shape.bits[" + i + "]"); }
            if (s.baseLo != wantBaseLo[i]) { fail("shape.baseLo[" + i + "]"); }
            if (s.baseHi != wantBaseHi[i]) { fail("shape.baseHi[" + i + "]"); }
            if (s.min != wantMin[i]) { fail("shape.min[" + i + "]"); }
            if (s.max != wantMax[i]) { fail("shape.max[" + i + "]"); }
            if (Float.floatToIntBits(s.qMin) != wantQMin[i]) { fail("shape.qMin[" + i + "]"); }
            if (Float.floatToIntBits(s.qMax) != wantQMax[i]) { fail("shape.qMax[" + i + "]"); }
            if (Float.floatToIntBits(s.qRes) != wantQRes[i]) { fail("shape.qRes[" + i + "]"); }
            if (Float.floatToIntBits(s.qDelta) != wantQDelta[i]) { fail("shape.qDelta[" + i + "]"); }
            if (s.steps != wantSteps[i]) { fail("shape.steps[" + i + "]"); }
            if (wantElemKind[i] < 0) {
                if (v.entries[i].element != null) { fail("element[" + i + "] present"); }
            } else {
                final TableVocabulary.Shape el = v.entries[i].element;
                if (el == null) { fail("element[" + i + "] missing"); }
                else {
                    if (el.kind != wantElemKind[i]) { fail("element.kind[" + i + "]"); }
                    if (el.packing != wantElemPacking[i]) { fail("element.packing[" + i + "]"); }
                    if (el.bits != wantElemBits[i]) { fail("element.bits[" + i + "]"); }
                    if (el.baseLo != wantElemBaseLo[i]) { fail("element.baseLo[" + i + "]"); }
                    if (el.baseHi != wantElemBaseHi[i]) { fail("element.baseHi[" + i + "]"); }
                }
            }
        }
        System.out.println("ok");
    }
`, len(vocab)+1, len(vocab), ir.BuildVersion(u), refBits,
		a.ids, a.kinds, a.packing, a.bits, a.baseLo, a.baseHi, a.min, a.max,
		a.qMin, a.qMax, a.qRes, a.qDelta, a.steps, a.elemKind, a.elemPacking,
		a.elemBits, a.elemBaseLo, a.elemBaseHi)
}

// shapeFacts is one entry's expected shape as the golden announces it
// (docs/SPEC-TABLES.md §3.3): the width and range facts a reader must derive,
// which the clause 2.2 driver asserts beside the id and the kind.
type shapeFacts struct {
	kind                     int
	packing                  int
	bits                     int64
	baseLo, baseHi           uint64
	min, max                 int64
	qMin, qMax, qRes, qDelta uint32
	steps                    int64
	elem                     *shapeFacts
}

// shapeArrays is every expected entry fact as a Java array literal, one array
// per fact, index-aligned with the vocabulary's entries.
type shapeArrays struct {
	ids, kinds                                string
	packing, bits, baseLo, baseHi             string
	min, max, qMin, qMax, qRes, qDelta, steps string
	elemKind, elemPacking                     string
	elemBits, elemBaseLo, elemBaseHi          string
}

// shapeBaseLoHi is a ranged base's two 64-bit halves, the wide kinds' raw
// sixteen-byte base (docs/SPEC-TABLES.md §3.3).
func shapeBaseLoHi(s ir.TableMessageShape) (lo, hi uint64) {
	if s.Base == nil {
		return 0, 0
	}
	v := new(big.Int).Set(s.Base)
	v.And(v, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)))
	lo = v.Uint64()
	hi = new(big.Int).Rsh(new(big.Int).Set(v), 64).Uint64()
	return lo, hi
}

// messageShapeFacts derives the facts a reader must derive for one kind and
// shape, mirroring the emitted shapeRead (docs/SPEC-TABLES.md §3.3's shape
// table): packing and bits for the numeric kinds, base for a ranged one, the
// quantized triple's derivation for a compressed f32, and min/max for the
// length kinds.
func messageShapeFacts(kind uint8, s ir.TableMessageShape) shapeFacts {
	f := shapeFacts{kind: int(kind), packing: int(s.Packing)}
	if (kind >= ir.TableKindI8 && kind <= ir.TableKindF32) ||
		(kind >= ir.TableKindI128 && kind <= ir.TableKindUFixed128) {
		switch s.Packing {
		case ir.TableMessageRaw:
			f.bits = int64(ir.TableKindWidth(int(kind))) * 8
		case ir.TableMessageRanged:
			f.bits = s.Bits
			f.baseLo, f.baseHi = shapeBaseLoHi(s)
		case ir.TableMessageQuantized:
			count, delta, _ := ir.TableMessageQuantization(s)
			f.qMin = math.Float32bits(s.QMin)
			f.qMax = math.Float32bits(s.QMax)
			f.qRes = math.Float32bits(s.QRes)
			f.qDelta = math.Float32bits(delta)
			f.steps = int64(count)
			f.bits = int64(bits.Len32(count))
		}
		return f
	}
	switch kind {
	case ir.TableKindString, ir.TableKindWstring:
		f.max = s.Max
	case ir.TableKindArray:
		f.min = s.Min
		f.max = s.Max
		f.elem = messageElemFacts(s)
	case ir.TableKindKeyed:
		f.max = s.Max
		f.elem = messageElemFacts(s)
	}
	return f
}

func messageElemFacts(s ir.TableMessageShape) *shapeFacts {
	if s.Inner == nil {
		return nil
	}
	f := messageShapeFacts(s.Elem, *s.Inner)
	return &f
}

// buildShapeArrays spells each expected fact as a Java array literal, so
// clause 2.2 bites the reader's shapes as well as its ids and kinds
// (docs/SPEC-TABLES.md §3.3, "AN ENTRY IS A TRIPLE").
func buildShapeArrays(vocab []ir.TableVocabularyEntry) shapeArrays {
	var b [18]strings.Builder
	sep := func(i, col int) {
		if i > 0 {
			b[col].WriteString(", ")
		}
	}
	for i, e := range vocab {
		f := messageShapeFacts(e.Kind, e.Shape)
		sep(i, 0)
		fmt.Fprintf(&b[0], "0x%016xL", e.Id)
		sep(i, 1)
		fmt.Fprintf(&b[1], "%d", e.Kind)
		sep(i, 2)
		fmt.Fprintf(&b[2], "%d", f.packing)
		sep(i, 3)
		fmt.Fprintf(&b[3], "%dL", f.bits)
		sep(i, 4)
		fmt.Fprintf(&b[4], "0x%016xL", f.baseLo)
		sep(i, 5)
		fmt.Fprintf(&b[5], "0x%016xL", f.baseHi)
		sep(i, 6)
		fmt.Fprintf(&b[6], "%dL", f.min)
		sep(i, 7)
		fmt.Fprintf(&b[7], "%dL", f.max)
		sep(i, 8)
		fmt.Fprintf(&b[8], "0x%08x", f.qMin)
		sep(i, 9)
		fmt.Fprintf(&b[9], "0x%08x", f.qMax)
		sep(i, 10)
		fmt.Fprintf(&b[10], "0x%08x", f.qRes)
		sep(i, 11)
		fmt.Fprintf(&b[11], "0x%08x", f.qDelta)
		sep(i, 12)
		fmt.Fprintf(&b[12], "%dL", f.steps)
		if f.elem == nil {
			sep(i, 13)
			b[13].WriteString("-1")
			sep(i, 14)
			b[14].WriteString("0")
			sep(i, 15)
			b[15].WriteString("0L")
			sep(i, 16)
			b[16].WriteString("0L")
			sep(i, 17)
			b[17].WriteString("0L")
		} else {
			sep(i, 13)
			fmt.Fprintf(&b[13], "%d", f.elem.kind)
			sep(i, 14)
			fmt.Fprintf(&b[14], "%d", f.elem.packing)
			sep(i, 15)
			fmt.Fprintf(&b[15], "%dL", f.elem.bits)
			sep(i, 16)
			fmt.Fprintf(&b[16], "0x%016xL", f.elem.baseLo)
			sep(i, 17)
			fmt.Fprintf(&b[17], "0x%016xL", f.elem.baseHi)
		}
	}
	return shapeArrays{
		ids: b[0].String(), kinds: b[1].String(), packing: b[2].String(),
		bits: b[3].String(), baseLo: b[4].String(), baseHi: b[5].String(),
		min: b[6].String(), max: b[7].String(), qMin: b[8].String(),
		qMax: b[9].String(), qRes: b[10].String(), qDelta: b[11].String(),
		steps: b[12].String(), elemKind: b[13].String(), elemPacking: b[14].String(),
		elemBits: b[15].String(), elemBaseLo: b[16].String(), elemBaseHi: b[17].String(),
	}
}

// The refusal and strict-check forgeries, built the reference's way: an
// announcement is an ordinary form-1 file whose body carries two fields and
// whose trailer is those two reserved ids (docs/SPEC-TABLES.md §3.3).
func leb(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		out = append(out, c)
		if v == 0 {
			return out
		}
	}
}

func versionField(version uint64) []byte {
	out := []byte{1, 9}
	return binary.LittleEndian.AppendUint64(out, version)
}

func vocabField(elem uint8, vocab []byte) []byte {
	sub := append([]byte{elem}, leb(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	field := []byte{2, 14}
	field = append(field, leb(uint64(len(sub)))...)
	return append(field, sub...)
}

func vocabFieldBadKind(kind uint8) []byte {
	sub := append([]byte{6}, leb(0)...)
	field := []byte{2, kind}
	field = append(field, leb(uint64(len(sub)))...)
	return append(field, sub...)
}

func announcement(fields []byte) []byte {
	out := append([]byte{1}, fields...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// badElemAnnouncement patches the vocabulary field's element kind to 9, walking
// the body the way the reference's framing pre-walk does.
func badElemAnnouncement(ann []byte) []byte {
	bad := append([]byte(nil), ann...)
	off := 1
	for off < len(bad) {
		ref := uint64(0)
		shift := 0
		for {
			b := bad[off]
			off++
			ref |= uint64(b&0x7f) << shift
			shift += 7
			if b&0x80 == 0 {
				break
			}
		}
		if ref == 0 {
			break
		}
		kind := bad[off]
		off++
		if kind == 14 {
			for bad[off]&0x80 != 0 {
				off++
			}
			off++
			bad[off] = 9
			break
		}
		if kind == 9 {
			off += 8
		}
	}
	return bad
}

// refusalsMain drives the clause 2.3 refusals and the two strict checks.
func refusalsMain(u *ir.Unit) string {
	const version = 0x1122334455667788
	twice := append(versionField(version), versionField(version)...)
	twice = append(twice, vocabField(6, nil)...)
	short := append([]byte{1, 9}, 0, 0, 0, 0)
	strict := []struct {
		name string
		data []byte
	}{
		{"build version missing", announcement(vocabField(6, nil))},
		{"build version twice", announcement(twice)},
		{"build version not kind 9", announcement(append([]byte{1, 5, 0, 0, 0, 0, 0, 0, 0, 0}, vocabField(6, nil)...))},
		{"build version fewer than eight bytes", announcement(short)},
		{"vocabulary missing", announcement(versionField(version))},
		{"vocabulary not kind 14", announcement(append(versionField(version), vocabFieldBadKind(13)...))},
		{"vocabulary element not kind 6", badElemAnnouncement(ir.TableAnnouncement(u))},
	}
	var cases strings.Builder
	cases.WriteString("        final String[] strictHex = {\n")
	for _, tc := range strict {
		fmt.Fprintf(&cases, "            \"%s\",\n", hexOf(tc.data))
	}
	cases.WriteString("        };\n")
	return fmt.Sprintf(`    public static void main(String[] args) {
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }

        // "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does not
        // replace or amend anything." and "REFUSAL IS TERMINAL" (§3.3).
        final TableVocabulary first = new TableVocabulary();
        first.reset(%d);
        final TableVocabulary.Report r1 = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(first, ann, r1)) { fail("first announce"); }
        final int before = first.count;
        final TableVocabulary.Report r2 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(first, ann, r2) || !r2.refused || !"second_announcement".equals(r2.reason) || r2.malformed) {
            fail("second announcement was not refused by name");
        }
        if (first.count != before) { fail("a second announcement replaced the vocabulary"); }
        // A refused FIRST announcement is terminal: an endpoint whose announcement was
        // refused holds no retry on that connection (§3.3).
        final TableVocabulary refused = new TableVocabulary();
        refused.reset(%d);
        final TableVocabulary.Report rr = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(refused, new byte[]{9, 9, 9}, rr) || !rr.refused || !"newer_form".equals(rr.reason) || refused.announced) {
            fail("a refused first announcement was not refused");
        }
        final TableVocabulary.Report later = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(refused, ann, later) || !"second_announcement".equals(later.reason) || refused.announced) {
            fail("an announcement after a refused first is not second_announcement");
        }

        // "vocabulary_too_large covers both bounds, the entry count and the
        // vocabulary's bytes." (§3.3)
        final TableVocabulary smallEntries = new TableVocabulary();
        smallEntries.reset(1);
        final TableVocabulary.Report e1 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(smallEntries, ann, e1) || !e1.refused || !"vocabulary_too_large".equals(e1.reason) || e1.malformed || smallEntries.announced) {
            fail("entry count bound was not refused by name");
        }
        // The entry bound is maxEntries too, not the storage's capacity alone:
        // a receiver that keeps room for every entry and sets maxEntries = 1
        // refuses an announcement of more than one (the hostile corpus's
        // announce-entries-too-large; the Go engine's Vocabulary.MaxEntries).
        final TableVocabulary narrowEntries = new TableVocabulary();
        narrowEntries.reset(%d);
        narrowEntries.maxEntries = 1;
        final TableVocabulary.Report e3 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(narrowEntries, ann, e3) || !e3.refused || !"vocabulary_too_large".equals(e3.reason) || e3.malformed || narrowEntries.announced) {
            fail("maxEntries bound was not refused by name");
        }
        final TableVocabulary smallBytes = new TableVocabulary();
        smallBytes.reset(%d);
        smallBytes.maxBytes = 4;
        final TableVocabulary.Report e2 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(smallBytes, ann, e2) || !e2.refused || !"vocabulary_too_large".equals(e2.reason) || e2.malformed || smallBytes.announced) {
            fail("vocabulary byte bound was not refused by name");
        }

        // "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS ...
        // A FAILED STRICT CHECK IS MALFORMED, not a refusal." (§3.3)
        %s        for (final String h : strictHex) {
            final TableVocabulary v = new TableVocabulary();
            v.reset(%d);
            final TableVocabulary.Report report = new TableVocabulary.Report();
            if (TableVocabulary.announceRead(v, fromHex(h), report) || !report.malformed || report.refused || v.announced) {
                fail("a failed strict check was not malformed");
            }
        }
        // The build version is kept the moment it is read, refusal or not (§3.3).
        final TableVocabulary keep = new TableVocabulary();
        keep.reset(%d);
        final TableVocabulary.Report kr = new TableVocabulary.Report();
        final byte[] keeps = fromHex("%s");
        if (TableVocabulary.announceRead(keep, keeps, kr) || !kr.malformed || Long.compareUnsigned(keep.buildVersion, 0x%016xL) != 0 || keep.announced) {
            fail("the refused announcement did not keep the build version");
        }
        System.out.println("ok");
    }
`, len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, cases.String(), len(ir.TableVocabulary(u))+1, len(ir.TableVocabulary(u))+1, hexOf(announcement(append(versionField(version), 2, 13))), version)
}
