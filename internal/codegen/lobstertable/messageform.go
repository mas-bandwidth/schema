// The message form, form byte 2 (docs/SPEC-TABLES.md §3.3; docs/PORT-LOBSTER.md,
// THE CARD MAP): the facts each table module registers with
// runtime/message.lobster. The walk lives once in the runtime; what is emitted
// here are the compile-time constants the reference emits as literals — the
// unit's announcement and build version (ir.TableAnnouncement), the roots that
// may ride (a FIXED table's, §3.3), and every field's and every union arm's
// SLOT in the unit's vocabulary (ir.TableVocabularySlots), so a writer never
// looks a slot up by shape.
package lobstertable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("message-form", emitMessageForm) }

// MessageRoot reports whether a table may ride the message form: a FIXED
// table's, never a plain table or a map's generated entry (§3.3).
func MessageRoot(st *ir.Struct) bool {
	return st.IsTable && st.FixedDeclared && !st.IsMapEntry()
}

func emitMessageForm(g *lobster.Gen) error {
	u := g.Unit
	reach := closure(u)
	slots := ir.TableVocabularySlots(u)
	base := g.File.Base
	g.Import("message")
	g.Pf("// %sMessageUnit — the unit's announcement and build version, the message\n", base)
	g.Pf("// form's compile-time constants (docs/SPEC-TABLES.md §3.3)\n")
	g.Pf("let _%sMessageUnit = %s.message_unit(%s, %s)\n", base, rt, lobster.StringLit(ir.TableAnnouncement(u)),
		lobster.IntLit(new(big.Int).SetUint64(ir.BuildVersion(u))))
	structs := append([]*ir.Struct{}, g.File.Tables...)
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok && reach[st.Name] {
			structs = append(structs, st)
		}
	}
	seen := map[string]bool{}
	for _, st := range structs {
		if seen[st.Name] || st.IsMapEntry() {
			continue
		}
		seen[st.Name] = true
		list := make([]string, len(st.Fields))
		for i, f := range st.Fields {
			list[i] = fmt.Sprint(slots[ir.TableFieldEntry(f).Key()])
		}
		g.Pf("let _%sMessageSlots = %s.message_slots(%s, [%s]::int)\n", st.Name, rt, lobster.StringLit([]byte(st.Name)), strings.Join(list, ", "))
		if MessageRoot(st) {
			g.Pf("let _%sMessageRoot = %s.message_root(%s)\n", st.Name, rt, lobster.StringLit([]byte(st.Name)))
		}
	}
	unions := append([]*ir.Union{}, g.File.TableUnions...)
	for _, d := range g.File.Decls {
		if un, ok := d.(*ir.Union); ok && reach[un.Name] {
			unions = append(unions, un)
		}
	}
	useen := map[string]bool{}
	for _, un := range unions {
		if useen[un.Name] {
			continue
		}
		useen[un.Name] = true
		list := make([]string, len(un.Variants))
		for i, v := range un.Variants {
			list[i] = fmt.Sprint(slots[ir.TableArmEntry(v).Key()])
		}
		g.Pf("let _%sMessageArms = %s.message_arm_slots(%s, [%s]::int)\n", un.Name, rt, lobster.StringLit([]byte(un.Name)), strings.Join(list, ", "))
	}
	g.Pf("\n")
	return nil
}
