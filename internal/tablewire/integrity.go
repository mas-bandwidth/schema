package tablewire

import (
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/ir"
	"hash/crc32"
)

var fileCRC32C = crc32.MakeTable(crc32.Castagnoli)

// fileIntegrityBody verifies the root file before its id table or body is
// interpreted. Callers check the form first. Nested declarations never seal
// their containing file; only the entry root selects this trailer.
func fileIntegrityBody(root *ir.Struct, data []byte) ([]byte, bool) {
	if root.Integrity == "" {
		return data, true
	}
	if root.Integrity != "crc32c" || len(data) < 5 {
		return nil, false
	}
	n := len(data) - 4
	if binary.LittleEndian.Uint32(data[n:]) != crc32.Checksum(data[:n], fileCRC32C) {
		return nil, false
	}
	return data[:n], true
}
