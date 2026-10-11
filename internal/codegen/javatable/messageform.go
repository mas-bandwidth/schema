// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2: a batch of
// bitpacked bodies under one announced vocabulary. This file carries the
// ANNOUNCEMENT surface of that form — the compile-time constant byte array the
// page names (§3.3, "the announcement is a compile-time constant byte array
// with a length") and its reader, `announce`, `announceMeasure` and
// `announceRead`. The batch surface (`measureMessages`, `saveMessages`,
// `loadMessages`) is a later card.
//
// THE FORM IS A FIXED TABLE'S (§3.3), so this surface is emitted for a unit
// with a `fixed table` root and for no other: a message has to step through a
// shape the declaration fixes.
package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageAnnouncementClass is the one public type the announcement surface
// lives in: Java has no unit class to hang a package-level function on, so
// `announce`, `announceMeasure` and `announceRead` are static and instance
// members of TableVocabulary.
const messageAnnouncementClass = "TableVocabulary"

// messageAnnouncementBytes is the announcement constant, byte for byte, which
// §3.3 fixes at compile time.
func messageAnnouncementBytes(u *ir.Unit) []byte { return ir.TableAnnouncement(u) }

// javaByteArrayLiteral spells a byte[] initializer. Java bytes are SIGNED, so
// a value above 0x7f needs the cast the Go literal does not.
func javaByteArrayLiteral(b []byte) string {
	var s strings.Builder
	s.WriteString("new byte[] {")
	for i, v := range b {
		if i > 0 {
			s.WriteString(", ")
		}
		fmt.Fprintf(&s, "(byte) 0x%02x", v)
	}
	s.WriteString("}")
	return s.String()
}

// generateMessageFiles emits the announcement surface for a unit with a fixed
// root. Without one there is no message form to read (§3.3) and the file is
// not written.
func generateMessageFiles(u *ir.Unit) (map[string][]byte, error) {
	if len(fixedRoots(u)) == 0 {
		return map[string][]byte{}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "// The announcement surface of the MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, for unit %s.\n", u.Package)
	fmt.Fprintf(&b, "public final class %s {\n\n", messageAnnouncementClass)
	fmt.Fprintf(&b, "    private %s() {}\n\n", messageAnnouncementClass)
	fmt.Fprintf(&b, "    // The compile-time constant byte array one announcement carries (§3.3).\n")
	fmt.Fprintf(&b, "    static final byte[] tableAnnouncement = %s;\n\n", javaByteArrayLiteral(messageAnnouncementBytes(u)))
	fmt.Fprintf(&b, "    // The announcement's length (§3.3): int announceMeasure().\n")
	fmt.Fprintf(&b, "    public static int announceMeasure() { return tableAnnouncement.length; }\n\n")
	fmt.Fprintf(&b, "    // Copy the announcement into the caller's buffer and answer its length,\n")
	fmt.Fprintf(&b, "    // or -1 when the buffer is too small (§3.3).\n")
	fmt.Fprintf(&b, "    public static int announce(byte[] buffer) {\n")
	fmt.Fprintf(&b, "        if (buffer == null || buffer.length < tableAnnouncement.length) { return -1; }\n")
	fmt.Fprintf(&b, "        System.arraycopy(tableAnnouncement, 0, buffer, 0, tableAnnouncement.length);\n")
	fmt.Fprintf(&b, "        return tableAnnouncement.length;\n")
	fmt.Fprintf(&b, "    }\n\n")
	fmt.Fprintf(&b, "}\n")
	return map[string][]byte{
		messageAnnouncementClass + ".java": javaFile(u, "the MESSAGE FORM's announcement surface (docs/SPEC-TABLES.md §3.3), form byte 2", b.String()),
	}, nil
}
