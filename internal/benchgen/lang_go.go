package benchgen

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { renderers["go"] = renderGo }

type goRender struct {
	u        *ir.Unit
	needsSer bool
}

func renderGo(u *ir.Unit, shapes []*Shape) (string, []byte) {
	g := &goRender{u: u}
	var body buf
	for _, s := range declOrder(u, shapes) {
		name := s.Struct.Name
		body.nl()
		body.line("// Pin%s fills a zero instance with testdata/wire/%s.bin's own values", name, s.Golden)
		body.line("// — the §1.5 oracle instance, %d wire bytes.", s.Bytes)
		body.line("func Pin%s(v *%s) {", name, name)
		body.in()
		g.pin(&body, "v.", s.Nodes)
		body.out()
		body.line("}")
		body.nl()
		body.line("// Vary%s applies the §2.2 LCG mapping: value fields only, structure held", name)
		body.line("// fixed so bytes/op never moves.")
		body.line("func Vary%s(v *%s, rng uint64) {", name, name)
		body.in()
		g.vary(&body, "v.", s.Nodes, "", 0)
		body.out()
		body.line("}")
	}

	var w buf
	w.raw(header("//", u.Package))
	w.nl()
	w.line("package %s", u.Package)
	if g.needsSer {
		w.nl()
		w.line("import \"github.com/mas-bandwidth/serialize.go\"")
	}
	w.raw(body.String())
	// gofmt the result: this file lands in the repo's own Go tree, where the
	// format gate applies to generated and hand-written source alike
	src := []byte(w.String())
	if pretty, err := format.Source(src); err == nil {
		src = pretty
	}
	return "BenchHarness.go", src
}

func (g *goRender) name(f *ir.Field) string { return ir.GoExportName(f.Name) }

// ---- pin ----

func (g *goRender) pin(w *buf, prefix string, nodes []*Node) {
	for _, n := range nodes {
		name := g.name(n.F)
		switch n.Kind {
		case NScalar:
			w.line("%s%s = %s", prefix, name, g.lit(n.F, n.Val))
		case NStruct:
			g.pin(w, prefix+name+".", n.Sub)
		case NUnion:
			u := n.F.Type.Ref.(*ir.Union)
			if n.Arm == nil {
				w.line("%s%s.Type = %sTypeNone", prefix, name, u.Name)
				continue
			}
			w.line("%s%s.Type = %sType%s", prefix, name, u.Name, ir.GoExportName(n.Arm.Name))
			g.pin(w, prefix+name+"."+ir.GoExportName(n.Arm.Name)+".", n.Sub)
		case NString, NBytes:
			if n.Count > 0 {
				w.line("copy(%s%s[:], %s)", prefix, name, goBytes(n.Buf))
			}
			w.line("%s%s = %d", prefix, ir.GoExportName(lengthName(n.F)), n.Count)
		case NArrayScalar:
			if n.Counted {
				w.line("%s%s = %d", prefix, ir.GoExportName(countName(n.F)), n.Count)
			}
			for i, el := range n.Elems {
				w.line("%s%s[%d] = %s", prefix, name, i, g.lit(n.F, el.Val))
			}
		case NArrayStruct:
			if n.Counted {
				w.line("%s%s = %d", prefix, ir.GoExportName(countName(n.F)), n.Count)
			}
			for i, el := range n.Elems {
				g.pin(w, fmt.Sprintf("%s%s[%d].", prefix, name, i), el.Sub)
			}
		}
	}
}

func (g *goRender) lit(f *ir.Field, v Value) string {
	switch v.Kind {
	case VBool:
		if v.B {
			return "true"
		}
		return "false"
	case VF32:
		return floatLit(float64(v.F32), 32)
	case VF64:
		return floatLit(v.F64, 64)
	case VEnum:
		e := f.Type.Ref.(*ir.Enum)
		if n := int(v.I.Int64()); n >= 1 && n <= len(e.Variants) {
			return e.Name + ir.GoExportName(e.Variants[n-1])
		}
		if v.I.Sign() == 0 {
			return e.Name + "None"
		}
		return fmt.Sprintf("%s(%s)", e.Name, v.I)
	case VFlags:
		return fmt.Sprintf("%s(0x%x)", f.Type.Ref.(*ir.Flags).Name, v.I)
	case VWide:
		g.needsSer = true
		u := wide128(v.I)
		t := "Uint128"
		if v.Signed {
			t = "Int128"
		}
		return fmt.Sprintf("serialize.%s{Lo: 0x%x, Hi: 0x%x}", t, lo64(u), hi64(u))
	}
	return intLit(v)
}

// intLit prints wide UNSIGNED values in hex — the spelling a wire constant is
// read in — and everything else in decimal.
func intLit(v Value) string {
	if !v.Signed && v.I.Sign() > 0 && v.I.BitLen() > 16 {
		return fmt.Sprintf("0x%x", v.I)
	}
	return v.I.String()
}

