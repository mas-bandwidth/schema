package pythontable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is a locked layout. Record includes the eight hash bytes;
// the compiler converts the lock's body size exactly once before handing it on.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
	Ranges  []ir.TableFixedWriterRange
	Digest  []byte
	Writer  *ir.Struct
}

type fixedLiteral string

// GenerateLineage generates the ordinary classes and the fixed-form plans from
// caller-supplied locked layouts. No generated reader parses a file's layout.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	return generatePythonTables(u, lineage)
}

func fixedLiteralOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case fixedLiteral:
		return string(x)
	case string:
		return strconv.Quote(x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	case []any:
		a := make([]string, len(x))
		for i, v := range x {
			a[i] = fixedLiteralOf(v)
		}
		return "[" + strings.Join(a, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		a := make([]string, 0, len(keys))
		for _, k := range keys {
			a = append(a, strconv.Quote(k)+":"+fixedLiteralOf(x[k]))
		}
		return "{" + strings.Join(a, ",") + "}"
	case float64:
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s
	default:
		return fmt.Sprint(v)
	}
}

func fixedRootAdmitted(u *ir.Unit, st *ir.Struct) bool { return ir.TableFixedEmitted(u, st) }

func fixedSemanticField(f *ir.Field) map[string]any {
	spec := map[string]any{"name": f.Name, "id": ir.TableFieldWireId(f), "size": ir.TableFixedFieldBytes(f)}
	if f.Type.Optional {
		inner := *f
		inner.Type.Optional = false
		spec["kind"] = 35
		spec["shape"] = "optional"
		spec["children"] = []any{fixedSemanticField(&inner)}
		return spec
	}
	if f.KeyEnum != "" {
		elem := *f
		elem.KeyEnum = ""
		elem.KeyEnumRef = nil
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.ArrayMin = 0
		keys := make([]any, len(f.KeyEnumRef.Variants))
		for i := range keys {
			keys[i] = ir.TableWireId(f.KeyEnumRef.VariantWireName(i))
		}
		spec["kind"] = 16
		spec["shape"] = "keyed"
		spec["bound"] = len(keys)
		spec["keys"] = keys
		spec["children"] = []any{fixedSemanticField(&elem)}
		return spec
	}
	if f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted {
		elem := *f
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.ArrayMin = 0
		spec["kind"] = 14
		spec["shape"] = "array"
		spec["bound"] = f.ArrayBound
		spec["minimum"] = f.ArrayMin
		spec["counted"] = f.Array == ir.ArrayCounted
		spec["children"] = []any{fixedSemanticField(&elem)}
		return spec
	}
	spec["kind"] = ir.TableFixedLeafKind(f)
	switch f.Type.Kind {
	case ir.TBool:
		spec["shape"] = "bool"
		spec["default"] = f.HasDefault && f.DefBool
	case ir.TInt, ir.TBits, ir.TFixed:
		spec["shape"] = "int"
		spec["signed"] = f.Type.Signed
		if f.Type.Kind == ir.TBits {
			spec["signed"] = false
		}
		lo, hi := intRange(f)
		spec["lo"] = fixedLiteral(lo)
		spec["hi"] = fixedLiteral(hi)
		d := big.NewInt(0)
		if f.HasDefault && f.DefInt != nil {
			d.Set(f.DefInt)
		}
		spec["default"] = fixedLiteral(d.String())
	case ir.TFloat32, ir.TFloat64:
		spec["shape"] = "float"
		value, lo, hi := f.DefFloat, f.FMin, f.FMax
		if f.Type.Kind == ir.TFloat32 {
			value, lo, hi = float64(float32(value)), float64(float32(lo)), float64(float32(hi))
		}
		spec["default"] = value
		if f.HasFloatRange {
			spec["lo"], spec["hi"], spec["ranged"] = lo, hi, true
		}
	case ir.TString:
		spec["shape"] = "string"
		spec["bound"] = f.Type.Size
		spec["default"] = string(f.DefBytes)
	case ir.TBytes:
		spec["kind"] = 14
		spec["shape"] = "bytes"
		spec["bound"] = f.Type.Size
		spec["default"] = fixedLiteral("bytes.fromhex(" + strconv.Quote(hex.EncodeToString(f.DefBytes)) + ")")
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			spec["kind"] = 13
			spec["shape"] = "table"
			spec["target"] = r.Name
			fields := fixedSemanticFields(r)
			spec["children"] = fields
		case *ir.Union:
			spec["kind"] = 15
			spec["shape"] = "union"
			spec["target"] = r.Name
			spec["tag"] = ir.TableFixedUnionTagBytes(r)
			arms := make([]any, 0, len(r.Variants))
			for _, v := range r.Variants {
				arm := map[string]any{"kind": 32, "shape": "void", "size": int64(0)}
				if v.F != nil {
					arm = fixedSemanticField(v.F)
				}
				arm["id"] = ir.TableWireId(v.WireName())
				arm["name"] = v.Name
				arms = append(arms, arm)
			}
			spec["children"] = arms
		case *ir.Enum:
			spec["kind"] = 30
			spec["shape"] = "enum"
			spec["default"] = enumDefaultValue(f)
			ids := make([]any, len(r.Variants))
			deprecated := []any{}
			for i := range ids {
				ids[i] = ir.TableWireId(r.VariantWireName(i))
				if i < len(r.VariantTags) {
					for _, tag := range r.VariantTags[i] {
						if tag == "deprecated" {
							deprecated = append(deprecated, i+1)
						}
					}
				}
			}
			spec["variants"] = ids
			spec["deprecated"] = deprecated
		case *ir.Flags:
			spec["shape"] = "int"
			spec["signed"] = false
			spec["lo"] = 0
			spec["hi"] = fixedLiteral("18446744073709551615")
			def := big.NewInt(0)
			if f.HasDefault && f.DefInt != nil {
				def.Set(f.DefInt)
			}
			spec["default"] = fixedLiteral(def.String())
		}
	}
	return spec
}

