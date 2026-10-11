package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// AnnouncementModule is the unit-level module carrying the MESSAGE FORM's
// announcement surface (docs/SPEC-TABLES.md §3.3).
const AnnouncementModule = "announcement"

// announcementModule emits Announce and AnnounceMeasure as a compile-time
// constant byte array and its length. docs/SPEC-TABLES.md §3.3: "THE WHOLE
// ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT ... so a backend emits
// `Announce` and `AnnounceMeasure` as a constant byte array and its length
// rather than as a walk." The bytes are settled by the compiler in
// ir.TableAnnouncement, the same value the Go and C# legs carry.
func announcementModule(u *ir.Unit) []byte {
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package,
		"the message form's announcement (docs/SPEC-TABLES.md §3.3)"))
	bytes := ir.TableAnnouncement(u)
	fmt.Fprintf(&b, "\n// ANNOUNCEMENT (docs/SPEC-TABLES.md §3.3): an ordinary form-`1` file\n// whose body carries the build version and the vocabulary. Every byte is\n// settled by the compiler, so it rides as a constant and not as a walk.\n")
	fmt.Fprintf(&b, "pub const TABLE_ANNOUNCEMENT: [u8; %d] = [\n", len(bytes))
	for i, v := range bytes {
		if i%12 == 0 {
			b.WriteString("    ")
		}
		fmt.Fprintf(&b, "0x%02x,", v)
		if i%12 == 11 || i == len(bytes)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("];\n")
	fmt.Fprintf(&b, "pub const TABLE_ANNOUNCEMENT_LEN: usize = %d;\n", len(bytes))
	b.WriteString("\npub fn announce() -> &'static [u8] { &TABLE_ANNOUNCEMENT }\n")
	b.WriteString("pub fn announce_measure() -> i64 { TABLE_ANNOUNCEMENT_LEN as i64 }\n")
	return []byte(b.String())
}
