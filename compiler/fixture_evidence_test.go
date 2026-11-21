package compiler

// What a generation-only gate over a corpus fixture has to prove before it may
// say "every target generates it" (review findings F-137, F-138, F-139 on
// #4450): that the fixture still declares the shapes the gate is named for,
// and that the target's output carries the table roots, not merely some file.
// fieldShape and rootShape spell a declaration back so a gate can pin it
// whole; tableRootsEmitted is the output half.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fieldShape spells one field as the schema declares it: the array or keyed
// prefix with its bound, `?` and `*`, the type, and a specified default.
func fieldShape(f *ir.Field) string {
	var b strings.Builder
	b.WriteString(f.Name + " ")
	switch {
	case f.KeyEnum != "":
		b.WriteString("[" + f.KeyEnum + "]")
	case f.Array == ir.ArrayFixed:
		fmt.Fprintf(&b, "[%d]", f.ArrayBound)
	case f.Array == ir.ArrayCounted && f.ArrayMin > 0:
		fmt.Fprintf(&b, "[%d..%d]", f.ArrayMin, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		fmt.Fprintf(&b, "[..%d]", f.ArrayBound)
	case f.Array == ir.ArrayList:
		b.WriteString("[]")
	}
	if f.Type.Optional {
		b.WriteString("?")
	}
	if f.Type.Pointer {
		b.WriteString("*")
	}
	switch f.Type.Kind {
	case ir.TNamed:
		b.WriteString(f.Type.Name)
	case ir.TMap:
		var entry []string
		if f.MapEntry != nil {
			for _, e := range f.MapEntry.Fields {
				entry = append(entry, fieldShape(e))
			}
		}
		b.WriteString("map{" + strings.Join(entry, "; ") + "}")
	case ir.TInt:
		if !f.Type.Signed {
			b.WriteString("u")
		}
		b.WriteString("int" + strconv.Itoa(f.Type.Width))
	case ir.TBool:
		b.WriteString("bool")
	case ir.TString:
		fmt.Fprintf(&b, "string(%d)", f.Type.Size)
	case ir.TBytes:
		fmt.Fprintf(&b, "bytes(%d)", f.Type.Size)
	default:
		fmt.Fprintf(&b, "kind%d", f.Type.Kind)
	}
	switch {
	case len(f.DefBytes) > 0:
		fmt.Fprintf(&b, " = %q", f.DefBytes)
	case f.HasDefault && f.DefInt != nil:
		b.WriteString(" = " + f.DefInt.String())
	}
	return b.String()
}

// rootShape spells a declaration's fields, one per line.
func rootShape(st *ir.Struct) string {
	var lines []string
	for _, f := range st.Fields {
		lines = append(lines, fieldShape(f))
	}
	return strings.Join(lines, "\n")
}

// wantRoot holds one table root of a fixture to its class (variable or fixed)
// and its exact field list.
func wantRoot(u *ir.Unit, name string, fixed bool, want ...string) error {
	st := u.Tables[name]
	if st == nil {
		return fmt.Errorf("the unit declares no table %s", name)
	}
	if st.FixedDeclared != fixed {
		return fmt.Errorf("table %s: fixed is %v, want %v", name, st.FixedDeclared, fixed)
	}
	if got := rootShape(st); got != strings.Join(want, "\n") {
		return fmt.Errorf("table %s declares\n%s\nwant\n%s", name, got, strings.Join(want, "\n"))
	}
	return nil
}

// tableRootsEmitted is the output half: every named table root appears in what
// the target emitted, in a file name or a file's text, in whatever case and
// underscore convention the target writes names. A target that emits the
// unit's packet files and drops its table products fails by root name; the
// gates' negative controls feed it exactly that output.
func tableRootsEmitted(files map[string][]byte, roots ...string) error {
	if len(files) == 0 {
		return fmt.Errorf("nothing was emitted")
	}
	fold := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "_", "") }
	var missing []string
	for _, root := range roots {
		found := false
		for name, text := range files {
			if strings.Contains(fold(name), fold(root)) || strings.Contains(fold(string(text)), fold(root)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, root)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d file(s) emitted and none carries table %s", len(files), strings.Join(missing, " or "))
	}
	return nil
}
