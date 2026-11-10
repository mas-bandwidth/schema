package lobstertable_test

// The eight fixed-table rows the shared fixedproof suites do not reach
// (numeric-evolution, scalar-bounds, ordinals, normalization, reports,
// array-bounds, text, hostile-input), each run against the emitted Lobster
// fixed form under the strict-only read (docs/FIXED-FORM-ALGORITHM.md §4.5,
// §4.6; docs/SPEC-TABLES.md §6.5): a value past the writer's range, an
// ordinal or tag past the writer's count, a bool or present byte other than
// 0 or 1, a count or length past its bound and ill-formed text all REFUSE by
// name with nothing landed; nothing clamps. Each row carries a negative
// control that plants one break in the generated code and must go red.
// Modelled on goosetable/fixed_evolution_proof_test.go and
// mootable/fixed_evolution_proof_test.go.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const rowsGate = "the pinned Lobster toolchain (it runs the emitted fixed-form reader)"

// rowRun is one native load: count is -1 on a refusal, reason its name.
type rowRun struct {
	count   int
	reason  string
	fields  map[string]string
	resaved []byte
	c       fixedproof.LoadCounters
}

func (r rowRun) head() string {
	if r.count < 0 {
		return "refused " + r.reason
	}
	return fmt.Sprintf("ok %d u%d w%d", r.count, r.c.Unknown, r.c.Widened)
}

func rowLoad(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, mutate func(map[string][]byte)) rowRun {
	t.Helper()
	n, why, fields, resaved, c := lobsterLoadLineage(t, u, root, lineage, wire, -1, mutate)
	return rowRun{count: n, reason: why, fields: fields, resaved: resaved, c: c}
}

// rowSave saves one record of root after setup (Lobster that edits r[0]).
func rowSave(t *testing.T, u *ir.Unit, root, setup string, records int) []byte {
	t.Helper()
	files := rowFiles(t, u)
	script := lobsterProofImports(u) + fmt.Sprintf(`let r:[%[1]s.%[2]s] = []
for(%[4]d): push(r, %[1]s.New%[2]s())
%[3]sassert write_file("out.bin", %[1]s.%[2]sFixedSave(r))
print("OK")
`, u.Package, root, setup, records)
	out, wire := lobsterEvolutionRun(t, files, script, nil)
	if strings.TrimSpace(out) != "OK" || wire == nil {
		t.Fatalf("native save: %q", out)
	}
	return wire
}

func rowFiles(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return files
}

// plantRow replaces anchor, which must occur exactly once in the named
// generated file.
func plantRow(t *testing.T, file, anchor, with string) func(map[string][]byte) {
	return func(files map[string][]byte) {
		t.Helper()
		src := string(files[file])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the anchor %q is not in %s exactly once", anchor, file)
		}
		files[file] = []byte(strings.Replace(src, anchor, with, 1))
	}
}

// plantBounds replaces the first occurrence of anchor inside root's emitted
// bounds pass (def <root>FixedBoundsBody); the save-side check body carries
// the same lines and is left alone.
func plantBounds(t *testing.T, root, anchor, with string) func(map[string][]byte) {
	return func(files map[string][]byte) {
		t.Helper()
		src := string(files[rowsProbeTable])
		marker := "def " + root + "FixedBoundsBody("
		at := strings.Index(src, marker)
		if at < 0 {
			t.Fatalf("no %s in %s", marker, rowsProbeTable)
		}
		end := strings.Index(src[at+len(marker):], "\ndef ")
		if end < 0 {
			end = len(src) - at - len(marker)
		}
		body := src[at : at+len(marker)+end]
		if strings.Count(body, anchor) != 1 {
			t.Fatalf("the anchor %q is not in %s exactly once", anchor, marker)
		}
		files[rowsProbeTable] = []byte(src[:at] + strings.Replace(body, anchor, with, 1) + src[at+len(body):])
	}
}

func rowEntry(t *testing.T, u *ir.Unit, root string) fixedproof.LineageEntry {
	t.Helper()
	e, ok := fixedproof.LineageEntryOf(u, root)
	if !ok {
		t.Fatalf("no fixed root %s", root)
	}
	return e
}

