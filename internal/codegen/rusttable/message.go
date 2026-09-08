package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// A unit's announcement is compiler-settled, beside its build version. No
// authoring walk, runtime vocabulary resolution or allocation constructs it.
func emitAnnouncement(b *strings.Builder, u *ir.Unit) {
	bytes := ir.TableAnnouncement(u)
	g := &gen{unit: u}
	fmt.Fprintf(b, "pub const TABLE_MESSAGE_NODE_SLOT:u64=%d;\npub const TABLE_MESSAGE_BLOB_SLOTS:[u64;2]=[%d,%d];\n", g.messageName(ir.TableNodeWireId), g.messageName(ir.BytesWireTypeId), g.messageName(ir.StringWireTypeId))
	fmt.Fprintf(b, "pub const TABLE_MESSAGE_ENTRIES:usize=%d;\npub const TABLE_MESSAGE_REF_BITS:usize=%d;\n", len(ir.TableVocabulary(u)), ir.TableMessageRefBits(len(ir.TableVocabulary(u))))
	fmt.Fprintf(b, "pub const TABLE_ANNOUNCEMENT:&[u8]=&%s;\n", rustBytes(bytes))
	b.WriteString(`
pub const fn announce_measure()->usize { TABLE_ANNOUNCEMENT.len() }
pub fn announce(buffer:&mut [u8])->core::result::Result<usize,TableMessageError> {
    let size=TABLE_ANNOUNCEMENT.len();
    if buffer.len()<size{return Err(TableMessageError::BufferTooSmall);}
    buffer[..size].copy_from_slice(TABLE_ANNOUNCEMENT);Ok(size)
}
`)
}
