package darttable

// THE CORPUS ROUND TRIP ON THE DART LEG (docs/roadmap-evidence/fixed-dart.sexp
// dart/R27): every record of every file build/fixedform-corpus/manifest.txt
// names is loaded through this leg's own reader and rewritten through its own
// writer, and the rewrite is the reference's bytes (docs/FIXED-FORM-ALGORITHM.md
// §3.1: the write is a template and a store, and the cell stores what the
// record held; docs/SPEC-TABLES.md §3: no float is canonicalised).
//
// FOUR FILES ARE NOT IDENTITY ROWS, AND EACH SAYS WHY BY CITATION. Their bytes
// hold a value the reader's own declaration does not, so the standard asks the
// read to CLAMP it, or the file was written by an OLDER writer whose layout the
// rewrite (always in the reader's own layout) does not restore. For those the
// test asserts the bytes the standard says the rewrite is — derived from the
// reference's own files and the manifest, never from this leg's output — and
// the counters the read must make. Nothing is skipped: every manifest row is a
// named subtest, and a contract row naming a file the manifest lost fails.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedCorpusContract is one file whose rewrite the standard does not make
// byte-identical, with the citation that says so.
type fixedCorpusContract struct {
	reader  string   // the reading schema's base
	older   []string // the older writers' schema bases, the reader's lineage
	clamped int      // report.clamped the read must make, exactly
	cite    string   // the section that rules the bytes
	// expect builds the rewrite the standard asks for, from the reference's
	// own bytes and the manifest
	expect func(t *testing.T, corpus string, data []byte) []byte
}

var fixedCorpusContracts = map[string]fixedCorpusContract{
	// The C++ dump writes raw 1000 (62.5) into fixed(12,4) | min = -8, max = 7:
	// past the declaration's own max. "Out-of-range value: clamped to the
	// reader's declared bounds, counted. A fixed field clamps on the RAW
	// scale" (docs/SPEC-TABLES.md §4, the silent class), so the read lands
	// 7 << 4 = 112 with clamped == 1, as the C++ reference's own decode does,
	// and the rewrite carries 112 where the file carried 1000.
	"new_fixed_I_grow.bin": {
		reader: "VNEW_fixed_I_grow", clamped: 1,
		cite: "docs/SPEC-TABLES.md §4 (out-of-range value: a fixed field clamps on the raw scale, counted)",
		expect: func(t *testing.T, corpus string, data []byte) []byte {
			return fixedCorpusClampedCell(t, corpus, "new_fixed_I_grow.bin", "VNEW_fixed_I_grow", data, "v", 0, "r0.v")
		},
	},
	// The same rule per slot: vals[0] carries raw 1000 past [4]fixed(28,4)'s
	// max 7; the other three slots are zero and inside the range.
	"new_fixed_I_grow_element.bin": {
		reader: "VNEW_fixed_I_grow_element", clamped: 1,
		cite: "docs/SPEC-TABLES.md §4 (out-of-range value: a fixed field clamps on the raw scale, per element, counted)",
		expect: func(t *testing.T, corpus string, data []byte) []byte {
			return fixedCorpusClampedCell(t, corpus, "new_fixed_I_grow_element.bin", "VNEW_fixed_I_grow_element", data, "vals", 0, "r0.vals[0]")
		},
	},
	// An OLD writer's bytes with aim forged to 1.5, read by the NEW reader:
	// it lands WHOLE and uncounted, the bounds pass being the READER's
	// (docs/FIXED-FORM-VERSIONING-TESTS.md, the compressed-float hostile
	// pass, schema#1164 statement B). The rewrite is in the reader's own
	// layout, so it is the reference's NEW file with aim = the forged value.
	"hostile_cfloat_range_widen.bin": {
		reader: "VNEW_cfloat_range_widen", older: []string{"VOLD_cfloat_range_widen"}, clamped: 0,
		cite: "docs/FIXED-FORM-VERSIONING-TESTS.md (compressed-float hostile pass; schema#1164 B) and docs/SPEC-TABLES.md §3.4 (a record is stamped by its writer's layout)",
		expect: func(t *testing.T, corpus string, data []byte) []byte {
			at, forged := fixedCorpusForgedAim(t, corpus, "hostile_cfloat_range_widen.bin")
			return fixedCorpusNewWithAim(t, corpus, data, at, forged)
		},
	},
	// The same old file with aim forged to 5.0, past the reader's [-2, 2] as
	// well: it clamps to the READER's 2.0 and counts once (same citation).
	"past_cfloat_range_widen.bin": {
		reader: "VNEW_cfloat_range_widen", older: []string{"VOLD_cfloat_range_widen"}, clamped: 1,
		cite: "docs/FIXED-FORM-VERSIONING-TESTS.md (compressed-float hostile pass: past the reader's range clamps to the reader's bound) and docs/SPEC-TABLES.md §4",
		expect: func(t *testing.T, corpus string, data []byte) []byte {
			at, forged := fixedCorpusForgedAim(t, corpus, "past_cfloat_range_widen.bin")
			f := fixedCorpusField(t, "VNEW_cfloat_range_widen", "aim")
			if !f.HasFloatRange || float64(math.Float32frombits(forged)) <= f.FMax {
				t.Fatalf("past_cfloat_range_widen.bin: the forged aim %#08x is not past the reader's max %v", forged, f.FMax)
			}
			return fixedCorpusNewWithAim(t, corpus, data, at, math.Float32bits(float32(f.FMax)))
		},
	},
}