func fixedSemanticFields(st *ir.Struct) []any {
	fields := make([]any, 0, len(st.Fields))
	for _, f := range st.Fields {
		spec := fixedSemanticField(f)
		if !st.IsTable && f.KeyEnum != "" {
			inner := spec
			if f.Type.Optional {
				inner = fixedChildren(spec)[0].(map[string]any)
			}
			inner["positional"] = true
		}
		fields = append(fields, spec)
	}
	return fields
}

func fixedSemanticRoot(st *ir.Struct) map[string]any {
	return map[string]any{"name": st.Name, "target": st.Name, "id": ir.TableWireId(st.WireName()), "kind": 13, "shape": "table", "size": ir.TableFixedTypeBytes(st), "children": fixedSemanticFields(st)}
}

type fixedLayoutNode struct {
	ID       uint64
	Kind     int
	Size     int64
	Children []*fixedLayoutNode
}

func parsePythonFixedLayout(data []byte) (*fixedLayoutNode, string) {
	if why := lockfile.ValidateLayout(data); why != "" {
		return nil, "layout_malformed"
	}
	at := 0
	var walk func() *fixedLayoutNode
	walk = func() *fixedLayoutNode {
		b := data[4+at*17:]
		at++
		n := &fixedLayoutNode{ID: binary.LittleEndian.Uint64(b), Kind: int(b[8]), Size: int64(binary.LittleEndian.Uint32(b[9:]))}
		for range binary.LittleEndian.Uint32(b[13:]) {
			n.Children = append(n.Children, walk())
		}
		return n
	}
	return walk(), ""
}

func fixedNodeData(n *fixedLayoutNode) map[string]any {
	children := make([]any, 0, len(n.Children))
	for _, c := range n.Children {
		children = append(children, fixedNodeData(c))
	}
	return map[string]any{"id": n.ID, "kind": n.Kind, "size": n.Size, "children": children}
}