func goBytes(b []byte) string {
	var parts []string
	for _, c := range b {
		parts = append(parts, fmt.Sprintf("0x%02x", c))
	}
	return "[]byte{" + strings.Join(parts, ", ") + "}"
}

// ---- vary ----

func (g *goRender) vary(w *buf, prefix string, nodes []*Node, idxVar string, depth int) {
	for _, n := range nodes {
		name := g.name(n.F)
		switch n.Kind {
		case NScalar:
			if n.Vary == nil {
				continue
			}
			w.line("%s%s = %s", prefix, name, g.varyExpr(n.F, n.Vary, idxVar))
		case NStruct:
			g.vary(w, prefix+name+".", n.Sub, idxVar, depth)
		case NUnion:
			if n.Arm != nil {
				g.vary(w, prefix+name+"."+ir.GoExportName(n.Arm.Name)+".", n.Sub, idxVar, depth)
			}
		case NString:
			if n.Vary != nil {
				w.line("%s%s[%d] = %s", prefix, name, n.Count-1, g.varyExpr(n.F, n.Vary, idxVar))
			}
		case NBytes:
			if n.Vary != nil {
				w.line("%s%s[0] = %s", prefix, name, g.varyExpr(n.F, n.Vary, idxVar))
			}
		case NArrayScalar:
			if len(n.Elems) > 0 && n.Elems[0].Vary != nil {
				w.line("%s%s[0] = %s", prefix, name, g.varyExpr(n.F, n.Elems[0].Vary, idxVar))
			}
		case NArrayStruct:
			if len(n.Elems) == 0 || !hasVary(n.Elems[0].Sub) {
				continue
			}
			v := fmt.Sprintf("i%d", depth)
			el := fmt.Sprintf("e%d", depth)
			w.line("for %s := 0; %s < %d; %s++ {", v, v, n.Count, v)
			w.in()
			w.line("%s := &%s%s[%s]", el, prefix, name, v)
			g.vary(w, el+".", n.Elems[0].Sub, v, depth+1)
			w.out()
			w.line("}")
		}
	}
}

// hasVary reports whether a subtree carries any mapping — an element the plan
// left entirely pinned needs no loop at all.
func hasVary(nodes []*Node) bool {
	for _, n := range nodes {
		if n.Vary != nil {
			return true
		}
		if hasVary(n.Sub) {
			return true
		}
		for _, el := range n.Elems {
			if el.Vary != nil || hasVary(el.Sub) {
				return true
			}
		}
	}
	return false
}

// draw renders the masked LCG extraction shared by every integral form, in the
// C-family spelling nearly every leg shares.
func draw(v *VarySpec, idxVar string) string {
	shift := varyShift(v, idxVar)
	term := "rng"
	if shift != "0" {
		term = fmt.Sprintf("rng >> %s", shift)
	}
	if v.FullU64 {
		return term
	}
	if term != "rng" {
		term = "(" + term + ")"
	}
	return fmt.Sprintf("%s & %s", term, hexMask(v.Mask))
}

// paren wraps a draw where the surrounding expression needs it.
func paren(s string) string {
	if strings.ContainsAny(s, " ") {
		return "(" + s + ")"
	}
	return s
}

func (g *goRender) varyExpr(f *ir.Field, v *VarySpec, idxVar string) string {
	t := g.goType(f)
	d := draw(v, idxVar)
	switch v.Form {
	case VaryBool:
		return fmt.Sprintf("%s != 0", d)
	case VaryF32Raw:
		return fmt.Sprintf("float32(%s)", d)
	case VaryF64Raw:
		return fmt.Sprintf("float64(%s) * 0.5", d)
	case VaryCompressed:
		return fmt.Sprintf("%s + float32(%s)*%s", floatLit(v.FMin, 32), d, floatLit(v.FScale, 32))
	case VaryWide:
		g.needsSer = true
		if v.Signed {
			return fmt.Sprintf("serialize.Int128{Lo: %s, Hi: 0}", d)
		}
		return "serialize.Uint128{Lo: rng, Hi: rng >> 1}"
	case VaryAscii:
		return fmt.Sprintf("byte(65 + %s)", paren(d))
	case VaryRanged:
		if v.Min != nil && v.Min.Sign() != 0 {
			return fmt.Sprintf("%s(%s)%s", t, d, offset(v.Min))
		}
	}
	return fmt.Sprintf("%s(%s)", t, d)
}

// goType mirrors the Go backend's storage mapping (SPEC §6.1) — the emitted
// harness must cast to exactly the type the generated struct declares.
func (g *goRender) goType(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TBits:
		if t.Width <= 32 {
			return "uint32"
		}
		return "uint64"
	case ir.TString, ir.TBytes:
		return "byte"
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			if t.Signed {
				return "serialize.Int128"
			}
			return "serialize.Uint128"
		}
		if t.Signed {
			return fmt.Sprintf("int%d", t.Width)
		}
		return fmt.Sprintf("uint%d", t.Width)
	case ir.TNamed:
		return t.Name
	}
	return "uint64"
}
