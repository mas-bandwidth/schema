package csharp

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueTypes decides which packet declarations are emitted as STRUCTS. A
// `type` and a union are value types: no heap allocation per object, fixed
// arrays stored inline ([InlineArray]), every function taking the value by
// ref. The exception is a declaration the TABLE runtime reaches: the managed
// table surface (internal/codegen/cstable) walks those through object-typed
// descriptors that need reference semantics, so until that surface takes its
// values by ref they stay sealed classes — and since a unit with tables also
// reflects every declaration through its UnitView, every declaration of such
// a unit does. A packet declaration that contains a class-kept one stays a
// class too, so a struct never holds a reference default(T) would leave null.
func valueTypes(u *ir.Unit) map[string]bool {
	values := map[string]bool{}
	if len(u.Tables) > 0 {
		// a unit with tables also carries the table surface's UnitView, which
		// reflects EVERY declaration of the unit through object-typed
		// descriptors: all of its declarations stay classes for now
		return values
	}
	class := map[string]bool{}
	for name := range ir.TableClosure(u) {
		if _, ok := u.Structs[name]; ok {
			class[name] = true
		}
	}
	// unions reached from a class-kept declaration, through fields or arms
	var markUnion func(un *ir.Union)
	markUnion = func(un *ir.Union) {
		if class[un.Name] {
			return
		}
		class[un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				if inner, ok := v.F.Type.Ref.(*ir.Union); ok {
					markUnion(inner)
				}
			}
		}
	}
	for name := range class {
		if st, ok := u.Structs[name]; ok {
			for _, f := range st.Fields {
				if un, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
					markUnion(un)
				}
			}
		}
	}
	// upward: a declaration holding a class-kept one is class-kept
	for changed := true; changed; {
		changed = false
		for name, st := range u.Structs {
			if class[name] {
				continue
			}
			for _, f := range st.Fields {
				if f.Type.Kind == ir.TNamed && class[f.Type.Name] {
					class[name], changed = true, true
					break
				}
			}
		}
		for name, un := range u.Unions {
			if class[name] {
				continue
			}
			for _, v := range un.Variants {
				if v.Type != "" && class[v.Type] {
					class[name], changed = true, true
					break
				}
			}
		}
	}
	for name := range u.Structs {
		if !class[name] {
			values[name] = true
		}
	}
	for name := range u.Unions {
		if !class[name] {
			values[name] = true
		}
	}
	return values
}

// isValue reports whether a named declaration is emitted as a struct.
func (g *gen) isValue(name string) bool {
	return g.values[name]
}

// ownerValue reports whether the declaration being emitted is a struct.
func (g *gen) ownerValue() bool {
	return g.values[g.owner]
}

// byRef is the parameter or argument prefix for a value of the named type:
// structs travel by ref, classes by reference already.
func (g *gen) byRef(name string) string {
	if g.isValue(name) {
		return "ref "
	}
	return ""
}

// bufferType is the inline storage of a struct's fixed buffer: one generic
// [InlineArray] per length, InlineBuffer<N><T>, declared once per unit by
// emitBuffers in the protocol-id home file.
func bufferType(length int64, elem string) string {
	return fmt.Sprintf("InlineBuffer%d<%s>", length, elem)
}

// fieldBufferLength is the folded element count of a struct field's buffer,
// or 0 for a field without one.
func fieldBufferLength(f *ir.Field) int64 {
	switch {
	case f.Type.Kind == ir.TWString || f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return f.Type.Size
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	}
	return 0
}

// bufferLengths is every buffer length the unit's structs use, ascending.
func bufferLengths(u *ir.Unit, values map[string]bool) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for name := range values {
		st, ok := u.Structs[name]
		if !ok {
			continue
		}
		for _, f := range st.Fields {
			if n := fieldBufferLength(f); n > 0 && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// emitBuffers declares the unit's inline buffer types (home file only).
func (g *gen) emitBuffers() error {
	lengths := bufferLengths(g.unit, g.values)
	for _, n := range lengths {
		name := fmt.Sprintf("InlineBuffer%d", n)
		if g.unit.Structs[name] != nil || g.unit.Unions[name] != nil || g.unit.Enums[name] != nil || g.unit.Flags[name] != nil || g.unit.Tables[name] != nil {
			return fmt.Errorf("C#: declaration %s collides with the generated inline buffer type of that name; rename it", name)
		}
	}
	if len(lengths) == 0 {
		return nil
	}
	g.needsCompiler = true // [InlineArray]
	g.tf("// Inline fixed buffers: a struct's arrays, strings and bytes live inside it.\n")
	for _, n := range lengths {
		g.tf("[InlineArray(%d)]\npublic struct InlineBuffer%d<T>\n{\n    private T element0;\n}\n\n", n, n)
	}
	return nil
}

// clearBuffer renders the statement zeroing a whole buffer of the owner.
func (g *gen) clearBuffer(name, length string) string {
	if g.ownerValue() {
		return fmt.Sprintf("%s[..].Clear();", name)
	}
	g.needsSystem = true
	return fmt.Sprintf("Array.Clear(%s, 0, %s);", name, length)
}

// prefix renders the span over a buffer's first n elements.
func (g *gen) prefix(name, n string) string {
	if g.ownerValue() {
		return fmt.Sprintf("%s[..%s]", name, n)
	}
	g.needsSystem = true
	return fmt.Sprintf("%s.AsSpan(0, %s)", name, n)
}
