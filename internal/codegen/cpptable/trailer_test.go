package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestTableIdsWriteBulkProperties(t *testing.T) {
	// Verify that the emitted TableIdsWrite in cpptable.go carries:
	// 1. Single capacity test over total_bytes = (count + 1) * 8
	// 2. Direct memcpy of ids.ids on little-endian
	// 3. Endian-correct byte-swapping on big-endian
	// 4. Little-endian entry count stored at the tail
	// 5. Unbroken w.offset advance
	requiredSnippets := []string{
		"const int64_t count = ids.count;",
		"const int64_t total_bytes = ( count + 1 ) * 8;",
		"if ( w.overflow || w.offset + total_bytes > w.capacity )",
		"uint8_t * dst = w.buffer + w.offset;",
		"defined( __ORDER_BIG_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__",
		"memcpy( dst, ids.ids, (size_t)( count * 8 ) );",
		"const uint64_t n = uint64_t( count );",
		"memcpy( dst + count * 8, &n, 8 );",
		"w.offset += total_bytes;",
	}

	u := &ir.Unit{Package: "bench"}
	src := tablePrimitives("bench", false, false, false, 64, u)
	for _, snip := range requiredSnippets {
		if !strings.Contains(src, snip) {
			t.Errorf("tablePrimitives missing required trailer snippet: %q", snip)
		}
	}
}