// emitPythonFixedData lays schema metadata, locked layouts and precompiled
// image-domain plans beside the classes. Importing this module only registers
// data; a load never parses or matches a caller's layout tree.
func emitPythonFixedData(u *ir.Unit, lineage map[string][]FixedLineageEntry) (string, error) {
	roots := ir.TableFixedFormRoots(u)
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	data := map[string]any{}
	for _, st := range roots {
		if !fixedRootAdmitted(u, st) {
			continue
		}
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		hash := ir.TableFixedLayoutHash(layout, st)
		own := FixedLineageEntry{Wire: hash, Layout: layout, Record: 8 + ir.TableFixedTypeBytes(st)}
		entries := append([]FixedLineageEntry(nil), lineage[st.Name]...)
		if len(entries) == 0 || entries[len(entries)-1].Wire != hash {
			entries = append(entries, own)
		}
		floor := 0
		for i, e := range entries {
			if e.Retired {
				floor = i + 1
			}
		}
		spec := fixedSemanticRoot(st)
		known := make([]any, 0, len(entries))
		planMax := 0
		for i, e := range entries {
			entry := map[string]any{"hash": e.Wire, "layout": hex.EncodeToString(e.Layout), "record": e.Record, "refusal": ""}
			if i < floor {
				entry["refusal"] = "layout_unsupported"
				known = append(known, entry)
				continue
			}
			node, why := parsePythonFixedLayout(e.Layout)
			if why != "" || node == nil {
				entry["refusal"] = "layout_malformed"
				known = append(known, entry)
				continue
			}
			if e.Record != 8+node.Size {
				entry["refusal"] = "layout_malformed"
				known = append(known, entry)
				continue
			}
			fills := []any{}
			plan, reason := compilePythonFixedPlan(node, spec, e.Wire == hash, &fills)
			entry["fills"] = fills
			entry["refusal"], entry["source"], entry["plan"] = reason, fixedNodeData(node), plan
			if reason != "" {
				known = append(known, entry)
				continue
			}
			if e.Writer != nil {
				writerLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(e.Writer))
				if string(writerLayout) != string(e.Layout) || ir.TableFixedLayoutHash(writerLayout, e.Writer) != e.Wire {
					entry["refusal"] = "layout_malformed"
					known = append(known, entry)
					continue
				}
				entry["check_spec"] = fixedSemanticRoot(e.Writer)
			} else if e.Wire != hash && e.Wire != ir.TableFixedLayoutHash(e.Layout, nil) {
				// Historical definition facts are not reconstructible from
				// anonymous digest records. Never validate them against this
				// reader's wider declaration as if that were the writer's.
				return "", fmt.Errorf("python: fixed table %s: historical layout 0x%016x needs its writer declaration snapshot; the lock lineage does not retain enough historical definition facts", st.Name, e.Wire)
			}
			planMax = max(planMax, len(plan)+len(fills))
			ranges := map[string]any{}
			for _, r := range e.Ranges {
				ranges[r.Name] = []any{r.Lo, r.Hi}
			}
			entry["ranges"] = ranges
			known = append(known, entry)
		}
		data[st.Name] = map[string]any{"hash": hash, "layout": hex.EncodeToString(layout), "body": own.Record - 8, "spec": spec, "floor": floor, "known": known, "plan_max": planMax}
	}
	return "# Code generated by the schema compiler. DO NOT EDIT.\nfrom ._schema.fixedform import fixed_register as _s_register\n_s_register(" + fixedLiteralOf(data) + ")\n", nil
}

