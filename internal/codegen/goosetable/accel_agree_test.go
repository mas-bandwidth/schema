package goosetable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ACCESSOR AND DESCRIPTOR AGREEMENT (docs/PORTING.md J1; M11's Goose
// form). A cooked record and a block row are read two ways in this leg: by
// the generated typed accessors (<Row>Row<Field>(b, at), <Table>Block<Field>(b),
// whose offsets are baked into the emitted code) and by the descriptors
// (CookRecords, BlockRecords: the same layout AS DATA, which the dumps, the
// forgery oracle and Open's pitch check read). This test generates a Goose
// program for each accelerator unit of the corpus that reads every field of
// every record both ways over a buffer whose every byte differs from its
// neighbours, at a row offset of 64 so an accessor that drops `at` is seen,
// and requires the two reads to agree. A moved accessor offset, a swapped
// accessor pair or a moved descriptor offset disagrees on that field by name.
//
// The checks are written per field from the IR, mirroring the accessor
// surface emitAccessors writes (a scalar, a text or bytes view, a pointer's
// deref, a nested record's offset, an array slot, a count, a presence flag);
// the VALUES compared are both read at run time by the compiled program.

var agreeUnits = []string{"tables/pointers", "tables/block"}

func TestGooseAccessorsAgreeWithDescriptors(t *testing.T) {
	t.Parallel()
	for _, dir := range agreeUnits {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			u := gooseMsgUnitDir(t, dir)
			files, err := goose.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			prog, checks := agreeProgram(u, files)
			if checks == 0 {
				t.Fatalf("%s: no accessor to check", dir)
			}
			work := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(work, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(work, "agree.goose"), []byte(prog), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimSpace(string(gooserun.Run(t, work, "agree.goose", nil)))
			want := fmt.Sprintf("agree %d", checks)
			for line := range strings.SplitSeq(out, "\n") {
				if strings.HasPrefix(line, "DISAGREE ") || strings.HasPrefix(line, "NO DESCRIPTOR ") {
					t.Errorf("%s: %s", dir, line)
				}
			}
			if !strings.HasSuffix(out, want) {
				t.Errorf("%s: the program printed %q, want it to end %q", dir, out, want)
			}
			t.Logf("%s: %d accessor reads agree with the descriptors", dir, checks)
		})
	}
}

// agreeBufferBytes holds the largest record of the unit at the row offset
// of 64, with room to spare.
func agreeBufferBytes(u *ir.Unit) int64 {
	most := int64(0)
	for _, st := range cookRecords(u) {
		most = max(most, ir.RecordLayout(u, st).Size)
	}
	if blocks := ir.Blocks(u); blocks != nil {
		for _, name := range blocks.Order {
			most = max(most, blocks.Layout(name).Size)
		}
		for _, bl := range blocks.Tables {
			most = max(most, bl.Projection.Size)
		}
	}
	return (most + 64 + 128 + 63) / 64 * 64
}