func rowLineage(t *testing.T, older *ir.Unit, root string) map[string][]fixedproof.LineageEntry {
	return map[string][]fixedproof.LineageEntry{root: {rowEntry(t, older, root)}}
}

// rowOffset is the byte offset of field inside root's record body.
func rowOffset(t *testing.T, u *ir.Unit, root, field string) int {
	t.Helper()
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != root {
			continue
		}
		var at int64
		for _, f := range st.Fields {
			if f.Name == field {
				return int(at)
			}
			at += ir.TableFixedFieldBytes(f)
		}
	}
	t.Fatalf("no field %s.%s", root, field)
	return 0
}

// rowBody is the offset of record i's body in a form-3 file.
func rowBody(file []byte, rec int64, i int) int {
	return 20 + int(binary.LittleEndian.Uint32(file[16:20])) + i*int(rec) + 8
}

func cloneRow(b []byte) []byte { return append([]byte(nil), b...) }

const rowsFixedevolve = "fixedevolve.lobster"
const rowsProbeTable = "probeTable.lobster"

// TestLobsterFixedRowNumericEvolution is numeric-evolution (E3, C8, R20,
// R21): an old file of {uint32, int16, float32, uint32} reads under a reader
// that widened int16 to int32 and float32 to float64 and appended a field:
// int16 sign-extends (-7 lands -7), the float lands its exact float64
// widening, the added field lands its declared default, and widened counts
// one per widened entry per record (4 over two records) with nothing else
// moved. The negative controls drop the widened count and the sign extension.
func TestLobsterFixedRowNumericEvolution(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	older := fixedproof.ParseCheckUnit(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int16 = 0\n    f float32 = 0.0\n    trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int32 = 0\n    f float64 = 0.0\n    trail uint32 = 2\n    added int32 = 77\n}\n")
	eo := rowEntry(t, older, "N")
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 0xFFF9) // -7
	body = binary.LittleEndian.AppendUint32(body, 0xC49A5200)
	body = binary.LittleEndian.AppendUint32(body, 6)
	file := fixedproof.MakeFixedFile(eo.Wire, eo.Layout, body, body)
	lin := rowLineage(t, older, "N")
	got := rowLoad(t, newer, "N", lin, file, nil)
	if got.head() != "ok 2 u0 w4" {
		t.Errorf("the widening read: %s, want ok 2 u0 w4", got.head())
	}
	for k, w := range map[string]string{"r0.lead": "5", "r0.v": "-7", "r0.trail": "6", "r0.added": "77", "r1.v": "-7", "r1.added": "77"} {
		if got.fields[k] != w {
			t.Errorf("%s landed %q, want %s", k, got.fields[k], w)
		}
	}
	en := rowEntry(t, newer, "N")
	fAt := rowOffset(t, newer, "N", "f")
	for i := range 2 {
		at := rowBody(got.resaved, en.Record, i) + fAt
		if at+8 > len(got.resaved) {
			t.Fatalf("the re-save is %d bytes, f of record %d sits at %d", len(got.resaved), i, at)
		}
		if b := binary.LittleEndian.Uint64(got.resaved[at:]); b != math.Float64bits(float64(math.Float32frombits(0xC49A5200))) || b != 0xC0934A4000000000 {
			t.Errorf("r%d.f widened to %#x, want 0xC0934A4000000000", i, b)
		}
	}
	nc := rowLoad(t, newer, "N", lin, file, plantRow(t, rowsFixedevolve,
		"                            dst = write_int8_le(dst, p.dst + k, shr(raw, 8 * k) & 0xff)\n                    report.widened += 1\n",
		"                            dst = write_int8_le(dst, p.dst + k, shr(raw, 8 * k) & 0xff)\n"))
	if nc.head() == got.head() {
		t.Errorf("the negative control: an integer widening not counted still reads %s", nc.head())
	}
	nc = rowLoad(t, newer, "N", lin, file, plantRow(t, rowsFixedevolve,
		"raw = read_int16_le(src, src_base + p.src)", "raw = read_uint16_le(src, src_base + p.src)"))
	if nc.count == 2 && nc.fields["r0.v"] == "-7" {
		t.Errorf("the negative control: a zero-extended int16 still lands -7")
	}
}

