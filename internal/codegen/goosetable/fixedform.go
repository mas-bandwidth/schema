// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, for Goose. This file
// is the fixed form's card: it builds the layout (§1), the hash (§5.2) and the
// per-table Goose surface — the constant body size, the baked layout bytes and
// hash, and the entry points that call the runtime in runtime/fixed.goose —
// following docs/PORT-GOOSE.md (THE RUNTIME LAYOUT) and
// docs/FIXED-FORM-ALGORITHM.md. It registers with the table surface through
// [Register] so it joins every <Base>Table.goose module.
//
// The compile-time walk is ir.TableFixedWalkRoot's, the ONE walk every target
// shares (docs/FIXED-FORM-ALGORITHM.md §1): a port that re-derived it would be
// a second opinion on the wire. The emitted constants are the walk's bytes
// exactly as they ride, so the corpus comparison in fixedform_test.go is a
// comparison of the reference's bytes with the bytes this build would write.
package goosetable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedLayoutBytes is the layout exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian
// (docs/FIXED-FORM-ALGORITHM.md §1; ir.TableFixedLayoutBytes).
func fixedLayoutBytes(entries []ir.TableFixedLayoutEntry) []byte {
	return ir.TableFixedLayoutBytes(entries)
}

// fixedRootFacts is everything a fixed root bakes at build time: its layout
// bytes, the hash over those bytes and the definitions digest
// (docs/FIXED-FORM-ALGORITHM.md §1, §5.2), and its constant body size (§3).
func fixedRootFacts(st *ir.Struct) (layout []byte, hash uint64, body int64) {
	entries := ir.TableFixedWalkRoot(st)
	layout = fixedLayoutBytes(entries)
	hash = ir.TableFixedLayoutHash(layout, st)
	body = ir.TableFixedTypeBytes(st)
	return layout, hash, body
}

// fixedGooseBytes renders bytes as the body of a Goose string literal, one
// \xNN escape per byte, so the layout is baked as data rather than rebuilt at
// run time (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func fixedGooseBytes(data []byte) string {
	var sb strings.Builder
	for _, b := range data {
		sb.WriteString(fmt.Sprintf("\\x%02x", b))
	}
	return sb.String()
}

// A Writer is the emit sink this card needs: the port-design skeleton's
// *goose.Gen satisfies it (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
type Writer interface {
	Pf(format string, args ...any)
}

// EmitFixedForm is this card's contribution to one fixed root: the constant
// body size, the hash, and the layout bytes and length
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT; docs/FIXED-FORM-ALGORITHM.md §1,
// §3, §5.2).
func EmitFixedForm(w Writer, st *ir.Struct) error {
	layout, hash, body := fixedRootFacts(st)
	w.Pf("// ---- %s, the fixed form, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n", st.Name)
	w.Pf("let %sFixedBodyBytes: i64 = %d;\n", st.Name, body)
	w.Pf("let %sFixedRecordBytes: i64 = 8 + %sFixedBodyBytes;\n", st.Name, st.Name)
	w.Pf("let %sFixedHash: u64 = 0x%016x;\n", st.Name, hash)
	w.Pf("let %sFixedLayout: const u8[:] = \"%s\";\n", st.Name, fixedGooseBytes(layout))
	w.Pf("let %sFixedLayoutLength: i64 = %d;\n", st.Name, len(layout))
	return nil
}

// fixedRootsOf is the roots of the unit this module declares, in declaration
// order (docs/FIXED-FORM-ALGORITHM.md §2.2: a form is a whole unit's, and the
// fixed roots are [ir.TableFixedFormRoots]).
func fixedRootsOf(g *goose.Gen) []*ir.Struct {
	roots := ir.TableFixedFormRoots(g.Unit)
	byName := map[string]*ir.Struct{}
	for _, st := range roots {
		byName[st.Name] = st
	}
	var out []*ir.Struct
	for _, t := range g.File.Tables {
		if st := byName[t.Name]; st != nil {
			out = append(out, st)
		}
	}
	return out
}

// emitFixedFormPart is the [Register]d part: every fixed root the file
// declares gets its baked constants (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func emitFixedFormPart(g *goose.Gen) error {
	roots := fixedRootsOf(g)
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for _, st := range roots {
		if err := EmitFixedForm(g, st); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	Register("fixedform", emitFixedFormPart)
}
