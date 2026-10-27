package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitNestedMeasureHelper emits the schema-guided extent walk used by a
// variable root and every node body it can place. It follows BY-VALUE bodies
// only; pointer indices are never followed on load (SPEC-TABLES §3.1, §6.5).
func (g *gen) emitNestedMeasureHelper(b *strings.Builder, root *ir.Struct) {
	_ = root
	fmt.Fprint(b, `    // one arm per by-value edge the schema declares: a unit with none, or
    // one, keeps a match of that size, and the walk does not move at
    #[allow(unused_mut, clippy::match_single_binding, clippy::single_match)]
    fn nested_measure(tid: u64, wire: &[u8], ids: &[u64], mut at: i64) -> i64 {
        let mut off = 0usize;
        while off < wire.len() {
            let Some(r) = leb_read(wire, &mut off) else { break; };
            if r == 0 { break; }
            let Some(id) = ref_id(ids, r) else { break; };
            if off >= wire.len() { break; }
            let kind = wire[off]; off += 1;
            match (tid, id) {
`)
	for _, st := range g.measureStructs() {
		tid := ir.TableWireId(st.WireName())
		for _, f := range st.Fields {
			fid := ir.TableFieldWireId(f)
			switch {
			case f.IsMap() || f.Array != ir.ArrayNone || f.KeyEnum != "":
				g.emitMeasureArrayCase(b, tid, fid, f)
			case classOf(f) == vTable:
				child, ok := f.Type.Ref.(*ir.Struct)
				if !ok {
					continue
				}
				fmt.Fprintf(b, "                (0x%016x, 0x%016x) if kind == %d => {\n", tid, fid, ir.TableKindTable)
				fmt.Fprintf(b, "                    let Some(n) = leb_read(wire, &mut off) else { break; }; if n > (wire.len() - off) as u64 { break; } let sub = off + n as usize;\n")
				fmt.Fprintf(b, "                    at = nested_measure(0x%016x, &wire[off..sub], ids, at); if at < 0 { return -1; } off = sub;\n", ir.TableWireId(child.WireName()))
				fmt.Fprint(b, "                }\n")
			case classOf(f) == vUnion:
				g.emitMeasureUnionCase(b, tid, fid, f)
			}
		}
	}
	fmt.Fprint(b, `                _ => { if !leb_skip(wire, &mut off, wire.len(), kind) { break; } }
            }
        }
        at
    }
`)
}

func (g *gen) measureStructs() []*ir.Struct {
	var names []string
	for name := range g.unit.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[uint64]bool{}
	var out []*ir.Struct
	var add func(*ir.Struct)
	add = func(st *ir.Struct) {
		if st == nil {
			return
		}
		id := ir.TableWireId(st.WireName())
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, st)
		for _, f := range st.Fields {
			if f.IsMap() {
				add(f.MapEntry)
			}
		}
	}
	for _, name := range names {
		add(g.unit.Tables[name])
	}
	return out
}

func (g *gen) emitMeasureArrayCase(b *strings.Builder, tid, fid uint64, f *ir.Field) {
	want := ir.TableWireElemKind(f)
	if f.IsMap() {
		want = ir.TableKindTable
	}
	fmt.Fprintf(b, "                (0x%016x, 0x%016x) if kind == %d => {\n", tid, fid, fieldWireKind(f))
	fmt.Fprint(b, `                    let Some(n) = leb_read(wire, &mut off) else { break; };
                    if n > (wire.len() - off) as u64 { break; }
                    let sub = off + n as usize;
                    if sub - off >= 2 {
                        let ek = wire[off]; off += 1;
`)
	var child *ir.Struct
	if f.IsMap() {
		child = f.MapEntry
	} else if s, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
		child = s
	}
	if f.IsMap() || f.IsList() || child != nil {
		fmt.Fprint(b, "                        let Some(count) = leb_read(&wire[..sub], &mut off) else { off = sub; continue; };\n")
	} else {
		fmt.Fprint(b, "                        if leb_read(&wire[..sub], &mut off).is_none() { off = sub; continue; }\n")
	}

	fmt.Fprintf(b, "                        if ek == %d || table_kind_widens(ek, %d) {\n", want, want)
	if f.IsMap() || f.IsList() {
		var size, align int64
		if f.IsMap() {
			layout := ir.RecordLayout(g.unit, f.MapEntry)
			size, align = layout.Size, layout.Align
		} else {
			size, align = ir.ListElementLayout(g.unit, f)
		}
		fmt.Fprint(b, `                            if count > 0x7fffffff { return -1; }
                            let floor = if ek == 13 { 2 } else { table_kind_width(ek).max(1) };
                            if off > sub || count as usize > (sub - off) / floor { return -1; }
`)
		fmt.Fprintf(b, "                            %sat += %s;\n", alignStep(align), scaled("count as i64", size))
	}
	if child != nil {
		fmt.Fprint(b, "                            for _ in 0..count {\n")
		if f.KeyEnum != "" {
			fmt.Fprint(b, "                                if leb_read(&wire[..sub], &mut off).is_none() { break; }\n")
		}
		fmt.Fprint(b, "                                if off >= sub { break; } let Some(el) = leb_read(&wire[..sub], &mut off) else { break; }; if el > (sub - off) as u64 { break; }\n")
		fmt.Fprintf(b, "                                at = nested_measure(0x%016x, &wire[off..off + el as usize], ids, at); if at < 0 { return -1; } off += el as usize;\n", ir.TableWireId(child.WireName()))
		fmt.Fprint(b, "                            }\n")
	}
	fmt.Fprint(b, `                        }
                    }
                    off = sub;
                }
`)
}

func (g *gen) emitMeasureUnionCase(b *strings.Builder, tid, fid uint64, f *ir.Field) {
	un, ok := f.Type.Ref.(*ir.Union)
	if !ok {
		return
	}
	fmt.Fprintf(b, "                (0x%016x, 0x%016x) if kind == %d => {\n", tid, fid, ir.TableKindUnion)
	fmt.Fprint(b, `                    let Some(arm) = leb_read(wire, &mut off) else { break; };
                    if arm == 0 { continue; }
                    let Some(aid) = ref_id(ids, arm) else { break; };
                    if off >= wire.len() { break; } off += 1;
                    let Some(n) = leb_read(wire, &mut off) else { break; };
                    if n > (wire.len() - off) as u64 { break; }
                    let sub = off + n as usize;
                    match aid {
`)
	for _, v := range un.Variants {
		if v.F == nil || v.F.Type.Pointer {
			continue
		}
		child, ok := v.F.Type.Ref.(*ir.Struct)
		if !ok {
			continue
		}
		fmt.Fprintf(b, "                        0x%016x => { at = nested_measure(0x%016x, &wire[off..sub], ids, at); if at < 0 { return -1; } }\n", ir.TableWireId(v.WireName()), ir.TableWireId(child.WireName()))
	}
	fmt.Fprint(b, `                        _ => {}
                    }
                    off = sub;
                }
`)
}

// alignStep rounds the running extent `at` up to align, or is empty when the
// alignment is 1 and the rounding would be an identity (clippy's identity_op).
func alignStep(align int64) string {
	if align <= 1 {
		return ""
	}
	return fmt.Sprintf("at = (at + %d - 1) & -%d; ", align, align)
}

// scaled renders n * size, or n alone when size is 1 (clippy's identity_op).
func scaled(n string, size int64) string {
	if size == 1 {
		return n
	}
	return fmt.Sprintf("%s * %d", n, size)
}
