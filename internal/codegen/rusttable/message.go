package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// AnnounceModule is the MESSAGE FORM's announcement surface (docs/SPEC-TABLES.md
// §3.3), emitted once per unit that carries a fixed root. The announcement is a
// compile-time constant byte array with a length; the form is a fixed table's,
// so it rides beside the fixed form (§3.3, §3.4).
const AnnounceModule = "message_announce"

// announceModule emits message_announce.rs: the unit's whole announcement as a
// compile-time constant, its length, and the copy that hands it out.
//
// "The ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE ... a compile-time constant byte
// array with a length" (§3.3). `announce` writes those bytes into a caller's
// buffer and answers the byte count, or -1 when the buffer is short.
func announceModule(u *ir.Unit) []byte {
	bytes := ir.TableAnnouncement(u)
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package,
		"THE MESSAGE FORM's announcement (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("//\n")
	b.WriteString("// The announcement is the unit's whole vocabulary, delivered once before the\n")
	b.WriteString("// first body and never again for the life of the connection. It is a compile-time\n")
	b.WriteString("// constant byte array with a length, and nothing about it is discovered at run\n")
	b.WriteString("// time (docs/SPEC-TABLES.md §3.3).\n\n")
	fmt.Fprintf(&b, "pub const TABLE_ANNOUNCEMENT: [u8; %d] = [", len(bytes))
	for i, v := range bytes {
		if i%12 == 0 {
			b.WriteString("\n    ")
		}
		fmt.Fprintf(&b, "0x%02x,", v)
	}
	b.WriteString("\n];\n\n")
	b.WriteString("/// The announcement's length in bytes (§3.3).\n")
	b.WriteString("pub fn announce_measure() -> usize { TABLE_ANNOUNCEMENT.len() }\n\n")
	b.WriteString("/// Write the announcement into the caller's buffer and answer its byte count,\n")
	b.WriteString("/// or -1 when the buffer is short (§3.3).\n")
	b.WriteString("pub fn announce(buffer: &mut [u8]) -> i64 {\n")
	b.WriteString("    if buffer.len() < TABLE_ANNOUNCEMENT.len() {\n")
	b.WriteString("        return -1;\n")
	b.WriteString("    }\n")
	b.WriteString("    buffer[..TABLE_ANNOUNCEMENT.len()].copy_from_slice(&TABLE_ANNOUNCEMENT);\n")
	b.WriteString("    TABLE_ANNOUNCEMENT.len() as i64\n")
	b.WriteString("}\n")
	return []byte(b.String())
}
