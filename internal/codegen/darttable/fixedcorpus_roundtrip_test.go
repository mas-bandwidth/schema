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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

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
	for k := len(b) - 1; k >= 0; k-- {
		u = u<<8 | uint64(b[k])
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
// float32 cell: no generated WriteBody or Decode converts a float32 through a
// double with setFloat32/getFloat32 (the hardware conversion quiets a
// signalling NaN), and every float32 cell goes through the runtime's bit-exact
// pair. Pure Go over the generated text.
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
		for _, fn := range fixedCorpusFunctions(home, "FixedWriteBody(", "FixedDecode(") {
			bodies++
			for _, bad := range []string{"setFloat32(", "getFloat32("} {
				if strings.Contains(fn, bad) {
					t.Errorf("%s: a generated store/load converts a float32 through a double (%s):\n%s", base, bad, fn)
				}
			}
			cells += strings.Count(fn, "TableFixedLayout.float32Bits(") + strings.Count(fn, "TableFixedLayout.float32Value(") +
				strings.Count(fn, "Uint32List.sublistView(")
		}
		if bodies == 0 || cells == 0 {
			t.Errorf("%s: found %d bodies and %d float32 cells; the pin is vacuous", base, bodies, cells)
		}
	}
}

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
