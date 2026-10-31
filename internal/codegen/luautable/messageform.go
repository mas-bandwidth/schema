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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("messageform", emitMessageForm)
}

// emitMessageForm writes the unit's announcement, its length, the reference
// width and the entry count into every table module (docs/SPEC-TABLES.md §3.3,
// "The announcement"). The bytes are ir.TableAnnouncement, the same bytes every
// other backend emits and the manifest's `connection` rows pin.
func emitMessageForm(g *luau.Gen) error {
	ann := ir.TableAnnouncement(g.Unit)
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
	g.Pf("%s.TableMessageRefBits = %d\n", g.Self, ir.TableMessageRefBits(len(ir.TableVocabulary(g.Unit))))
	g.Pf("%s.TableMessageEntriesHere = %d\n", g.Self, len(ir.TableVocabulary(g.Unit)))
	g.Pf("%s.TableMessageBuildVersion = %s\n\n", g.Self, luau.IntegerLit(new(big.Int).SetUint64(ir.BuildVersion(g.Unit))))
	return nil
}