// agreeProgram is the Goose program for one unit and the number of checks it
// makes.
func agreeProgram(u *ir.Unit, files map[string][]byte) (string, int) {
	ns := goose.Namespace(u)
	rt := goose.RuntimeNamespace
	var b strings.Builder
	b.WriteString("import os;\n")
	var mods []string
	for name := range files {
		if mod, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, mod)
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	fmt.Fprintf(&b, `
fn pattern() -> u8[>..] {
    var p: u8[>..] = [];
    for i in %[2]d { p.push(((i * 131 + 7) & 0xFF) as! u8); }
    return p;
}
fn field_of(rec: const %[1]s::AccelRecord&, name: const u8[:]) -> %[1]s::AccelField {
    for f in rec.fields { if f.name == name { return f; } }
    write_stdout(str("NO DESCRIPTOR ", rec.name, ".", name, "\n"));
    exit(1);
    return rec.fields[0];
}
fn record_of(recs: const %[1]s::AccelRecord[:], name: const u8[:]) -> %[1]s::AccelRecord {
    for r in recs { if r.name == name { return r; } }
    write_stdout(str("NO DESCRIPTOR ", name, "\n"));
    exit(1);
    return recs[0];
}
fn agree(ok: bool, what: const u8[:], n: i64&) {
    n += 1;
    if !ok { write_stdout(str("DISAGREE ", what, "\n")); }
}
fn main() {
    var n: i64 = 0;
    let at: i64 = 64;
`, rt, agreeBufferBytes(u))
	checks := 0
	emit := func(format string, args ...any) {
		fmt.Fprintf(&b, "    "+format+"\n", args...)
	}
	// one record's fields, read by accessors spelled with prefix and by the
	// descriptor held in `rec`; base is the record's place ("at", or "0" for
	// a projection whose accessors take no offset)
	fields := func(what, prefix, recExpr string, fls []ir.FieldLayout, projection bool) {
		emit("{")
		emit("    let rec = %s;", recExpr)
		atArg, base := ", at", "at"
		if projection {
			atArg, base = "", "0"
		}
		for _, fl := range fls {
			f := fl.Field
			if projection && ir.BlockOutOfLine(f) {
				continue
			}
			facts := ir.BlockFieldOf(u, f, fl.Offset, projection)
			member := ns + "::" + prefix + ir.GoExportName(f.Name)
			label := what + "." + f.Name
			elem := facts.ElemSize
			if elem == 0 {
				elem = fl.Size
			}
			indexed := facts.IsArray && f.Type.Kind != ir.TBytes
			slots := []int64{-1}
			if indexed {
				slots = []int64{0}
				if facts.ArrayBound > 1 {
					slots = append(slots, 1)
				}
			}
			emit("    {")
			emit("        let fd = field_of(rec, %q);", f.Name)
			for _, i := range slots {
				emit("        {")
				idx, off := "", "fd.offset"
				if i >= 0 {
					idx = fmt.Sprintf(", %d", i)
					off = fmt.Sprintf("fd.offset + %d * fd.elem_size", i)
				}
				switch {
				case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
					// a used length the bound admits, so both reads return bytes
					emit("        var t = pattern();")
					emit("        %s::accel_put(t, %s + fd.count_offset, (if fd.array_bound > 2 { 2 } else { fd.array_bound }) as! u64, 4);", rt, base)
					emit("        agree(%s(t%s) == %s::accel_text(t, %s + fd.offset, %s + fd.count_offset, fd.array_bound), %q, n);", member, atArg, rt, base, base, label)
					checks++
				case f.Type.Pointer && f.Type.Kind == ir.TNamed:
					// a delta of eight in the slot the descriptor names: the
					// accessor must land eight bytes past that slot
					emit("        var q = pattern();")
					emit("        %s::accel_put(q, %s + %s, 8, 8);", rt, base, off)
					emit("        agree(%s(q%s%s) == %s + %s + 8, %q, n);", member, atArg, idx, base, off, label)
					checks++
				case accelRef(f) != nil:
					args := strings.TrimPrefix(atArg, ", ")
					if args == "" {
						args = strings.TrimPrefix(idx, ", ")
					} else {
						args += idx
					}
					emit("        agree(%s(%s) == %s + %s, %q, n);", member+"At", args, base, off, label)
					checks++
				default:
					slot, ok := accelSlotOf(f, elem)
					if !ok {
						break
					}
					emit("        let p = pattern();")
					v := fmt.Sprintf("%s(p%s%s)", member, atArg, idx)
					where := fmt.Sprintf("%s + %s", base, off)
					switch {
					case slot.typ == "bool":
						emit("        agree(%s == (%s::accel_u8(p, %s) != 0), %q, n);", v, rt, where, label)
					case slot.typ == "f32":
						emit("        agree(%s::accel_f32_bits(%s) == %s::accel_f32_bits(%s::accel_bits_f32(%s::accel_u32(p, %s))), %q, n);", rt, v, rt, rt, rt, where, label)
					case slot.typ == "f64":
						emit("        agree(%s::accel_f64_bits(%s) == %s::accel_f64_bits(%s::accel_bits_f64(%s::accel_u64(p, %s))), %q, n);", rt, v, rt, rt, rt, where, label)
					case strings.HasSuffix(slot.typ, "Int128"):
						emit("        let w = %s;", v)
						emit("        agree(w.lo == %s::accel_u64(p, %s) && w.hi == %s::accel_u64(p, %s + 8), %q, n);", rt, where, rt, where, label)
					case strings.HasPrefix(slot.typ, "i"):
						emit("        agree((%s as i64) == %s::accel_get_signed(p, %s, fd.elem_size), %q, n);", v, rt, where, label)
					default:
						emit("        agree((%s as u64) == %s::accel_get(p, %s, fd.elem_size), %q, n);", v, rt, where, label)
					}
					checks++
				}
				emit("        }")
			}
			if facts.Counted && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
				emit("        let pc = pattern();")
				emit("        agree((%sCount(pc%s) as i64) == %s::accel_get_signed(pc, %s + fd.count_offset, 4), %q, n);", member, atArg, rt, base, label+" count")
				checks++
			}
			if facts.Optional {
				emit("        let pp = pattern();")
				emit("        agree(%sPresent(pp%s) == (%s::accel_u8(pp, %s + fd.present_offset) != 0), %q, n);", member, atArg, rt, base, label+" present")
				checks++
			}
			emit("    }")
		}
		emit("}")
	}
	for _, st := range cookRecords(u) {
		ml := ir.RecordLayout(u, st)
		fields(st.Name, st.Name+"Row", fmt.Sprintf("record_of(%s::CookRecords, %q)", ns, st.Name), ml.Fields, false)
	}
	if blocks := ir.Blocks(u); blocks != nil && len(blocks.Tables) > 0 {
		_, index, _ := blockRecordIndex(blocks)
		for _, name := range blocks.Order {
			fields("block row "+name, name+"Row", fmt.Sprintf("%s::BlockRecords[%d]", ns, index[blockRecordKey(name, false)]), blocks.Layout(name).Fields, false)
		}
		for _, bl := range blocks.Tables {
			n := bl.Table.Name
			fields("block "+n, n+"Block", fmt.Sprintf("%s::BlockRecords[%s::%sBlockRecord]", ns, ns, n), bl.Projection.Fields, true)
		}
	}
	fmt.Fprintf(&b, "    write_stdout(str(\"agree \", n, \"\\n\"));\n}\n")
	return b.String(), checks
}
