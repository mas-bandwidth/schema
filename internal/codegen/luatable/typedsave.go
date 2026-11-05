package luatable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE TYPED SAVE (docs/PORTING.md M22). Beside the descriptor walk that every
// Save is (runtime/save.lua, S.Save), the unit's module carries a typed body
// per table and a typed entry per declared table: T.TypedBodies[<Name>] and
// _M.SaveTyped<Name>(value). A body writes the SCALAR LEAVES of its table, the
// bool, integer, float and string fields with no guard, no presence companion
// and no `required`, straight from the value with the field's id, kind, width
// and declared default as constants in the body: no descriptor is read, no
// shape is dispatched and no default is looked up per call. Every other field
// of the table goes through the descriptor walk (S.Typed.field, the walk's own
// write_field) at its constant ordinal, so the body is the walk with its
// scalar part unrolled, and the two write the same bytes. A body refuses
// (false) where the walk does: an integer outside its width, its declared
// range or its bits(N), and a string past its bound, each tested against
// constants (the walk's D.int_ok and D.len_ok), and a string that is not
// well-formed UTF-8 (the walk's D.text_ok, through SV.Typed.utf8_ok).
func init() { RegisterPass(emitTypedSave) }

// typedIntCheck is the refusal test a typed integer leaf carries: the one interval
// its width, its declared range and its bits(N) leave, tested with Lua's
// signed compare, or math.ult for a 64-bit unsigned value. It is empty when
// the interval is the whole of what a Lua integer holds at that width.
func typedIntCheck(f *ir.Field, width int, signed bool) string {
	bits := uint(width * 8)
	lo, hi := new(big.Int), new(big.Int).Lsh(big.NewInt(1), bits)
	if signed {
		lo.Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
		hi.Lsh(big.NewInt(1), bits-1)
	}
	hi.Sub(hi, big.NewInt(1))
	fullLo, fullHi := new(big.Int).Set(lo), new(big.Int).Set(hi)
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		if rlo.Cmp(lo) > 0 {
			lo.Set(rlo)
		}
		if rhi.Cmp(hi) < 0 {
			hi.Set(rhi)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width > 0 && f.Type.Width < 64 {
		bmax := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		if bmax.Cmp(hi) < 0 {
			hi.Set(bmax)
		}
	}
	if width == 8 && lo.Cmp(fullLo) == 0 && hi.Cmp(fullHi) == 0 {
		return ""
	}
	if width == 8 && !signed {
		return fmt.Sprintf("math.ult(x, %s) or math.ult(%s, x)", lua.IntLiteral(lo), lua.IntLiteral(hi))
	}
	return fmt.Sprintf("x < %s or x > %s", lua.IntLiteral(lo), lua.IntLiteral(hi))
}

// typedLeaf says whether the field is one a typed body writes itself, and
// which writer it takes.
func typedLeaf(f *ir.Field, guarded bool) string {
	t := f.Type
	if guarded || f.Required || t.Optional || f.Array != ir.ArrayNone || f.MapEntry != nil || t.Pointer {
		return ""
	}
	switch t.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32, ir.TFloat64:
		return "float"
	case ir.TString:
		if t.TextUnbounded() {
			return ""
		}
		return "string"
	case ir.TWString, ir.TBytes, ir.TNamed:
		return ""
	}
	if (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width == 128 {
		return ""
	}
	return "int"
}

// typedAccess is the value's slot for a field name: .name where the name is a
// Lua name, ["name"] where it is not.
func typedAccess(name string) string {
	k := key(name)
	if strings.HasPrefix(k, "[") {
		return k
	}
	return "." + k
}

func emitTypedSave(m *Module) error {
	m.Printf("-- the typed save bodies (docs/PORTING.md M22): the scalar leaves of each table\n")
	m.Printf("-- written straight from the value, every other field through the descriptor walk\n")
	m.Printf("local SV = require(\"save\")\n")
	// the bodies are built first, so the helpers they share are declared once
	// and only the ones some body reads
	used := map[string]bool{}
	var bodies strings.Builder
	for _, st := range m.Closure {
		emitTypedBody(&bodies, st, used)
	}
	for _, h := range []struct{ use, local, value string }{
		{"head", "head", "SV.Typed.head"}, {"leb", "leb", "SV.Typed.leb"}, {"raw", "raw", "SV.Typed.raw"},
		{"float", "wfloat", "SV.Typed.float"}, {"same_float", "same", "SV.Typed.same_float"},
		{"utf8", "utf8_ok", "SV.Typed.utf8_ok"}, {"char", "char", "string.char"}, {"pack", "pack", "string.pack"}, {"field", "field", "SV.Typed.field"},
	} {
		if used[h.use] {
			m.Printf("local %s = %s\n", h.local, h.value)
		}
	}
	m.Printf("_M.TypedBodies = {}\n\n")
	m.Body.WriteString(bodies.String())
	for _, st := range m.Closure {
		if st.IsTable && !st.IsMapEntry() {
			m.Printf("function _M.SaveTyped%s(value)\n  return SV.SaveTyped(_M, %q, value)\nend\n\n", st.Name, st.Name)
		}
	}
	return nil
}

func emitTypedBody(out *strings.Builder, st *ir.Struct, used map[string]bool) {
	guards := structGuards(st)
	var body strings.Builder
	delegated := false
	for i, f := range st.Fields {
		slot := "v" + typedAccess(f.Name)
		id := idLit(ir.TableFieldWireId(f))
		kind := ir.TableWireFieldKind(f)
		switch typedLeaf(f, len(guards[f.Name]) > 0) {
		case "bool":
			used["head"], used["raw"] = true, true
			fmt.Fprintf(&body, "    do\n      local x = %s\n      if x ~= %s then\n        head(w, ids, %s, 1)\n        raw(w, x and \"\\1\" or \"\\0\")\n      end\n    end\n",
				slot, defaultExpr(f), id)
		case "int":
			used["head"], used["raw"] = true, true
			sk := ir.TableWireScalarKind(f)
			t := f.Type
			signed := (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Signed
			var value string
			check := ""
			if test := typedIntCheck(f, ir.TableKindWidth(sk), signed); test != "" {
				// a nil is the field's absence: it writes zero, as the walk does
				check = fmt.Sprintf("        if x ~= nil and (%s) then return false end\n", test)
			}
			switch w := ir.TableKindWidth(sk); w {
			case 1:
				used["char"] = true
				value = "char((x or 0) & 0xff)"
			case 2, 4:
				used["pack"] = true
				format := "<I"
				if signed {
					format = "<i"
				}
				value = fmt.Sprintf("pack(%q, x or 0)", fmt.Sprintf("%s%d", format, w))
			default:
				used["pack"] = true
				value = "pack(\"<i8\", x or 0)"
			}
			fmt.Fprintf(&body, "    do\n      local x = %s\n      if x ~= %s then\n%s        head(w, ids, %s, %d)\n        raw(w, %s)\n      end\n    end\n",
				slot, defaultExpr(f), check, id, kind, value)
		case "float":
			used["head"], used["float"], used["same_float"] = true, true, true
			fmt.Fprintf(&body, "    do\n      local x = %s\n      if not same(%v, x, %s) then\n        head(w, ids, %s, %d)\n        if not wfloat(w, %v, x) then return false end\n      end\n    end\n",
				slot, f.Type.Kind == ir.TFloat32, defaultExpr(f), id, kind, f.Type.Kind == ir.TFloat32)
		case "string":
			used["head"], used["leb"], used["raw"], used["utf8"] = true, true, true, true
			check := ""
			if f.Type.Size > 0 {
				check = fmt.Sprintf("        if #x > %d then return false end\n", f.Type.Size)
			}
			check += "        if not utf8_ok(x) then return false end\n"
			fmt.Fprintf(&body, "    do\n      local x = %s\n      if x ~= %s then\n%s        head(w, ids, %s, 12)\n        leb(w, #x)\n        raw(w, x)\n      end\n    end\n",
				slot, defaultExpr(f), check, id)
		default:
			delegated = true
			used["field"] = true
			fmt.Fprintf(&body, "    if not field(_M, ids, nodes, fds[%d], v, w) then return false end\n", i+1)
		}
	}
	nodes := "_nodes" // the walk's field line takes the nodes; a body that delegates nothing never reads them
	if delegated {
		nodes = "nodes"
		fmt.Fprintf(out, "do\n  local fds = _M.Tables[%q].fields\n", st.Name)
	} else {
		out.WriteString("do\n")
	}
	fmt.Fprintf(out, "  _M.TypedBodies[%q] = function(ids, %s, v, w)\n", st.Name, nodes)
	out.WriteString(body.String())
	out.WriteString("    return true\n  end\nend\n\n")
}
