// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, in moo. The
// announcement is a COMPILE-TIME CONSTANT of the unit, so every
// <Base>Table.moo module carries the unit's announcement bytes (the bytes
// ir.TableAnnouncement writes and the manifest's `connection` rows pin), the
// reference width, the entry count and the build version; and beside every
// descriptor the module registers, the VOCABULARY SLOT of each field (or
// arm), in declared order. The walks themselves are the runtime's
// (runtime/table_message.moo), driven by those slots, the descriptors and the
// shapes the runtime reads back out of the unit's own announcement — so the
// shape a writer encodes with and the shape a reader announces are one parse
// of one set of bytes.
package mootable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("messageform", emitMessageForm)
}

// messageSlots is the unit's vocabulary as slot numbers keyed by entry
// identity (ir.TableVocabularyEntry.Key): slot k is entry k, 1-based.
func messageSlots(u *ir.Unit) map[string]int {
	out := map[string]int{}
	for i, e := range ir.TableVocabulary(u) {
		out[e.Key()] = i + 1
	}
	return out
}

func emitMessageForm(g *Gen) error {
	ann := ir.TableAnnouncement(g.Unit)
	vocabulary := ir.TableVocabulary(g.Unit)
	ms := g.Require("table_message")
	g.Pf("-- THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): the unit's announcement, an\n")
	g.Pf("-- ordinary form-1 file carrying the build version and the vocabulary, a\n")
	g.Pf("-- compile-time constant, registered once with the message runtime.\n")
	g.Pf("%s.TableMessageAnnouncement = %s.unit(%s)\n", g.Self, ms, luaString(ann))
	g.Pf("%s.TableMessageAnnouncementLength = %d\n", g.Self, len(ann))
	g.Pf("%s.TableMessageRefBits = %d\n", g.Self, ir.TableMessageRefBits(len(vocabulary)))
	g.Pf("%s.TableMessageEntriesHere = %d\n", g.Self, len(vocabulary))
	g.Pf("%s.TableMessageBuildVersion = %s -- the eight little-endian bytes of the build version\n\n", g.Self, IdLit(ir.BuildVersion(g.Unit)))

	slots := messageSlots(g.Unit)
	slotOf := func(e ir.TableVocabularyEntry) string {
		return fmt.Sprint(slots[e.Key()]) // 0: the vocabulary does not spell it, which the runtime refuses
	}
	emitStruct := func(st *ir.Struct) {
		parts := make([]string, len(st.Fields))
		for i, f := range st.Fields {
			parts[i] = slotOf(ir.TableFieldEntry(f))
		}
		g.Pf("%s.slots(%q, { %s })\n", ms, st.Name, strings.Join(parts, ", "))
	}
	emitUnion := func(un *ir.Union) {
		parts := make([]string, len(un.Variants))
		for i, v := range un.Variants {
			parts[i] = slotOf(ir.TableArmEntry(v))
		}
		g.Pf("%s.slots(%q, { %s })\n", ms, un.Name, strings.Join(parts, ", "))
	}
	// THE MESSAGE FORM IS FIXED TABLES ONLY (§3.3): the slots are the fixed
	// tables' and what they reach, and a variable table registers none, so
	// table_message.moo refuses it by name before a walk begins.
	g.Pf("-- each fixed table's fields (a union's arms) as vocabulary slots, declared order\n")
	reach := reachFrom(g.Unit, func(t *ir.Struct) bool { return t.FixedDeclared }, false)
	for _, un := range g.File.TableUnions {
		if reach[un.Name] {
			emitUnion(un)
		}
	}
	for _, t := range g.File.Tables {
		if reach[t.Name] {
			emitStruct(t)
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !d.IsTable && reach[d.Name] {
				emitStruct(d)
			}
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] == nil && reach[d.Name] {
				emitUnion(d)
			}
		}
	}
	g.Pf("\n")
	return nil
}