// TestLobsterFixedRowScalarBounds is scalar-bounds (C7, R30) under the
// strict read: a ranged int32, int128 and uint16 inside their ranges land and
// re-save byte for byte; one value past either end of any of them refuses
// out_of_range with nothing landed (fix 5: it clamped and counted before). A
// compressed float is saved as its float32 bits unquantized; past its range it
// refuses out_of_range, -0.0 lands as written, and its resolution is in the
// digest under 'Q', so a finer or a dropped resolution is a new hash whose
// compiled plan reads the old writer's bits unchanged. The negative control
// drops the int32 range check from the bounds pass.
func TestLobsterFixedRowScalarBounds(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T\n{\n    x int32 = 0 | min = -5, max = 5\n    s int128 = 0 | min = -500, max = 500\n    u uint16 = 10 | min = 10, max = 1000\n}\n")
	e := rowEntry(t, u, "T")
	body := func(x int32, s int64, v uint16) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(x))
		b = binary.LittleEndian.AppendUint64(b, uint64(s))
		hi := uint64(0)
		if s < 0 {
			hi = ^uint64(0)
		}
		b = binary.LittleEndian.AppendUint64(b, hi)
		return binary.LittleEndian.AppendUint16(b, v)
	}
	if e.Record != 8+int64(len(body(0, 0, 0))) {
		t.Fatalf("the record body is %d, the test frames %d", e.Record-8, len(body(0, 0, 0)))
	}
	in := fixedproof.MakeFixedFile(e.Wire, e.Layout, body(3, -42, 500))
	got := rowLoad(t, u, "T", nil, in, nil)
	if got.head() != "ok 1 u0 w0" || got.fields["r0.x"] != "3" || got.fields["r0.u"] != "500" || string(got.resaved) != string(in) {
		t.Errorf("C7 in range: %s %v (re-save identical %v)", got.head(), got.fields, string(got.resaved) == string(in))
	}
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"x below", body(-6, 0, 10)}, {"x above", body(6, 0, 10)},
		{"s below", body(0, -501, 10)}, {"s above", body(0, 501, 10)},
		{"u below", body(0, 0, 9)}, {"u above", body(0, 0, 1001)},
	} {
		if got := rowLoad(t, u, "T", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, c.b), nil); got.head() != "refused out_of_range" {
			t.Errorf("C7 %s: %s, want refused out_of_range", c.name, got.head())
		}
	}
	nc := rowLoad(t, u, "T", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, body(-6, 0, 10)), plantBounds(t, "T",
		"    if (v.X < -5) or (v.X > 5):\n        return false\n", ""))
	if nc.head() == "refused out_of_range" {
		t.Errorf("the negative control: the dropped range check still refuses")
	}

	const cf = "package probe\nfixed table Reading\n{\n    lead uint32 = 1\n    aim float32 = 0.0%s\n    trail uint32 = 2\n}\n"
	old := fixedproof.ParseCheckUnit(t, fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.1"))
	if d := ir.TableFixedDefinitionsDigest(old.Tables["Reading"]); !strings.Contains(string(d), "Q") {
		t.Errorf("R30: the digest does not carry the resolution under 'Q'")
	}
	saved := rowSave(t, old, "Reading", "r[0].Aim = 0.3\n", 1)
	at := len(saved) - 8
	if b := binary.LittleEndian.Uint32(saved[at:]); b != 0x3e99999a {
		t.Errorf("R30: the compressed float is not saved as its float32 bits: %#x", b)
	}
	for _, c := range []struct {
		bits uint32
		want string
	}{{0xc0000000, "refused out_of_range"}, {0x40800000, "refused out_of_range"}, {0x80000000, "ok 1 u0 w0"}, {0x3e99999a, "ok 1 u0 w0"}} {
		f := cloneRow(saved)
		binary.LittleEndian.PutUint32(f[at:], c.bits)
		got := rowLoad(t, old, "Reading", nil, f, nil)
		if got.head() != c.want || (got.count == 1 && string(got.resaved) != string(f)) {
			t.Errorf("R30 %#08x: %s (re-save identical %v), want %s", c.bits, got.head(), string(got.resaved) == string(f), c.want)
		}
	}
	lin := rowLineage(t, old, "Reading")
	for _, c := range []struct{ name, src string }{
		{"finer", fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.01")},
		{"dropped", fmt.Sprintf(cf, "")},
	} {
		nu := fixedproof.ParseCheckUnit(t, c.src)
		if rowEntry(t, nu, "Reading").Wire == rowEntry(t, old, "Reading").Wire {
			t.Errorf("R30 %s: the resolution did not move the hash", c.name)
		}
		got := rowLoad(t, nu, "Reading", lin, saved, nil)
		if got.head() != "ok 1 u0 w0" || len(got.resaved) < 8 || binary.LittleEndian.Uint32(got.resaved[len(got.resaved)-8:]) != 0x3e99999a {
			t.Errorf("R30 %s: %s", c.name, got.head())
		}
	}
}

const rowOrdinalOld = "package probe\nenum Tier { Bronze, Silver }\nunion Pick\n{\n    a int32\n    b int32\n}\nfixed table Lineage\n{\n    tier Tier\n    pick Pick\n    seq int32\n}\n"

// TestLobsterFixedRowOrdinals is ordinals (C9, C10, R31) under the strict
// read (fix 14): an enum ordinal past the writer's variants and a union tag
// past the writer's arms each refuse out_of_range, on the identity plan and
// on a compiled plan whose reader declares MORE variants and arms (the
// writer's count governs). The clean record lands. The negative controls drop
// the compiled plan's ordinal check and the check on the const entry that
// lands a remapped union tag; with either gone its forged record reads.
func TestLobsterFixedRowOrdinals(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	uOld := fixedproof.ParseCheckUnit(t, rowOrdinalOld)
	uNew := fixedproof.ParseCheckUnit(t, strings.Replace(strings.Replace(rowOrdinalOld, "Silver }", "Silver, Gold }", 1), "    b int32\n", "    b int32\n    c int32\n", 1))
	eo := rowEntry(t, uOld, "Lineage")
	if eo.Record != 8+10 {
		t.Fatalf("the record is %d bytes, the test frames 18", eo.Record)
	}
	clean := fixedproof.MakeFixedFile(eo.Wire, eo.Layout, []byte{2, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	tier := fixedproof.MakeFixedFile(eo.Wire, eo.Layout, []byte{3, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	tag := fixedproof.MakeFixedFile(eo.Wire, eo.Layout, []byte{2, 3, 9, 0, 0, 0, 42, 0, 0, 0})
	lin := rowLineage(t, uOld, "Lineage")
	for _, plan := range []struct {
		name string
		u    *ir.Unit
		lin  map[string][]fixedproof.LineageEntry
	}{{"identity", uOld, nil}, {"compiled", uNew, lin}} {
		if c := rowLoad(t, plan.u, "Lineage", plan.lin, clean, nil); c.head() != "ok 1 u0 w0" || c.fields["r0.tier"] != "2" || c.fields["r0.pick.type"] != "2" || c.fields["r0.pick.b"] != "9" || c.fields["r0.seq"] != "42" {
			t.Errorf("%s, the clean record: %s %v", plan.name, c.head(), c.fields)
		}
		if got := rowLoad(t, plan.u, "Lineage", plan.lin, tier, nil).head(); got != "refused out_of_range" {
			t.Errorf("C9 %s: an ordinal past the writer's variants: %s", plan.name, got)
		}
		if got := rowLoad(t, plan.u, "Lineage", plan.lin, tag, nil).head(); got != "refused out_of_range" {
			t.Errorf("C10 %s: a tag past the writer's arms: %s", plan.name, got)
		}
	}
	if got := rowLoad(t, uNew, "Lineage", lin, tier, plantRow(t, rowsFixedevolve,
		"                if ult(length(p.remap), fixed_tag_at(src, src_base + p.src, p.size)):\n", "                if false:\n")); got.count != 1 {
		t.Errorf("the negative control: the dropped ordinal check still refuses: %s", got.head())
	}
	if got := rowLoad(t, uNew, "Lineage", lin, tag, plantRow(t, rowsFixedevolve,
		"                    if ult(p.dst_size, fixed_tag_at(src, src_base + p.src, p.meta)):\n", "                    if false:\n")); got.count != 1 {
		t.Errorf("the negative control: the dropped tag check still refuses: %s", got.head())
	}
}

// TestLobsterFixedRowNormalization is normalization (C12, C13, R17) under
// the strict read (fix 2): a bool byte or a present byte other than 0 or 1
// refuses wire_damaged with nothing landed; 1 and 1 land true and the payload
// behind the present byte lands; a present byte of 0 makes the payload dead
// storage, never read (0xff there loads) and written back as zeros. The
// negative control widens the byte check to 255; 0x7f then reads.
func TestLobsterFixedRowNormalization(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T\n{\n    flag bool\n    opt ?int32\n}\n")
	e := rowEntry(t, u, "T")
	if e.Record != 8+6 {
		t.Fatalf("the record is %d bytes, the test frames 14", e.Record)
	}
	file := func(b ...byte) []byte { return fixedproof.MakeFixedFile(e.Wire, e.Layout, b) }
	for _, c := range []struct {
		name string
		f    []byte
	}{
		{"C12 bool 0x7f", file(0x7f, 1, 42, 0, 0, 0)},
		{"C12 bool 0x02", file(2, 1, 42, 0, 0, 0)},
		{"C13 present 0x5a", file(1, 0x5a, 42, 0, 0, 0)},
		{"C13 present 0xff", file(0, 0xff, 42, 0, 0, 0)},
	} {
		if got := rowLoad(t, u, "T", nil, c.f, nil).head(); got != "refused wire_damaged" {
			t.Errorf("%s: %s, want refused wire_damaged", c.name, got)
		}
	}
	if got := rowLoad(t, u, "T", nil, file(1, 1, 42, 0, 0, 0), nil); got.head() != "ok 1 u0 w0" || got.fields["r0.flag"] != "true" || got.fields["r0.opt_present"] != "true" || got.fields["r0.opt"] != "42" {
		t.Errorf("R17 the lawful bytes: %s %v", got.head(), got.fields)
	}
	dead := file(0, 0, 0xff, 0xff, 0xff, 0xff)
	if got := rowLoad(t, u, "T", nil, dead, nil); got.head() != "ok 1 u0 w0" || got.fields["r0.opt_present"] != "false" || string(got.resaved) != string(file(0, 0, 0, 0, 0, 0)) {
		t.Errorf("R17 an absent payload: %s %v", got.head(), got.fields)
	}
	if got := rowLoad(t, u, "T", nil, file(0x7f, 1, 42, 0, 0, 0), plantRow(t, rowsFixedevolve, "                        if v > 1:\n", "                        if v > 255:\n")); got.count != 1 {
		t.Errorf("the negative control: the widened byte check still refuses: %s", got.head())
	}
}

// TestLobsterFixedRowReports is reports (E9, R16, R18): the fixed form never
// raises duplicate and has no clamp counter (no `duplicate +=` and no
// `clamped +=` in the emitted table module or the fixed runtime); a field the
// writer has and the reader dropped counts unknown ONCE per load, not once per
// record, and a load through a report holding stale counters resets them
// first (three records: unknown 1, widened 0); and an int8 ranged on its
// width's own limits beside an unranged uint8 reads all 256 byte values with
// nothing moved. The negative control removes the report reset; the stale
// unknown then shows.
func TestLobsterFixedRowReports(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table R\n{\n    a int8 = 0 | min = -128, max = 127\n    b uint8\n}\n")
	files := rowFiles(t, u)
	for _, name := range []string{rowsProbeTable, rowsFixedevolve} {
		src := string(files[name])
		if strings.Contains(src, "duplicate +=") || strings.Contains(src, "clamped +=") {
			t.Errorf("E9: %s moves duplicate or clamped", name)
		}
	}
	e := rowEntry(t, u, "R")
	var bodies [][]byte
	for x := range 256 {
		bodies = append(bodies, []byte{byte(x), byte(x)})
	}
	if got := rowLoad(t, u, "R", nil, fixedproof.MakeFixedFile(e.Wire, e.Layout, bodies...), nil); got.head() != "ok 256 u0 w0" {
		t.Errorf("R18: every byte of a full-width range: %s", got.head())
	}
	older := fixedproof.ParseCheckUnit(t, "package probe\nfixed table C\n{\n    lead uint32 = 1\n    gone int32 = 0\n    trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table C\n{\n    lead uint32 = 1\n    trail uint32 = 2\n}\n")
	eo := rowEntry(t, older, "C")
	rec := []byte{5, 0, 0, 0, 9, 0, 0, 0, 6, 0, 0, 0}
	three := fixedproof.MakeFixedFile(eo.Wire, eo.Layout, rec, rec, rec)
	lin := rowLineage(t, older, "C")
	got := rowLoad(t, newer, "C", lin, three, nil)
	if got.head() != "ok 3 u1 w0" || got.fields["r2.lead"] != "5" || got.fields["r2.trail"] != "6" {
		t.Errorf("R16: the census over three records: %s %v", got.head(), got.fields)
	}
	if nc := rowLoad(t, newer, "C", lin, three, plantRow(t, rowsFixedevolve, "    r.unknown = 0\n", "")); nc.head() == got.head() {
		t.Errorf("the negative control: without the reset the stale census still reads %s", nc.head())
	}
}

// TestLobsterFixedRowArrayBounds is array-bounds (C1, C2, R11): the plan
// carries the WRITER's bound. A counted array the old writer bounded at 4 and
// the new reader at 8 refuses count_over_bound for a count of -1 and of 7
// (inside the reader's 8, past the writer's 4), and reads at exactly 4. The
// negative control bounds the count by the reader's 8; 7 then reads.
func TestLobsterFixedRowArrayBounds(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	older := fixedproof.ParseCheckUnit(t, "package probe\nfixed table G\n{\n    lead uint32 = 1\n    vals [..4]int32\n    trail uint32 = 2\n}\n")
	newer := fixedproof.ParseCheckUnit(t, "package probe\nfixed table G\n{\n    lead uint32 = 1\n    vals [..8]int32\n    trail uint32 = 2\n}\n")
	base := rowSave(t, older, "G", "r[0].ValsCount = 4\nr[0].Vals = [10, 20, 30, 40]\n", 1)
	countAt := rowBody(base, rowEntry(t, older, "G").Record, 0) + rowOffset(t, older, "G", "vals")
	with := func(n uint32) []byte {
		b := cloneRow(base)
		binary.LittleEndian.PutUint32(b[countAt:], n)
		return b
	}
	lin := rowLineage(t, older, "G")
	for _, c := range []struct {
		name  string
		count uint32
		want  string
	}{
		{"C1 count -1", 0xFFFFFFFF, "refused count_over_bound"},
		{"C2 count 7 past the writer's 4", 7, "refused count_over_bound"},
		{"C2 count 5 past the writer's 4", 5, "refused count_over_bound"},
		{"R11 count 4, the writer's bound", 4, "ok 1 u0 w0"},
	} {
		if got := rowLoad(t, newer, "G", lin, with(c.count), nil); got.head() != c.want || (c.count == 4 && (got.fields["r0.vals_count"] != "4" || got.fields["r0.vals[3]"] != "40")) {
			t.Errorf("%s: %s %v, want %s", c.name, got.head(), got.fields, c.want)
		}
	}
	if got := rowLoad(t, newer, "G", lin, with(7), plantRow(t, rowsFixedevolve,
		"                if v < 0 or v > p.size:\n                    return TableRefuseCountOverBound\n",
		"                if v < 0 or v > 8:\n                    return TableRefuseCountOverBound\n")); got.head() == "refused count_over_bound" {
		t.Errorf("the negative control: the reader's bound still refuses the writer's overrun")
	}
}

// TestLobsterFixedRowText is text (C3, C4, C5, R24): a length past the
// field's capacity refuses length_over_bound; ill-formed UTF-8 or a zero in
// the USED bytes, and an unpaired surrogate in a wstring's used units, refuse
// text_ill_formed; a hostile byte in the slack past the used length is not
// read and not a refusal. The negative control drops the UTF-8 check from the
// read and from the bounds pass; 0xff in the used bytes then reads.
func TestLobsterFixedRowText(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table T\n{\n    s string(4)\n}\n")
	clean := rowSave(t, u, "T", "r[0].S = \"ab\"\nr[0].SLength = 2\nr[0].W = \"\\x41\\x00\\x42\\x00\"\nr[0].WLength = 2\n", 1)
	body := len(clean) - 8
	if body != rowBody(clean, rowEntry(t, u, "T").Record, 0) {
		t.Fatalf("the record body is not the last 8 bytes")
	}
	cut := func(f func(b []byte)) []byte { b := cloneRow(clean); f(b); return b }
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"C3 length 5 of 4", cut(func(b []byte) { b[body] = 5 }), "refused length_over_bound"},
		{"C3 length -1", cut(func(b []byte) { b[body], b[body+1], b[body+2], b[body+3] = 0xff, 0xff, 0xff, 0xff }), "refused length_over_bound"},
		{"C4 0xff in the used bytes", cut(func(b []byte) { b[body+4] = 0xff }), "refused text_ill_formed"},
		{"C4 a zero in the used bytes", cut(func(b []byte) { b[body+5] = 0 }), "refused text_ill_formed"},
		{"R24 0xff in the slack", cut(func(b []byte) { b[body+6], b[body+7] = 0xff, 0xff }), "ok 1 u0 w0"},
	}
	for _, c := range cases {
		if got := rowLoad(t, u, "T", nil, c.file, nil).head(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := rowLoad(t, u, "T", nil, clean, nil); got.fields["r0.s"] != `"ab"` || got.fields["r0.s_length"] != "2" || string(got.resaved) != string(clean) {
		t.Errorf("the clean record: %s %v", got.head(), got.fields)
	}
	if got := rowLoad(t, u, "T", nil, cases[2].file, func(files map[string][]byte) {
		plantRow(t, rowsFixedevolve, "        return not fixed_utf8_valid_no_nul(p, base, used)\n", "        return false\n")(files)
		plantBounds(t, "T", " or not schema_rt.fixed_utf8_valid_no_nul(v.S, 0, v.SLength):\n", ":\n")(files)
	}); got.count != 1 {
		t.Errorf("the negative control: with the UTF-8 checks gone ill-formed text still refuses: %s", got.head())
	}
}

const rowHostSchema = "package probe\nfixed table Host\n{\n    small int32 = 5 | min = 0, max = 1000\n    marks [..4]int32 | min = 0, max = 1000\n    note ?int32 | min = 0, max = 1000\n    flag bool\n    tail int32 = 7\n}\n"

// rowHostSweep poisons every byte of a Host record six ways (0x00, 0x01,
// 0x02, 0x7f, 0x80, 0xff) and loads each image into storage pre-set to a
// sentinel. A refusal must be a declared name with the storage reset to the
// declared defaults (nothing landed); a read must land every live value
// inside its bound. It prints "<failures> <cases>" and each failure.
const rowHostSweep = `import .probeTable
let v = probe.NewHost()
v.Small = 500
v.MarksCount = 2
v.Marks[0] = 1
v.Marks[1] = 2
v.NotePresent = true
v.Note = 3
v.Flag = true
let wire = probe.HostFixedSave([v])
let body = 20 + read_uint32_le(wire, 16) + 8
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let report = schema_rt.TableReport {}
let pokes = [0x00, 0x01, 0x02, 0x7f, 0x80, 0xff]
let names = ["out_of_range", "count_over_bound", "wire_damaged"]
var fails = 0
var cases = 0
for(probe.HostFixedRecordBytes) off:
    for(pokes) b:
        cases += 1
        let bad = write_int8_le(copy(wire), body + off, b)
        let back = [probe.NewHost()]
        back[0].Small = 4242
        back[0].Tail = 4242
        let n = probe.HostFixedLoad(back, bad, plan, report)
        var ok = true
        if n == -1:
            var named = false
            for(names) x:
                if x == report.reason:
                    named = true
            if not named:
                ok = false
            if back[0].Small != 5 or back[0].Tail != 7 or back[0].MarksCount != 0 or back[0].NotePresent or back[0].Flag:
                ok = false
        elif n == 1:
            let h = back[0]
            if h.Small < 0 or h.Small > 1000 or h.MarksCount < 0 or h.MarksCount > 4:
                ok = false
            else:
                for(h.MarksCount) i:
                    if h.Marks[i] < 0 or h.Marks[i] > 1000:
                        ok = false
            if h.NotePresent and (h.Note < 0 or h.Note > 1000):
                ok = false
        else:
            ok = false
        if not ok:
            fails += 1
            print("FAIL off=" + string(off) + " b=" + string(b) + " n=" + string(n) + " " + report.reason)
print(string(fails) + " " + string(cases))
`

// TestLobsterFixedRowHostileInput is hostile-input (P3, P4): every byte of a
// record poisoned six ways either refuses by a declared name with nothing
// landed or lands every live value in its bound; a bad hash on the LAST
// record of a batch refuses no_layout; and the wrong plan goes red: B's reader
// refuses A's file layout_unknown, and A's file announced under B's hash is
// layout_malformed, never read through B's plan. The negative control drops
// the range check on small and requires the sweep to find an out-of-bound
// value.
func TestLobsterFixedRowHostileInput(t *testing.T) {
	slowtest.Gate(t, rowsGate)
	t.Parallel()
	u := fixedproof.ParseCheckUnit(t, rowHostSchema)
	cases := 6 * (rowEntry(t, u, "Host").Record - 8)
	sweep := func(mutate func(map[string][]byte)) string {
		files := rowFiles(t, u)
		if mutate != nil {
			mutate(files)
		}
		out, _ := lobsterEvolutionRun(t, files, rowHostSweep, nil)
		lines := strings.Split(strings.TrimSpace(out), "\n")
		return lines[len(lines)-1] + "|" + strings.Join(lines[:len(lines)-1], ";")
	}
	if got, want := sweep(nil), fmt.Sprintf("0 %d|", cases); got != want {
		t.Errorf("P3: the sweep: %q, want %q", got, want)
	}
	two := rowSave(t, u, "Host", "", 2)
	two[len(two)-int(rowEntry(t, u, "Host").Record)] ^= 1
	if got := rowLoad(t, u, "Host", nil, two, nil).head(); got != "refused no_layout" {
		t.Errorf("P3: the last record's hash: %s", got)
	}
	if got := sweep(plantBounds(t, "Host", "    if (v.Small < 0) or (v.Small > 1000):\n        return false\n", "")); strings.HasPrefix(got, "0 ") {
		t.Errorf("the negative control: the sweep without the range check finds nothing: %s", got)
	}

	ab := fixedproof.ParseCheckUnit(t, "package probe\nfixed table A\n{\n    lead uint32 = 0\n    x int32 = 0\n    y int32 = 0\n    trail uint32 = 0\n}\nfixed table B\n{\n    p int8 = 0\n    q int8 = 0\n}\n")
	aFile := rowSave(t, ab, "A", "r[0].Lead = 0xAABBCCDD\nr[0].X = 111\nr[0].Y = 222\nr[0].Trail = 0x11223344\n", 1)
	if got := rowLoad(t, ab, "B", nil, aFile, nil).head(); got != "refused layout_unknown" {
		t.Errorf("P4: B's reader on A's file: %s, want refused layout_unknown", got)
	}
	asB := cloneRow(aFile)
	binary.LittleEndian.PutUint64(asB[8:], rowEntry(t, ab, "B").Wire)
	if got := rowLoad(t, ab, "B", nil, asB, nil).head(); got != "refused layout_malformed" {
		t.Errorf("P4: A's file announced as B: %s, want refused layout_malformed", got)
	}
	if got := rowLoad(t, ab, "A", nil, aFile, nil); got.fields["r0.x"] != "111" || got.fields["r0.trail"] != "287454020" {
		t.Errorf("P4: the right plan: %s %v", got.head(), got.fields)
	}
}