// TestFixedCorpusRoundTripsByteIdenticalDart is dart/R27: every manifest row,
// by name, through this leg's reader and writer.
func TestFixedCorpusRoundTripsByteIdenticalDart(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	dartBinary(t)
	rows := t12ManifestRows(t, corpus)
	for file := range fixedCorpusContracts {
		if _, ok := rows[file]; !ok {
			t.Errorf("contract row %s names a file the manifest does not carry", file)
		}
	}
	for _, file := range slices.Sorted(maps.Keys(rows)) {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			fixedCorpusRoundTrip(t, corpus, file, rows[file])
		})
	}
}

// fixedCorpusRoundTrip is the R27 probe for one corpus file: identity for an
// ordinary row, the cited bytes and counters for a contract row.
func fixedCorpusRoundTrip(t *testing.T, corpus, file string, records int) {
	t.Helper()
	path := filepath.Join(corpus, file)
	if leaf := fixedManifestNonFinite(t, corpus)[file]; leaf != "" {
		// THE REFERENCE STILL WRITES A NaN OR AN INFINITY HERE, and under the
		// finite rule of 2026-10-27 (docs/SPEC-TABLES.md §3.3) the file is a
		// MUST-REFUSE vector for this leg: every read refuses it by name and
		// there is nothing to write back. The manifest decides, so the
		// identity row comes back by itself the day the C++ card of the
		// finite-tables plan (tables-finite/cpp) turns the corpus finite.
		base := t12SchemaBase(file)
		if _, contract := fixedCorpusContracts[file]; contract || base == "" {
			t.Fatalf("R27: %s carries a non-finite %s and has no plain schema to refuse it under", file, leaf)
		}
		out := t12MustRefuse(t, t12Schema(t, base), path, leaf)
		if !strings.Contains(out, "ok") {
			t.Fatalf("R27 must-refuse read of %s printed no ok: %s", file, out)
		}
		return
	}
	c, ok := fixedCorpusContracts[file]
	if !ok {
		base := t12SchemaBase(file)
		if base == "" {
			t.Fatalf("R27: no schema known for corpus file %s", file)
		}
		out := t12RoundTrip(t, t12Schema(t, base), records, path)
		if !strings.Contains(out, "ok") {
			t.Fatalf("R27 round trip of %s printed no ok: %s", file, out)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := c.expect(t, corpus, data)
	lower := lowerFirst(versionRoot(t, versionSchema(t, c.reader)).Name)
	body := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == %[1]d, 'load returned $n, want %[1]d: ${why(report)}');
  check(report.refused == 0 && !report.malformed, 'a clean read: ${why(report)}');
  check(report.clamped == %[2]d, 'clamped is exactly %[2]d: ${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 &&
      report.widened == 0 && report.duplicate == 0,
      'no other counter moves: ${why(report)}');
  final back = Uint8List(t.%[3]sFixedMeasure(%[1]d));
  final wrote = t.%[3]sFixedSave(values, %[1]d, back);
  check(wrote == back.length, 'rewrite length $wrote/${back.length}');
  final hex = StringBuffer();
  for (final b in back) {
    hex.write(b.toRadixString(16).padLeft(2, '0'));
  }
  stdout.writeln('rewrite=$hex');`, records, c.clamped, lower)
	out, err := runVersionProbe(t, dartBinary(t), c.reader, c.older, 0, path, body)
	if err != nil {
		t.Fatalf("R27 %s (%s): probe failed: %v\n%s", file, c.cite, err, out)
	}
	var got []byte
	for line := range strings.SplitSeq(out, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "rewrite="); ok {
			if got, err = hex.DecodeString(h); err != nil {
				t.Fatalf("R27 %s: unreadable rewrite: %v", file, err)
			}
		}
	}
	if !strings.Contains(out, "ok") || got == nil {
		t.Fatalf("R27 %s printed no ok or no rewrite: %s", file, out)
	}
	if !bytes.Equal(got, want) {
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		t.Fatalf("R27 %s: the rewrite is not the bytes %s asks for: first difference at byte %d (len %d vs %d)",
			file, c.cite, at, len(got), len(want))
	}
}

// fixedCorpusField answers one field of a schema's root.
func fixedCorpusField(t *testing.T, base, name string) *ir.Field {
	t.Helper()
	st := versionRoot(t, versionSchema(t, base))
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s's root declares no %s", base, name)
	return nil
}

// fixedCorpusManifestRaw answers a manifest value as the raw integer the dump
// set: a plain integer, or a fixed value's `<units>|raw=<n>`.
func fixedCorpusManifestRaw(t *testing.T, corpus, file, key string) int64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.Contains(line, "file="+file+" ") {
			continue
		}
		_, values, ok := strings.Cut(line, " values=")
		if !ok {
			break
		}
		for pair := range strings.SplitSeq(values, ",") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || k != key {
				continue
			}
			if _, raw, ok := strings.Cut(v, "|raw="); ok {
				v = raw
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				t.Fatalf("the manifest's %s for %s is %q, not an integer", key, file, v)
			}
			return n
		}
		t.Fatalf("the manifest's values for %s carry no %s", file, key)
	}
	t.Fatalf("the manifest carries no row for %s", file)
	return 0
}

// fixedCorpusClampedCell is the reference's one-record file with one fixed
// cell (field `name`, element `slot`) moved to the reader's clamped raw value.
// The cell is found by the reader's declared layout and then CHECKED against
// the manifest's raw value, so a mislaid offset fails rather than passes.
func fixedCorpusClampedCell(t *testing.T, corpus, file, base string, data []byte, name string, slot int, key string) []byte {
	t.Helper()
	st := versionRoot(t, versionSchema(t, base))
	off := int64(len(data)) - fixedTypeBytes(st)
	var f *ir.Field
	for _, g := range st.Fields {
		if g.Name == name {
			f = g
			break
		}
		off += fixedFieldBytes(g)
	}
	if f == nil {
		t.Fatalf("%s's root declares no %s", base, name)
	}
	width := fixedStorageBytes(f.Type)
	off += int64(slot) * width
	raw := fixedCorpusManifestRaw(t, corpus, file, key)
	if got := fixedCorpusLE(data[off : off+width]); got != raw {
		t.Fatalf("%s: the cell at byte %d holds %d, the manifest says %d", file, off, got, raw)
	}
	lo, hi, ok := ir.TableRawRange(f)
	if !ok {
		t.Fatalf("%s.%s declares no range to clamp against", base, name)
	}
	r := big.NewInt(raw)
	var clamp *big.Int
	switch {
	case r.Cmp(hi) > 0:
		clamp = hi
	case r.Cmp(lo) < 0:
		clamp = lo
	default:
		t.Fatalf("%s: raw %d is inside the reader's range [%v, %v]; this is not a clamp row", file, raw, lo, hi)
	}
	out := slices.Clone(data)
	v := clamp.Int64()
	for k := range width {
		out[off+k] = byte(uint64(v) >> (8 * k))
	}
	return out
}

func fixedCorpusLE(b []byte) int64 {
	var u uint64
	for _, v := range slices.Backward(b) {
		u = u<<8 | uint64(v)
	}
	shift := 64 - 8*len(b)
	return int64(u<<shift) >> shift
}

// fixedCorpusForgedAim answers the manifest's `forged=r0.aim@<at>=<v>|0x<bits>`.
func fixedCorpusForgedAim(t *testing.T, corpus, file string) (int, uint32) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.Contains(line, "file="+file+" ") {
			continue
		}
		for field := range strings.FieldsSeq(line) {
			v, ok := strings.CutPrefix(field, "forged=r0.aim@")
			if !ok {
				continue
			}
			atText, _, _ := strings.Cut(v, "=")
			at, err := strconv.Atoi(atText)
			if err != nil {
				t.Fatalf("%s: unreadable forged offset %q", file, atText)
			}
			return at, versionManifestFloatBits(t, corpus, file, "forged", "r0.aim")
		}
	}
	t.Fatalf("the manifest carries no forged aim for %s", file)
	return 0, 0
}

// fixedCorpusNewWithAim is the reference's NEW-generation file — the reader's
// own layout, stamped by its own hash — with aim's four bytes set to `bits`.
// The old file it stands in for must agree with it everywhere but the layout
// hashes and the forged aim, which is checked: the two writers held the same
// lead and trail.
func fixedCorpusNewWithAim(t *testing.T, corpus string, old []byte, at int, bits uint32) []byte {
	t.Helper()
	nu, err := os.ReadFile(filepath.Join(corpus, "new_cfloat_range_widen.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nu) != len(old) {
		t.Fatalf("the new file is %d bytes and the old %d: the layouts are not one size", len(nu), len(old))
	}
	st := versionRoot(t, versionSchema(t, "VNEW_cfloat_range_widen"))
	body := len(nu) - int(fixedTypeBytes(st))
	if !bytes.Equal(nu[body:at], old[body:at]) || !bytes.Equal(nu[at+4:], old[at+4:]) {
		t.Fatal("the new and old cfloat_range_widen records differ outside aim")
	}
	out := slices.Clone(nu)
	binary.LittleEndian.PutUint32(out[at:], bits)
	return out
}

// TestFixedFloat32CellsStoreBitsNotDoubles pins the store and load path of a
// float32 cell: no generated WriteBody or Decode converts a float32 through
// setFloat32/getFloat32 — the decode reads the cell's RAW BITS, which the
// finite rule tests before they land (docs/SPEC-TABLES.md §3.3) — and every
// float32 cell goes through the runtime's pair. Pure Go over the generated
// text.
func TestFixedFloat32CellsStoreBitsNotDoubles(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"VOLD_float_widen", "FU1", "F1"} {
		u := versionSchema(t, base)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: generate: %v", base, err)
		}
		var home string
		for name, data := range files {
			if strings.HasSuffix(name, "Fixed.dart") && bytes.Contains(data, []byte("const int tableFixedForm")) {
				home = string(data)
			}
		}
		if home == "" {
			t.Fatalf("%s: no runtime home generated", base)
		}
		for _, helper := range []string{"static int float32Bits(double value)", "static double float32Value(int bits)"} {
			if !strings.Contains(home, helper) {
				t.Errorf("%s: the runtime carries no %q", base, helper)
			}
		}
		bodies := 0
		cells := 0
		cachedArrays := 0
		for _, fn := range fixedCorpusFunctions(home, "FixedWriteBody(", "FixedDecode(") {
			bodies++
			for _, bad := range []string{"setFloat32(", "getFloat32(", "Uint32List.sublistView("} {
				if strings.Contains(fn, bad) {
					t.Errorf("%s: a generated store/load converts or allocates a float32 view (%s):\n%s", base, bad, fn)
				}
			}
			cachedArrays += strings.Count(fn, "_fixedFloat32Words(")
			cells += strings.Count(fn, "TableFixedLayout.float32Bits(") + strings.Count(fn, "TableFixedLayout.float32Value(") +
				strings.Count(fn, "_fixedFloat32Words(")
		}
		if bodies == 0 || cells == 0 {
			t.Errorf("%s: found %d bodies and %d float32 cells; the pin is vacuous", base, bodies, cells)
		}
		if cachedArrays > 0 {
			for _, fragment := range []string{
				"final Expando<Uint32List> _fixedFloat32WordViews",
				"final cached = _fixedFloat32WordViews[values];",
				"_fixedFloat32WordViews[values] = words;",
			} {
				if !strings.Contains(home, fragment) {
					t.Errorf("%s: float32 array view is not cached with its caller-owned list: missing %q", base, fragment)
				}
			}
		}
	}
}

// The cached view is per caller-owned float array. A second record must not
// borrow the first record's words, neither read nor write may erase signed
// zero or a subnormal while using the cache, and a NaN or an infinity in an
// element is refused both ways (docs/SPEC-TABLES.md §3.3): the save writes
// nothing and the load lands nothing, naming the field.
func TestFixedFloat32ArrayWordViewsPreserveBits(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nfixed table Root { samples [3]float32 }\n")
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tableFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tableFiles)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(fixedFloat32ArrayWordViewProbe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("float32 array raw-bit roundtrip: %v\n%s", err, out)
	}
}

const fixedFloat32ArrayWordViewProbe = `import 'dart:typed_data';
import 'ProbeFixed.dart' as f;

void check(bool ok, String what) { if (!ok) throw StateError(what); }

void main() {
  const expected = <int>[0x00000001, 0x80000000, 0x3f800000];
  final input = <f.Root>[f.Root(), f.Root()];
  for (var record = 0; record < input.length; record++) {
    final words = Uint32List.sublistView(input[record].samples);
    for (var i = 0; i < expected.length; i++) {
      words[i] = expected[i] ^ (record == 0 ? 0 : 0x80000000);
    }
  }
  final wire = Uint8List(f.rootFixedMeasure(input.length));
  check(f.rootFixedSave(input, input.length, wire) == wire.length, 'save failed');
  final output = <f.Root>[f.Root(), f.Root()];
  final plan = f.rootFixedNewPlan();
  final report = f.TableFixedReport();
  check(f.rootFixedLoad(output, output.length, wire, wire.length, plan, report) == output.length, 'load failed');
  for (var record = 0; record < output.length; record++) {
    final words = Uint32List.sublistView(output[record].samples);
    for (var i = 0; i < expected.length; i++) {
      check(words[i] == (expected[i] ^ (record == 0 ? 0 : 0x80000000)), 'changed record $record cell $i');
    }
  }
  // A NaN OR AN INFINITY IN ONE ELEMENT (§3.3): the save refuses before the
  // first byte, and a wire holding one is refused by name with both records
  // back at their defaults.
  for (final bad in <int>[0x7f800001, 0xff800000]) {
    final held = <f.Root>[f.Root(), f.Root()];
    Uint32List.sublistView(held[1].samples)[2] = bad;
    final untouched = Uint8List(wire.length);
    check(f.rootFixedSave(held, held.length, untouched) == -1, 'save wrote the non-finite $bad');
    check(untouched.every((b) => b == 0), 'a refused save wrote bytes');
    final forged = Uint8List.fromList(wire);
    ByteData.sublistView(forged).setUint32(forged.length - 4, bad, Endian.little);
    final landed = <f.Root>[f.Root(), f.Root()];
    landed[0].samples[0] = 5.0;
    final refused = f.TableFixedReport();
    check(f.rootFixedLoad(landed, landed.length, forged, forged.length, f.rootFixedNewPlan(), refused) == -1, 'load kept the non-finite $bad');
    check(refused.refused == f.TableFixedRefusal.nonFiniteFloat, 'load refused $bad as ${f.TableFixedRefusal.name(refused.refused)}');
    check(refused.nonFiniteFloat == 'Root.samples', 'load named "${refused.nonFiniteFloat}" for $bad');
    check(landed.every((r) => r.samples.every((x) => x == 0.0)), 'a refused load landed values for $bad');
  }
}
`

// fixedCorpusFunctions cuts every top-level function whose name ends in one
// of the suffixes out of a generated library.
func fixedCorpusFunctions(src string, suffixes ...string) []string {
	var out []string
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "void ") {
			continue
		}
		hit := false
		for _, s := range suffixes {
			hit = hit || strings.Contains(lines[i], s)
		}
		if !hit {
			continue
		}
		j := i
		for j < len(lines) && lines[j] != "}" {
			j++
		}
		out = append(out, strings.Join(lines[i:min(j+1, len(lines))], "\n"))
		i = j
	}
	return out
}

// fixedManifestNonFiniteValue is one manifest value whose bits a dump spelled
// beside its digits: its path, then the bits ("nan|0x7F800001").
var fixedManifestNonFiniteValue = regexp.MustCompile(`^([A-Za-z0-9_.\[\]]+)=[^|]*\|0x([0-9A-Fa-f]{16}|[0-9A-Fa-f]{8})$`)

// fixedManifestNonFinite reads, per `file=` line of the corpus manifest, the
// leaf name of the first value the reference wrote as a NaN or an infinity,
// and "" for a file that holds none — the Go leg's own reading
// (internal/codegen/gotable/fixedoracle_test.go fixedOracleManifestNonFinite).
// Under the finite rule (docs/SPEC-TABLES.md §3.3) such a file is a
// MUST-REFUSE vector until tables-finite/cpp regenerates the corpus finite.
func fixedManifestNonFinite(t *testing.T, corpus string) map[string]string {
	t.Helper()
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		head, values, ok := strings.Cut(line, " values=")
		if !ok {
			continue
		}
		file := ""
		for field := range strings.FieldsSeq(head) {
			if v, ok := strings.CutPrefix(field, "file="); ok {
				file = v
			}
		}
		for value := range strings.SplitSeq(values, ",") {
			m := fixedManifestNonFiniteValue.FindStringSubmatch(value)
			if m == nil {
				continue
			}
			bits, err := strconv.ParseUint(m[2], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			nonFinite := len(m[2]) == 8 && bits&0x7F800000 == 0x7F800000 || len(m[2]) == 16 && bits&0x7FF0000000000000 == 0x7FF0000000000000
			if nonFinite && file != "" && out[file] == "" {
				leaf := m[1]
				if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
					leaf = leaf[i+1:]
				}
				out[file] = leaf
			}
		}
	}
	return out
}

// TestFixedManifestNonFiniteDart pins the manifest reading the must-refuse
// rows stand on: the first NaN or infinity a file's values spell, at either
// width, by its leaf name, and nothing for a file whose floats are finite.
func TestFixedManifestNonFiniteDart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := `file=fu1.bin row=fu1 side=none root=FuRoot records=3 values=v[0].flag=1,v[0].pick.labelled.label="he\,llo",v[0].heat=nan|0x7F800001,v[1].heat=-nan|0xFFA5A5A5,v[2].heat=1.5|0x3FC00000
file=fu2.bin row=fu2 side=none root=FuRoot records=2 values=v[1].heat=-2.25|0xC002000000000000,v[0].wide=inf|0x7FF0000000000000
file=fx1.bin row=fx1 side=none root=FxRoot records=2 values=v[0].keep=4242,v[0].x=-0|0x80000000,v[1].y=4.9e-324|0x0000000000000001
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	got := fixedManifestNonFinite(t, dir)
	if got["fu1.bin"] != "heat" || got["fu2.bin"] != "wide" || got["fx1.bin"] != "" {
		t.Fatalf("non-finite leaves %v, want fu1 heat, fu2 wide, fx1 none", got)
	}
}