func fixedChildren(spec map[string]any) []any { c, _ := spec["children"].([]any); return c }
func fixedInt(spec map[string]any, key string) int64 {
	switch v := spec[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}
func fixedSigned(kind int) bool {
	return kind >= 2 && kind <= 5 || kind == 18 || kind >= 20 && kind <= 24
}

// compilePythonFixedPlan works wholly at generate time in the record-image
// domain. The identity is one whole-body copy; older layouts get coalesced
// copy/widen/ordinal/constant operations, partitioned by their guard chains.
func compilePythonFixedPlan(writer *fixedLayoutNode, reader map[string]any, identity bool, fills *[]any) ([]any, string) {
	if identity {
		return []any{map[string]any{"op": "copy", "src": int64(0), "dst": int64(0), "size": writer.Size, "guards": []any{}}}, ""
	}
	ops := []any{}
	why := ""
	emit := func(op string, src, dst, size, width int64, guards []any, extra map[string]any) {
		p := map[string]any{"op": op, "src": src, "dst": dst, "size": size, "width": width, "guards": append([]any{}, guards...)}
		maps.Copy(p, extra)
		ops = append(ops, p)
	}
	var walk func(*fixedLayoutNode, map[string]any, int64, int64, []any, bool, int)
	walk = func(w *fixedLayoutNode, r map[string]any, src, dst int64, guards []any, localDefaults bool, depth int) {
		if why != "" {
			return
		}
		if depth > ir.TableFixedMaxDepth {
			why = "layout_malformed"
			return
		}
		rk := int(fixedInt(r, "kind"))
		rs := fixedInt(r, "size")
		shape, _ := r["shape"].(string)
		if rk == 35 && w.Kind != 35 {
			emit("const", 0, dst, 1, 0, guards, map[string]any{"value": 1})
			walk(w, fixedChildren(r)[0].(map[string]any), src, dst+1, guards, true, depth+1)
			return
		}
		if rk != w.Kind {
			if fixedPythonWidens(w.Kind, rk) && w.Size <= rs {
				op := "widen"
				if w.Kind == 10 && rk == 11 {
					op = "widenf"
				}
				emit(op, src, dst, w.Size, rs, guards, map[string]any{"signed": fixedSigned(w.Kind)})
				return
			}
			why = "layout_malformed"
			return
		}
		switch shape {
		case "table":
			theirs := map[uint64]struct {
				node *fixedLayoutNode
				at   int64
			}{}
			at := src
			for _, c := range w.Children {
				if _, dup := theirs[c.ID]; dup {
					why = "layout_malformed"
					return
				}
				theirs[c.ID] = struct {
					node *fixedLayoutNode
					at   int64
				}{c, at}
				at += c.Size
			}
			mine := map[uint64]bool{}
			at = dst
			for _, v := range fixedChildren(r) {
				c := v.(map[string]any)
				id := c["id"].(uint64)
				mine[id] = true
				if hit, ok := theirs[id]; ok {
					walk(hit.node, c, hit.at, at, guards, localDefaults, depth+1)
				} else if localDefaults {
					*fills = append(*fills, map[string]any{"dst": at, "size": fixedInt(c, "size"), "guards": append([]any{}, guards...), "spec": c})
				}
				at += fixedInt(c, "size")
			}
			for id := range theirs {
				if !mine[id] {
					why = "layout_malformed"
					return
				}
			}
		case "optional":
			if len(w.Children) != 1 {
				why = "layout_malformed"
				return
			}
			emit("copy", src, dst, 1, 0, guards, nil)
			gs := append(append([]any{}, guards...), []any{src, int64(1), int64(1), "eq"})
			walk(w.Children[0], fixedChildren(r)[0].(map[string]any), src+1, dst+1, gs, true, depth+1)
		case "array", "keyed":
			elem := fixedChildren(r)[0].(map[string]any)
			es := fixedInt(elem, "size")
			if es <= 0 {
				why = "layout_malformed"
				return
			}
			head := int64(0)
			ei := 0
			if shape == "keyed" {
				ei = 1
			}
			if len(w.Children) <= ei {
				why = "layout_malformed"
				return
			}
			we := w.Children[ei]
			if we.Size <= 0 {
				why = "layout_malformed"
				return
			}
			if counted, _ := r["counted"].(bool); counted {
				head = 4
			}
			n := (w.Size - head) / we.Size
			bound := fixedInt(r, "bound")
			if n > bound || w.Size < head || (w.Size-head)%we.Size != 0 {
				why = "layout_malformed"
				return
			}
			if shape == "keyed" {
				keys, _ := r["keys"].([]any)
				if len(w.Children) != 2 || len(w.Children[0].Children) > len(keys) {
					why = "layout_malformed"
					return
				}
				for i, k := range w.Children[0].Children {
					if keys[i].(uint64) != k.ID {
						why = "layout_malformed"
						return
					}
				}
			}
			if head != 0 {
				emit("count", src, dst, n, 4, guards, nil)
			}
			for i := range n {
				gs := guards
				if head != 0 {
					gs = append(append([]any{}, guards...), []any{src, int64(4), i, "gt"})
				}
				walk(we, elem, src+head+i*we.Size, dst+head+i*es, gs, localDefaults || head != 0, depth+1)
			}
			if localDefaults && head == 0 {
				for i := n; i < bound; i++ {
					*fills = append(*fills, map[string]any{"dst": dst + i*es, "size": es, "guards": append([]any{}, guards...), "spec": elem})
				}
			}
		case "bytes":
			if len(w.Children) != 1 || w.Children[0].Kind != 6 || w.Children[0].Size != 1 || w.Size > rs {
				why = "layout_malformed"
				return
			}
			emit("text", src, dst, w.Size-4, 0, guards, nil)
		case "string":
			if w.Size > rs {
				why = "layout_malformed"
				return
			}
			emit("text", src, dst, w.Size-4, 0, guards, nil)
		case "union":
			arms := fixedChildren(r)
			if len(w.Children) > len(arms) {
				why = "layout_malformed"
				return
			}
			widest := int64(0)
			for _, c := range w.Children {
				widest = max(widest, c.Size)
			}
			tag := w.Size - widest
			mytag := fixedInt(r, "tag")
			emit("const", 0, dst, mytag, 0, guards, map[string]any{"value": 0})
			for i, c := range w.Children {
				arm := arms[i].(map[string]any)
				if arm["id"].(uint64) != c.ID {
					why = "layout_malformed"
					return
				}
				gs := append(append([]any{}, guards...), []any{src, tag, int64(i + 1), "eq"})
				emit("const", 0, dst, mytag, 0, gs, map[string]any{"value": i + 1})
				walk(c, arm, src+tag, dst+mytag, gs, true, depth+1)
			}
		case "enum":
			variants, _ := r["variants"].([]any)
			if len(w.Children) > len(variants) || w.Size > rs {
				why = "layout_malformed"
				return
			}
			remap := []any{0}
			for i, c := range w.Children {
				if variants[i].(uint64) != c.ID {
					why = "layout_malformed"
					return
				}
				remap = append(remap, i+1)
			}
			emit("ordinal", src, dst, w.Size, rs, guards, map[string]any{"remap": remap})
		case "void":
		default:
			if w.Size == rs {
				emit("copy", src, dst, w.Size, 0, guards, nil)
			} else if w.Size < rs && rs <= 16 {
				emit("widen", src, dst, w.Size, rs, guards, map[string]any{"signed": false})
			} else {
				why = "layout_malformed"
			}
		}
	}
	walk(writer, reader, 0, 0, nil, false, 0)
	if why != "" {
		return nil, why
	}
	// The unguarded run precedes the guarded run. Coalescing is bounded by the
	// split and never joins entries with different guard chains.
	ordered := make([]any, 0, len(ops))
	for _, guarded := range []bool{false, true} {
		for _, v := range ops {
			p := v.(map[string]any)
			if (len(p["guards"].([]any)) > 0) == guarded {
				ordered = append(ordered, p)
			}
		}
	}
	out := []any{}
	for _, v := range ordered {
		p := v.(map[string]any)
		if len(out) > 0 && p["op"] == "copy" {
			prev := out[len(out)-1].(map[string]any)
			if prev["op"] == "copy" && fixedInt(prev, "src")+fixedInt(prev, "size") == fixedInt(p, "src") && fixedInt(prev, "dst")+fixedInt(prev, "size") == fixedInt(p, "dst") && fixedLiteralOf(prev["guards"]) == fixedLiteralOf(p["guards"]) {
				prev["size"] = fixedInt(prev, "size") + fixedInt(p, "size")
				continue
			}
		}
		out = append(out, p)
	}
	if len(out)+len(*fills) > 4096 {
		return nil, "plan_too_large"
	}
	return out, ""
}

func fixedPythonWidens(from, to int) bool {
	return ir.TableKindWidens(from, to) || from >= 20 && from < to && to <= 24 || from >= 25 && from < to && to <= 29
}
