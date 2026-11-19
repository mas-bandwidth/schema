// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, in Luau: the
// announcement is a COMPILE-TIME CONSTANT of the unit ("THE WHOLE ANNOUNCEMENT
// IS A COMPILE-TIME CONSTANT OF THE UNIT ... a backend emits `Announce` and
// `AnnounceMeasure` as a constant byte array and its length rather than as a
// walk"), so every <Base>Table.luau module carries the unit's bytes and the
// facts a body walk needs. The read and write walks themselves are the home
// runtime's (runtime/schema_message.luau), driven by the descriptors luautable.go
// emits; no per-table code is written here.
package luautable

import (
	"math/big"
	"slices"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("messageform", emitMessageForm)
}

// emitMessageForm writes the unit's announcement, its length, the reference
// width and the entry count into every table module (docs/SPEC-TABLES.md §3.3,
// "The announcement"). The bytes are tablewire.Announce, the same bytes every
// other backend emits and the manifest's `connection` rows pin.
func emitMessageForm(g *luau.Gen) error {
	ann := tablewire.Announce(g.Unit)
	g.Pf("-- the message form's announcement, form byte 2 (docs/SPEC-TABLES.md §3.3):\n")
	g.Pf("-- an ordinary form-1 file carrying the build version and the vocabulary.\n")
	g.Pf("%s.TableMessageAnnouncement = (function()\n", g.Self)
	g.Pf("\tlocal b = buffer.create(%d)\n", len(ann))
	g.Pf("\tlocal v = {")
	for i, c := range ann {
		if i%24 == 0 {
			g.Pf("\n\t\t")
		}
		g.Pf("%d,", c)
	}
	g.Pf("\n\t}\n")
	g.Pf("\tfor i = 1, #v do buffer.writeu8(b, i - 1, v[i]) end\n")
	g.Pf("\treturn b\nend)()\n")
	g.Pf("%s.TableMessageAnnouncementLength = %d\n", g.Self, len(ann))
	g.Pf("%s.TableMessageRefBits = %d\n", g.Self, ir.TableMessageRefBits(len(tablewire.MessageVocabulary(g.Unit))))
	g.Pf("%s.TableMessageEntriesHere = %d\n", g.Self, len(tablewire.MessageVocabulary(g.Unit)))
	g.Pf("%s.TableMessageBuildVersion = %s\n\n", g.Self, luau.IntegerLit(new(big.Int).SetUint64(ir.BuildVersion(g.Unit))))
	// the build version under each table the message form carries (a fixed
	// table, §3.3: the form is fixed tables only, so no variable table is
	// named here), so a message read knows when the sender announced this
	// reader's own build (§3.3 (a), (b))
	names := make([]string, 0, len(g.Unit.Tables))
	for _, t := range g.Unit.Tables {
		if t.FixedDeclared {
			names = append(names, t.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	vocab := tablewire.MessageVocabulary(g.Unit)
	slotOf := func(want ir.TableVocabularyEntry) int {
		key := want.Key()
		for idx, e := range vocab {
			if e.Id == want.Id && e.Kind == want.Kind && e.Key() == key {
				return idx + 1
			}
		}
		return 0
	}
	closure := tablewire.MessageClosure(g.Unit)
	cNames := make([]string, 0, len(closure))
	for name := range closure {
		cNames = append(cNames, name)
	}
	slices.Sort(cNames)
	var allNames []string
	unions := map[string]*ir.Union{}
	var collectUnions func(t ir.FieldType)
	collectUnions = func(t ir.FieldType) {
		if t.Kind == ir.TNamed {
			if un, ok := t.Ref.(*ir.Union); ok && unions[un.Name] == nil {
				unions[un.Name] = un
				for _, v := range un.Variants {
					if v.F != nil {
						collectUnions(v.F.Type)
					}
				}
			}
		}
	}
	for _, name := range cNames {
		stDef := g.Unit.Tables[name]
		if stDef == nil {
			stDef = g.Unit.Structs[name]
		}
		if stDef == nil {
			continue
		}
		allNames = append(allNames, name)
		for _, f := range stDef.Fields {
			collectUnions(f.Type)
		}
	}
	uNames := make([]string, 0, len(unions))
	for unName := range unions {
		uNames = append(uNames, unName)
	}
	slices.Sort(uNames)
	st := g.Require(RuntimeModule)
	g.Pf("%s.messageAnnouncement(%s.TableMessageBuildVersion, %s.TableMessageAnnouncement)\n", st, g.Self, g.Self)
	g.Pf("%s.messageSlots(%s.TableMessageBuildVersion, {", st, g.Self)
	for _, name := range cNames {
		stDef := g.Unit.Tables[name]
		if stDef == nil {
			stDef = g.Unit.Structs[name]
		}
		if stDef == nil {
			continue
		}
		g.Pf(" [%q] = {", name)
		for i, f := range stDef.Fields {
			if i > 0 {
				g.Pf(",")
			}
			g.Pf(" %d", slotOf(ir.TableFieldEntry(f)))
		}
		g.Pf(" },")
	}
	for _, unName := range uNames {
		un := unions[unName]
		g.Pf(" [%q] = {", unName)
		for i, v := range un.Variants {
			if i > 0 {
				g.Pf(",")
			}
			g.Pf(" %d", slotOf(ir.TableArmEntry(v)))
		}
		g.Pf(" },")
	}
	g.Pf(" })\n")
	g.Pf("%s.buildVersion(%s.TableMessageBuildVersion, {", st, g.Self)
	for i, name := range allNames {
		if i > 0 {
			g.Pf(",")
		}
		g.Pf(" %q", name)
	}
	g.Pf(" })\n\n")
	return nil
}
